// Package rest is the small JSON-over-HTTP client both forge clients share.
//
// Small on purpose. The bridge uses about a dozen endpoints on each side, and a
// full SDK for either forge would bring a major version a month and still not
// do the two things this package is for: follow pagination without ever
// sending a credential to a host it was not configured for, and answer an
// unchanged GitHub list from its ETag, which GitHub does not count against the
// rate limit.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
)

// maxErrorBody bounds how much of an error response is kept for the message.
const maxErrorBody = 512

// StatusError is a response outside 2xx.
type StatusError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.Status, e.Body)
}

// Is makes a 404 or a 410 match forge.ErrNotFound and a 422 forge.ErrRejected,
// so callers above the clients never look at status codes.
func (e *StatusError) Is(target error) bool {
	switch target {
	case forge.ErrNotFound:
		return e.Status == http.StatusNotFound || e.Status == http.StatusGone
	case forge.ErrRejected:
		return e.Status == http.StatusUnprocessableEntity
	}
	return false
}

// Authorizer adds credentials to a request. It is called for every request,
// so it can hand out a token that rotates.
type Authorizer func(ctx context.Context, req *http.Request) error

// Client talks to one API.
type Client struct {
	base      *url.URL
	http      *http.Client
	authorize Authorizer
	headers   http.Header
	etags     *etagCache
	observe   func(*http.Response)
}

// Option configures a Client.
type Option func(*Client)

// WithHeader sets a header on every request.
func WithHeader(name, value string) Option {
	return func(c *Client) { c.headers.Set(name, value) }
}

// WithETagCache answers repeated GETs with conditional requests.
func WithETagCache(entries int) Option {
	return func(c *Client) { c.etags = newETagCache(entries) }
}

// WithObserver sees every response, for metrics such as a rate-limit gauge.
func WithObserver(f func(*http.Response)) Option {
	return func(c *Client) { c.observe = f }
}

// New creates a client for the API at base.
func New(base *url.URL, httpClient *http.Client, authorize Authorizer, opts ...Option) *Client {
	c := &Client{
		base:      base,
		http:      httpClient,
		authorize: authorize,
		headers:   http.Header{},
	}
	c.headers.Set("Accept", "application/json")
	c.headers.Set("User-Agent", "forgejo-github-bridge")
	for _, o := range opts {
		o(c)
	}
	return c
}

// URL appends an API path to the base URL. The path is taken as already
// escaped: the clients escape each segment they interpolate with Seg.
//
// Appended rather than resolved, so a base with a path, such as Forgejo's
// /api/v1, keeps it.
func (c *Client) URL(path string, query url.Values) string {
	s := strings.TrimRight(c.base.String(), "/") + path
	if q := query.Encode(); q != "" {
		s += "?" + q
	}
	return s
}

// Seg escapes one path segment.
func Seg(s string) string { return url.PathEscape(s) }

// Do sends one request. in is encoded as the JSON body when not nil; out
// receives the decoded response when not nil.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	_, err := c.do(ctx, method, c.URL(path, query), in, out)
	return err
}

// GetAll follows the Link header's rel="next" and decodes every page into one
// slice.
func GetAll[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	var all []T
	next := c.URL(path, query)
	for next != "" {
		var page []T
		header, err := c.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		next, err = c.nextPage(header)
		if err != nil {
			return nil, err
		}
	}
	return all, nil
}

// nextPage reads the next page's URL from a response, and refuses one that
// points anywhere but the configured API. The next URL is what the server
// says; following it to another host would send that host the token.
func (c *Client) nextPage(header http.Header) (string, error) {
	next := parseNextLink(header.Values("Link"))
	if next == "" {
		return "", nil
	}
	u, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("unparsable next page link %q: %w", next, err)
	}
	if !strings.EqualFold(u.Scheme, c.base.Scheme) || !strings.EqualFold(u.Host, c.base.Host) {
		return "", fmt.Errorf("next page link %q leaves %s", next, c.base.Host)
	}
	return next, nil
}

// do sends a request and returns the response's headers. The body is read
// and closed here, so nothing above has a body to forget.
func (c *Client) do(ctx context.Context, method, target string, in, out any) (http.Header, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("encoding the request to %s: %w", target, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header[k] = v
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.authorize != nil {
		if err := c.authorize(ctx, req); err != nil {
			return nil, err
		}
	}

	var cached *etagEntry
	if method == http.MethodGet && c.etags != nil {
		if cached = c.etags.get(target); cached != nil {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, redact(target), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.observe != nil {
		c.observe(resp)
	}

	var raw []byte
	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		raw = cached.body
		// The page's links belong to the cached page. A 304 need not repeat
		// them, and without them pagination would stop at a cached page.
		if len(resp.Header.Values("Link")) == 0 {
			resp.Header["Link"] = cached.links
		}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return resp.Header, &StatusError{Method: method, URL: redact(target), Status: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
	default:
		if raw, err = io.ReadAll(resp.Body); err != nil {
			return resp.Header, fmt.Errorf("reading the response to %s %s: %w", method, redact(target), err)
		}
		if method == http.MethodGet && c.etags != nil {
			if etag := resp.Header.Get("ETag"); etag != "" {
				c.etags.put(target, etagEntry{etag: etag, body: raw, links: resp.Header.Values("Link")})
			}
		}
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.Header, fmt.Errorf("decoding the response to %s %s: %w", method, redact(target), err)
		}
	}
	return resp.Header, nil
}

// redact drops the query from a URL for messages. Nothing the bridge sends
// in a query is secret, but a URL in a log line should not be the place that
// has to be checked for that.
func redact(target string) string {
	if i := strings.IndexByte(target, '?'); i >= 0 {
		return target[:i]
	}
	return target
}

// parseNextLink extracts rel="next" from RFC 8288 Link headers.
func parseNextLink(headers []string) string {
	for _, h := range headers {
		for _, part := range strings.Split(h, ",") {
			segs := strings.Split(part, ";")
			if len(segs) < 2 {
				continue
			}
			target := strings.TrimSpace(segs[0])
			if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
				continue
			}
			for _, p := range segs[1:] {
				p = strings.TrimSpace(p)
				if p == `rel="next"` || p == "rel=next" {
					return target[1 : len(target)-1]
				}
			}
		}
	}
	return ""
}

// IsNotFound reports whether err is a 404 or 410.
func IsNotFound(err error) bool { return errors.Is(err, forge.ErrNotFound) }

type etagEntry struct {
	etag  string
	body  []byte
	links []string
}

// etagCache is a bounded map with first-in-first-out eviction. The keys are
// the poll URLs, which change only when a cursor moves, so the working set is
// a few entries per repository and anything cleverer would be wasted.
type etagCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]etagEntry
	order   []string
}

func newETagCache(max int) *etagCache {
	return &etagCache{max: max, entries: map[string]etagEntry{}}
}

func (c *etagCache) get(key string) *etagEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		return &e
	}
	return nil
}

func (c *etagCache) put(key string, e etagEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		c.order = append(c.order, key)
		for len(c.order) > c.max {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.entries[key] = e
}
