// Package events carries change notifications from PostgreSQL to the
// console's live views (GET /api/v1/events, docs/console.md).
//
// The triggers of migration 0005 announce every change to a pair, the queue
// or a poll cursor on one channel. A hub holds one connection that listens on
// it and fans each announcement out to its subscribers. Because the
// announcement travels through the database, a console hears the work of
// whichever instance did it.
//
// The leader lock is the one thing no trigger can announce: it is a session,
// not a row. The hub checks it on a timer and announces a change.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// Channel is the notification channel the triggers announce on.
const Channel = "forgejo_github_bridge_events"

// Event is one change: its kind ("pair", "queue", "cursor", "leader",
// "resync") and the JSON document describing it.
type Event struct {
	Kind string
	Data []byte
}

// ResyncKind tells a subscriber that it missed events, because it was too
// slow or the connection to PostgreSQL was lost: it should reload what it
// shows.
const ResyncKind = "resync"

// LeaderKind announces that the instance holding the leader lock changed.
const LeaderKind = "leader"

// subscriberBuffer is how many events a subscriber may fall behind.
const subscriberBuffer = 256

// leaderCheck is how often the leader lock is looked at.
const leaderCheck = 10 * time.Second

// LeaderFunc reports who holds the leader lock.
type LeaderFunc func(ctx context.Context) (store.Leader, error)

// Hub listens on the channel and fans every event out to its subscribers.
type Hub struct {
	pool   *pgxpool.Pool
	leader LeaderFunc
	log    *slog.Logger

	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewHub returns a hub; Run starts it.
func NewHub(pool *pgxpool.Pool, leader LeaderFunc, log *slog.Logger) *Hub {
	return &Hub{pool: pool, leader: leader, log: log, subs: map[chan Event]struct{}{}}
}

// Subscribe returns a channel of events and a function that ends the
// subscription. The channel is closed when the hub closes.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Close ends every subscription, so that open event streams let the server
// shut down.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// Run listens until ctx ends, reconnecting with backoff. After a lost
// connection subscribers get a resync, since changes may have been missed
// meanwhile.
func (h *Hub) Run(ctx context.Context) {
	go h.watchLeader(ctx)
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := h.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("the console's event listener lost its connection", "error", err, "retry_in", backoff)
		h.Broadcast(Event{Kind: ResyncKind, Data: []byte(`{}`)})
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A connection that listened goes back to the pool only after UNLISTEN;
	// destroying it is simpler and the pool replaces it.
	defer func() { _ = conn.Hijack().Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var head struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(n.Payload), &head) != nil || head.Kind == "" {
			continue
		}
		h.Broadcast(Event{Kind: head.Kind, Data: []byte(n.Payload)})
	}
}

// watchLeader announces when the leader lock changes hands, is taken or is
// released.
func (h *Hub) watchLeader(ctx context.Context) {
	var last store.Leader
	first := true
	ticker := time.NewTicker(leaderCheck)
	defer ticker.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, leaderCheck)
		l, err := h.leader(checkCtx)
		cancel()
		if err == nil {
			if !first && (l.Present != last.Present || l.Instance != last.Instance || !l.ConnectedAt.Equal(last.ConnectedAt)) {
				data, _ := json.Marshal(map[string]any{"kind": LeaderKind, "present": l.Present})
				h.Broadcast(Event{Kind: LeaderKind, Data: data})
			}
			last, first = l, false
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Broadcast hands ev to every subscriber. A subscriber whose buffer is full
// gets a resync in place of its oldest event, and nothing is lost but
// detail: it reloads.
func (h *Hub) Broadcast(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- Event{Kind: ResyncKind, Data: []byte(`{}`)}:
			default:
			}
		}
	}
}
