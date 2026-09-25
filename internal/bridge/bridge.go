// Package bridge is the controller: polls that mark resources dirty, and the
// reconciler that makes both sides of a dirty resource agree.
//
// Nothing here reacts to what changed. A poll only says that something about
// an issue did; the reconciler then reads the whole current state of both
// sides and converges them. Running it twice does what running it once did,
// which is what lets every failure be handled the same way: try again later.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/translate"
)

const (
	// pollOverlap is how far before its cursor each poll starts reading.
	// Both forges index updates asynchronously, and an item updated just
	// before the previous poll may only become visible after it. Reading a
	// little twice is harmless; reconciling is idempotent.
	pollOverlap = 2 * time.Minute

	// projectionLookback is how far back the bridge looks for a projection
	// of its own that the database does not know about, before creating one.
	// That happens when the process dies between creating an issue and
	// recording it; a day is far longer than any such gap.
	projectionLookback = 24 * time.Hour

	// reconcileTimeout bounds one reconcile. A forge that hangs must not stop
	// the queue.
	reconcileTimeout = 2 * time.Minute

	// Backoff for a resource whose reconcile failed.
	backoffBase = 30 * time.Second
	backoffMax  = 30 * time.Minute

	// idleWait is how long the worker sleeps when nothing is due, unless a
	// poll wakes it first. It bounds how late an entry held back by backoff
	// is picked up.
	idleWait = 5 * time.Second
)

// Options configures a Bridge.
type Options struct {
	PollInterval time.Duration
	FullResync   time.Duration
	// ForgejoURLs are the addresses Forgejo is known by. A link starting with
	// any of them is a link into the private instance.
	ForgejoURLs []*url.URL
	UserMapping map[string]string
	// Messages overrides what the bridge says, for every repository.
	Messages map[string]string
}

// Bridge keeps the configured repositories in step.
type Bridge struct {
	store  store.Store
	github forge.Client
	forgej forge.Client
	opts   Options
	log    *slog.Logger
	now    func() time.Time

	repos map[string]*repo
	order []string

	githubSelf  string
	forgejoSelf string
	// contribSelf is the account imported pull requests are opened by. Like
	// forgejoSelf, nothing it wrote is ever a source.
	contribSelf string

	pulls *Pulls
	// reviews holds the Forgejo reviews of the pull request being
	// reconciled, by negated ID. Touched only by the single worker.
	reviews map[int64]forge.Review

	// seen is touched only by the poll loop.
	seen seenSet

	wake chan struct{}
}

// repo is one configured pair with its translator and messages.
type repo struct {
	cfg config.Repository
	tr  *translate.Translator
	msg *translate.Messages
}

// CheckMessages reports whether every repository's messages load. The
// bridge falls back to the defaults for a repository whose do not, so this
// is what startup calls to refuse a broken configuration instead.
func CheckMessages(global map[string]string, repos []config.Repository) error {
	for _, r := range repos {
		if _, err := translate.NewMessages(global, r.Messages); err != nil {
			return fmt.Errorf("%s: %w", r.GitHub, err)
		}
	}
	return nil
}

// New creates a bridge for the configured repositories.
func New(st store.Store, github, forgejo forge.Client, repos []config.Repository, opts Options, log *slog.Logger) *Bridge {
	b := &Bridge{
		store:  st,
		github: github,
		forgej: forgejo,
		opts:   opts,
		log:    log,
		now:    time.Now,
		repos:  map[string]*repo{},
		wake:   make(chan struct{}, 1),
	}
	for _, r := range repos {
		msg, err := translate.NewMessages(opts.Messages, r.Messages)
		if err != nil {
			log.Error("the configured messages do not load; using the defaults", "repository", r.Key(), "error", err)
			msg = translate.MustDefaultMessages()
		}
		b.repos[r.Key()] = &repo{cfg: r, tr: translate.New(r, opts.ForgejoURLs, opts.UserMapping), msg: msg}
		b.order = append(b.order, r.Key())
	}
	return b
}

// Init resolves the identities the bridge acts as on both sides. Everything
// either of them wrote is a projection, never a source.
func (b *Bridge) Init(ctx context.Context) error {
	var err error
	if b.githubSelf, err = b.github.Self(ctx); err != nil {
		return fmt.Errorf("identifying the GitHub App: %w", err)
	}
	if b.forgejoSelf, err = b.forgej.Self(ctx); err != nil {
		return fmt.Errorf("identifying the Forgejo account: %w", err)
	}
	if b.pulls != nil {
		for _, r := range b.repos {
			if !r.cfg.ImportsPullRequests() {
				continue
			}
			if b.contribSelf, err = b.pulls.Contrib.Self(ctx); err != nil {
				return fmt.Errorf("identifying the Forgejo contrib account: %w", err)
			}
			if b.contribSelf == b.forgejoSelf {
				// The split is what keeps imported pull requests untrusted in
				// CI (CONCEPT.md §8, §12). One account for both defeats it.
				return fmt.Errorf("the contrib token belongs to %s, the same account as the bridge token", b.contribSelf)
			}
			break
		}
	}
	b.log.Info("identities resolved", "github", b.githubSelf, "forgejo", b.forgejoSelf, "contrib", b.contribSelf)
	return nil
}

