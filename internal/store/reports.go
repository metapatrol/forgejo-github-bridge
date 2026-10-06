package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Reports for a pair that does not exist.
var ErrNotFound = errors.New("not found")

// Reports is what the console reads (docs/console.md). It changes nothing:
// the PostgreSQL implementation runs every query in a read-only transaction.
//
// It is separate from Store because nothing in the bridge needs it, and
// because a console can run with a database role that may only read.
type Reports interface {
	// Leader describes the instance holding the leader lock.
	Leader(ctx context.Context) (Leader, error)
	// QueueSummary describes the queue.
	QueueSummary(ctx context.Context) (QueueStats, error)
	// RepositorySummaries returns what is held for each repository that has
	// anything; a configured repository without pairs, cursors or queue
	// entries is absent.
	RepositorySummaries(ctx context.Context) (map[string]RepositorySummary, error)
	// Pairs lists a page of pairs, newest first, counts all that match, and
	// says where the next page starts; nil when there is none.
	Pairs(ctx context.Context, f PairFilter) ([]PairReport, int, *Position, error)
	// Pair returns one pair. ErrNotFound if there is none.
	Pair(ctx context.Context, id int64) (PairReport, error)
	// CommentPairs lists the comment pairs of a pair.
	CommentPairs(ctx context.Context, id int64) ([]CommentReport, error)
	// Queue lists a page of queued entries, failing ones first, then by when
	// they are due, counts all that match, and says where the next page
	// starts.
	Queue(ctx context.Context, f QueueFilter) ([]QueueReport, int, *Position, error)
	// System describes the database, the bridge's tables and their upkeep.
	System(ctx context.Context) (System, error)
	// RateLimits lists the GitHub rate-limit windows as last saved.
	RateLimits(ctx context.Context) ([]RateLimit, error)
	// Activity lists a page of what the bridge wrote, the newest first unless
	// sorted otherwise, counts all that match, and says where the next page
	// starts.
	Activity(ctx context.Context, f ActivityFilter) ([]Activity, int, *Position, error)
	// Authors lists a page of who opened pairs, the most active first, counts
	// all that match, and says where the next page starts.
	Authors(ctx context.Context, f AuthorFilter) ([]Author, int, *Position, error)
}

// Author is someone who opened pairs: a GitHub login for pairs that came
// from GitHub, a Forgejo login for issues published from Forgejo.
type Author struct {
	Login  string
	Side   Side
	Issues int
	Pulls  int
	// Last is when the newest of their pairs was made.
	Last time.Time
}

// Leader is the instance doing the work.
type Leader struct {
	Present bool
	// Instance is its application name with the "forgejo-github-bridge@"
	// prefix removed, its host name; empty when this session may not see it.
	Instance    string
	ConnectedAt time.Time
}

// RepositorySummary is what is held for one repository.
type RepositorySummary struct {
	Counts  PairCounts
	Queue   QueueStats
	Cursors []CursorReport
}

// PairCounts counts a repository's pairs by kind and state.
type PairCounts struct {
	IssuesOpen, IssuesClosed            int
	PullsOpen, PullsMerged, PullsClosed int
}

// CursorReport is one poll cursor.
type CursorReport struct {
	Side      Side
	Stream    Stream
	Cursor    time.Time
	UpdatedAt time.Time
}

// PairState is what the console calls a pair's state: open and closed for
// every pair, merged for a merged pull request.
type PairState string

// The pair states.
const (
	StateOpen   PairState = "open"
	StateClosed PairState = "closed"
	StateMerged PairState = "merged"
)

// PairFilter narrows a list of pairs. Zero values match everything.
type PairFilter struct {
	Repository string
	Kind       Kind
	State      PairState
	Origin     Side
	// Author is who opened it, on the origin side.
	Author string
	// Query matches the title, case-insensitively, or a number on either
	// side.
	Query string
	Page
}

