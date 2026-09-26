package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// leaderLockID is the advisory lock only the active instance holds. Like the
// migration lock it identifies this service to PostgreSQL and must never
// change.
const leaderLockID int64 = 0x464742524c454144 // "FGBRLEAD"

// How often a waiting replica retries, and how often the active one checks
// that the connection holding its lock is still there.
const (
	leaderRetry = 5 * time.Second
	leaderCheck = 10 * time.Second
)

// Lease is the leader lock, held on a dedicated connection.
type Lease struct {
	lost chan struct{}
}

// Lost is closed when the lock can no longer be vouched for: the connection
// holding it failed. PostgreSQL releases a session lock when its session
// ends, so from that moment another replica may be active, and this one must
// stop.
func (l *Lease) Lost() <-chan struct{} { return l.lost }

// AcquireLeader blocks until this process holds the leader lock or ctx ends.
//
// Only one instance may poll and reconcile at a time (docs/design.md §7): two
// would each see an unmapped issue and each create its projection. A second
// replica therefore waits here, passive, and takes over when the first one's
// session ends.
func AcquireLeader(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) (*Lease, error) {
	for {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("acquiring a connection for the leader lock: %w", err)
		}
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLockID).Scan(&got); err != nil {
			conn.Release()
			return nil, fmt.Errorf("trying the leader lock: %w", err)
		}
		if got {
			lease := &Lease{lost: make(chan struct{})}
			go func() {
				defer close(lease.lost)
				// The connection is never returned to the pool while it holds
				// the lock; a pooled connection would carry the lock into
				// somebody else's queries. Closing it on the way out is what
				// releases the lock.
				defer func() { _ = conn.Hijack().Close(context.Background()) }()
				ticker := time.NewTicker(leaderCheck)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						checkCtx, cancel := context.WithTimeout(ctx, leaderCheck)
						err := conn.Ping(checkCtx)
						cancel()
						if err != nil && ctx.Err() == nil {
							log.Error("the connection holding the leader lock failed; stopping work", "error", err)
							return
						}
					}
				}
			}()
			return lease, nil
		}
		conn.Release()

		log.Info("another instance is active; waiting for the leader lock", "retry", leaderRetry)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(leaderRetry):
		}
	}
}