// Run polls, resyncs and reconciles until ctx ends. Init must have
// succeeded.
func (b *Bridge) Run(ctx context.Context) error {
	go b.pollLoop(ctx)
	go b.resyncLoop(ctx)
	b.work(ctx)
	return ctx.Err()
}

func (b *Bridge) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *Bridge) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(b.opts.PollInterval)
	defer ticker.Stop()
	for {
		b.PollAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PollAll polls every repository on both sides once.
func (b *Bridge) PollAll(ctx context.Context) {
	for _, key := range b.order {
		r := b.repos[key]
		for _, side := range []store.Side{store.GitHub, store.Forgejo} {
			start := b.now()
			err := b.poll(ctx, r, side)
			telemetry.RecordPoll(key, string(side), start, err)
			if err != nil && ctx.Err() == nil {
				b.log.Error("poll failed", "repository", key, "side", side, "error", err)
			}
		}
	}
	b.signal()
}

func (b *Bridge) resyncLoop(ctx context.Context) {
	ticker := time.NewTicker(b.opts.FullResync)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.Resync(ctx); err != nil && ctx.Err() == nil {
				b.log.Error("full resync failed", "error", err)
			}
		}
	}
}

// Resync marks every open pair dirty. Incremental polls see edits and new
// items but not deletions; a full pass over what is open is what catches
// those.
func (b *Bridge) Resync(ctx context.Context) error {
	for _, key := range b.order {
		open, err := b.store.OpenIssues(ctx, key)
		if err != nil {
			return err
		}
		keys := make([]store.DirtyKey, len(open))
		for i, m := range open {
			keys[i] = store.DirtyKey{Repository: key, Side: store.GitHub, Number: m.GitHubNumber}
		}
		if err := b.store.MarkDirty(ctx, keys...); err != nil {
			return err
		}
		telemetry.RecordMarked(key, "resync", len(keys))
	}
	b.signal()
	return nil
}

// work drains the queue, one resource at a time.
//
// One at a time on purpose. The two halves of a pair can both be queued, and
// two workers reconciling the same pair would each see a comment without a
// projection and each create one. The volume of an open-source project's
// issue traffic does not need more than one.
func (b *Bridge) work(ctx context.Context) {
	stats := time.NewTicker(15 * time.Second)
	defer stats.Stop()
	for ctx.Err() == nil {
		select {
		case <-stats.C:
			if q, err := b.store.DirtyStats(ctx); err == nil {
				telemetry.SetQueue(q.Depth, q.Failing, q.Oldest)
			}
		default:
		}

		processed, err := b.Step(ctx)
		if err != nil && ctx.Err() == nil {
			b.log.Error("reading the queue failed", "error", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
		case <-b.wake:
		case <-time.After(idleWait):
		}
	}
}

// Step reconciles the next due resource, if there is one, and reports
// whether there was.
func (b *Bridge) Step(ctx context.Context) (bool, error) {
	d, ok, err := b.store.NextDirty(ctx)
	if err != nil || !ok {
		return false, err
	}

	rctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	err = b.reconcile(rctx, d.DirtyKey)
	cancel()
	telemetry.RecordReconcile(d.Repository, err)

	if err != nil {
		if ctx.Err() != nil {
			// Shutting down. The entry stays queued as it was.
			return true, ctx.Err()
		}
		delay := backoff(d.Attempts)
		if errors.Is(err, errWaitingForMirror) {
			b.log.Info("merged; waiting for the push mirror", "repository", d.Repository, "number", d.Number, "retry_in", delay)
			return true, b.store.DeferDirty(ctx, d, b.now().Add(delay), err.Error())
		}
		b.log.Warn("reconcile failed; retrying later",
			"repository", d.Repository, "side", d.Side, "number", d.Number,
			"attempt", d.Attempts+1, "retry_in", delay, "error", err)
		return true, b.store.DeferDirty(ctx, d, b.now().Add(delay), truncate(err.Error(), 1000))
	}
	if _, err := b.store.ClearDirty(ctx, d); err != nil {
		return true, err
	}
	return true, nil
}

func backoff(attempts int) time.Duration {
	d := backoffBase
	for i := 0; i < attempts && d < backoffMax; i++ {
		d *= 2
	}
	return min(d, backoffMax)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func hasLabel(labels []string, name string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

func isNotFound(err error) bool { return errors.Is(err, forge.ErrNotFound) }

// own reports whether login is one of the bridge's own accounts on a side.
func (b *Bridge) own(side store.Side, login string) bool {
	if side == store.GitHub {
		return login == b.githubSelf
	}
	return login == b.forgejoSelf || (b.contribSelf != "" && login == b.contribSelf)
}