// Page is how a list is sorted and cut.
type Page struct {
	// Sort is a column, descending with a leading "-"; empty for the list's
	// default. An unknown column is ErrInvalid.
	Sort string
	// Limit is the page size; zero for every row.
	Limit int
	// After, when set, is where the page starts: the position of the last
	// row of the page before.
	After *Position
}

// ErrInvalid is returned for a sort or a position a list does not know.
var ErrInvalid = errors.New("invalid")

// PairReport is a pair as the console shows it.
type PairReport struct {
	IssueMapping
	State     PairState
	Comments  int
	CreatedAt time.Time
	UpdatedAt time.Time
	// Pull is set for a pull request pair whose state is known.
	Pull *PullReport
	// Queued is set when the pair has an entry in the queue.
	Queued *QueueReport
}

// PullReport is a pull request pair's state.
type PullReport struct {
	PullRequestState
	UpdatedAt time.Time
}

// CommentReport is a comment pair.
type CommentReport struct {
	CommentMapping
	CreatedAt time.Time
}

// QueueFilter narrows the queue.
type QueueFilter struct {
	Repository  string
	FailingOnly bool
	// Query matches the repository or the last error, case-insensitively,
	// or the number.
	Query string
	Page
}

// ActivityFilter narrows the activity. Zero values match everything.
type ActivityFilter struct {
	Repository string
	PairID     int64
	Side       Side
	Kind       string
	Action     string
	Page
}

// AuthorFilter narrows the list of authors.
type AuthorFilter struct {
	// Query matches the login, case-insensitively.
	Query string
	// Side keeps the authors of one side; Kind those who opened at least one
	// pair of that kind. Their counts stay whole.
	Side Side
	Kind Kind
	Page
}

// QueueReport is a queued entry.
type QueueReport struct {
	DirtyKey
	MarkedAt  time.Time
	NotBefore time.Time
	Attempts  int
	LastError string
	// PairID is the pair the entry belongs to; zero for an issue that is not
	// paired yet. Kind is that pair's.
	PairID int64
	Kind   Kind
}

// System is how the database is doing and how it is kept: what the console's
// system page shows. All figures are PostgreSQL's.
type System struct {
	Database   SystemDatabase
	Tables     []SystemTable
	Migrations []Migration
	// Running lists the maintenance statements PostgreSQL runs now,
	// autovacuum included.
	Running []string
	// Rounds are the bridge's own maintenance rounds, the newest first.
	Rounds []MaintenanceRound
}

// MaintenanceRound is one database maintenance round of the bridge.
type MaintenanceRound struct {
	StartedAt time.Time
	// FinishedAt is zero while the round runs.
	FinishedAt                         time.Time
	Tables, Rebuilt, Dropped, Failures []string
}

// SystemDatabase describes the database and the connections to it.
type SystemDatabase struct {
	Version                      string
	SizeBytes                    int64
	Connections, MaxConnections  int64
	PoolSize, PoolInUse, PoolMax int64
	BlocksHit, BlocksRead        int64
	Deadlocks, TempBytes         int64
	XIDAge, FreezeMaxAge         int64
	StatsReset                   time.Time
	Settings                     []Setting
}

// Setting is one server setting.
type Setting struct{ Name, Value string }

// SystemTable describes one of the bridge's tables and how autovacuum keeps
// it.
type SystemTable struct {
	Name                              string
	TotalBytes, HeapBytes, IndexBytes int64
	LiveRows, DeadRows                int64
	ModifiedSinceAnalyze              int64
	// VacuumThreshold and AnalyzeThreshold are the dead and changed rows at
	// which autovacuum vacuums or analyzes the table.
	VacuumThreshold, AnalyzeThreshold int64
	LastVacuum, LastAutovacuum        time.Time
	LastAnalyze, LastAutoanalyze      time.Time
	Indexes                           []SystemIndex
}

// SystemIndex describes an index.
type SystemIndex struct {
	Name, Method string
	Bytes, Scans int64
	Valid        bool
}

// Migration is an applied schema migration.
type Migration struct {
	Name      string
	AppliedAt time.Time
}
