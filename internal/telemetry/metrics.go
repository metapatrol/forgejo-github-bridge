// Package telemetry holds the bridge's Prometheus metrics.
//
// The set is what docs/design.md §18 names: whether polls work, how long the queue
// is and how old its oldest entry, what reconciles end in, and how much of
// the GitHub rate limit is left. Together they answer the one question an
// operator has about a bridge — is anything stuck, and where.
package telemetry

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "forgejo_github_bridge"

var (
	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information; the value is always 1.",
	}, []string{"version", "commit", "built_at"})

	pollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "poll_duration_seconds",
		Help:      "Duration of one poll of one repository on one side.",
		Buckets:   prometheus.ExponentialBuckets(0.05, 2, 10),
	}, []string{"repository", "side"})

	pollErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "poll_errors_total",
		Help:      "Polls that failed.",
	}, []string{"repository", "side"})

	lastPoll = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_successful_poll_timestamp_seconds",
		Help:      "When a poll of the repository on the side last succeeded.",
	}, []string{"repository", "side"})

	marked = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "resources_marked_total",
		Help:      "Resources marked dirty by polls and resyncs.",
	}, []string{"repository", "source"})

	reconciles = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "reconciles_total",
		Help:      "Reconciles by outcome: ok, error.",
	}, []string{"repository", "outcome"})

	changes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "changes_total",
		Help:      "Writes the bridge made, by side, kind and action.",
	}, []string{"repository", "side", "kind", "action"})

	queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "dirty_queue_depth",
		Help:      "Resources waiting to be reconciled.",
	})

	queueFailing = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "dirty_failing",
		Help:      "Queued resources whose last reconcile failed and that wait for a retry. Above zero for long means something is stuck.",
	})

	queueOldest = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "dirty_oldest_age_seconds",
		Help:      "How long the oldest waiting resource has been waiting; zero when the queue is empty.",
	})

	rateRemaining = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "github_rate_limit_remaining",
		Help:      "Requests left in the current GitHub rate-limit window, as of the last response.",
	})

	leader = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "leader",
		Help:      "1 while this instance holds the leader lock and does the work.",
	})

	rateWindowRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "github_rate_limit_window_remaining",
		Help:      "Requests left in a GitHub rate-limit window of the App installation, by GitHub's resource name, as of the last response.",
	}, []string{"resource"})

	rateWindowLimit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "github_rate_limit_window_limit",
		Help:      "Requests a GitHub rate-limit window allows in all.",
	}, []string{"resource"})

	rateWindowReset = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "github_rate_limit_window_reset_timestamp_seconds",
		Help:      "When a GitHub rate-limit window starts over.",
	}, []string{"resource"})

	maintenanceInterval = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "db_maintenance_interval_seconds",
		Help:      "Time between two database maintenance rounds as configured; zero when they are off.",
	})

	maintenanceLast = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "db_maintenance_last_round_timestamp_seconds",
		Help:      "When the last database maintenance round finished, on any instance, as the active instance last read it; zero on an instance that is not active, or before the first round.",
	})

	maintenanceDuration = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "db_maintenance_round_duration_seconds",
		Help:      "How long this instance's last database maintenance round took.",
	})

	maintenanceRounds = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "db_maintenance_rounds_total",
		Help:      "Database maintenance rounds this instance ran, by outcome: ok, or failed when a table or index could not be kept.",
	}, []string{"outcome"})

	consoleSubscribers = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "console_event_subscribers",
		Help:      "Console event streams this instance serves now: the browsers watching it live.",
	})
)

// Register creates the series of the configured repositories at zero.
//
// A counter labelled by repository does not exist until the first event, and
// a dashboard panel over a series that does not exist says "No data" — which
// reads as broken, not as "no poll has failed yet".
func Register(repositories []string) {
	for _, r := range repositories {
		for _, side := range []string{"github", "forgejo"} {
			pollErrors.WithLabelValues(r, side)
			pollDuration.WithLabelValues(r, side)
		}
		for _, outcome := range []string{"ok", "error"} {
			reconciles.WithLabelValues(r, outcome)
		}
		for _, source := range []string{"poll", "resync"} {
			marked.WithLabelValues(r, source)
		}
	}
	// The REST API's window exists before the first response, so a panel or
	// an alert over it is not empty until then.
	rateWindowRemaining.WithLabelValues("core")
	rateWindowLimit.WithLabelValues("core")
	rateWindowReset.WithLabelValues("core")
	for _, outcome := range []string{"ok", "failed"} {
		maintenanceRounds.WithLabelValues(outcome)
	}
}

// SetBuildInfo records the build.
func SetBuildInfo(version, commit, builtAt string) {
	buildInfo.WithLabelValues(version, commit, builtAt).Set(1)
}

// RecordPoll records one poll.
func RecordPoll(repo, side string, start time.Time, err error) {
	pollDuration.WithLabelValues(repo, side).Observe(time.Since(start).Seconds())
	if err != nil {
		pollErrors.WithLabelValues(repo, side).Inc()
		return
	}
	lastPoll.WithLabelValues(repo, side).SetToCurrentTime()
}

// RecordMarked counts resources marked dirty.
func RecordMarked(repo, source string, n int) {
	marked.WithLabelValues(repo, source).Add(float64(n))
}

// RecordReconcile counts a reconcile.
func RecordReconcile(repo string, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	reconciles.WithLabelValues(repo, outcome).Inc()
}

// RecordChange counts one write to a forge.
func RecordChange(repo, side, kind, action string) {
	changes.WithLabelValues(repo, side, kind, action).Inc()
}

// SetQueue records the queue's state.
func SetQueue(depth, failing int, oldest time.Time) {
	queueDepth.Set(float64(depth))
	queueFailing.Set(float64(failing))
	if oldest.IsZero() {
		queueOldest.Set(0)
		return
	}
	queueOldest.Set(time.Since(oldest).Seconds())
}

// SetLeader records whether this instance is active.
func SetLeader(active bool) {
	if active {
		leader.Set(1)
	} else {
		leader.Set(0)
	}
}

// RateLimit is one GitHub rate-limit window, as the last response in it
// reported.
type RateLimit struct {
	// Resource is GitHub's name for the budget, "core" for the REST API.
	Resource         string
	Limit, Remaining int
	Used             int
	Reset            time.Time
	ObservedAt       time.Time
}

var (
	rateMu     sync.Mutex
	rateLimits = map[string]RateLimit{}
)

// ObserveGitHub reads the rate-limit headers from a GitHub response made with
// an installation token.
func ObserveGitHub(resp *http.Response) {
	h := resp.Header
	remaining, err := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	rl := RateLimit{Resource: h.Get("X-RateLimit-Resource"), Remaining: remaining, ObservedAt: time.Now()}
	if rl.Resource == "" {
		rl.Resource = "core"
	}
	rl.Limit, _ = strconv.Atoi(h.Get("X-RateLimit-Limit"))
	rl.Used, _ = strconv.Atoi(h.Get("X-RateLimit-Used"))
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		rl.Reset = time.Unix(reset, 0)
	}
	if rl.Resource == "core" {
		rateRemaining.Set(float64(remaining))
	}
	rateWindowRemaining.WithLabelValues(rl.Resource).Set(float64(remaining))
	if rl.Limit > 0 {
		rateWindowLimit.WithLabelValues(rl.Resource).Set(float64(rl.Limit))
	}
	if !rl.Reset.IsZero() {
		rateWindowReset.WithLabelValues(rl.Resource).Set(float64(rl.Reset.Unix()))
	}
	rateMu.Lock()
	rateLimits[rl.Resource] = rl
	rateMu.Unlock()
}

// RateLimits returns the last observation of every rate-limit window.
func RateLimits() []RateLimit {
	rateMu.Lock()
	defer rateMu.Unlock()
	out := make([]RateLimit, 0, len(rateLimits))
	for _, rl := range rateLimits {
		out = append(out, rl)
	}
	return out
}

// SetMaintenanceInterval records the configured time between database
// maintenance rounds; zero when they are off.
func SetMaintenanceInterval(d time.Duration) {
	maintenanceInterval.Set(d.Seconds())
}

// SetLastMaintenance records when the last database maintenance round
// finished, on any instance.
func SetLastMaintenance(at time.Time) {
	if at.IsZero() {
		maintenanceLast.Set(0)
		return
	}
	maintenanceLast.Set(float64(at.Unix()))
}

// RecordMaintenance records a database maintenance round this instance ran.
func RecordMaintenance(took time.Duration, failures int) {
	maintenanceDuration.Set(took.Seconds())
	if failures > 0 {
		maintenanceRounds.WithLabelValues("failed").Inc()
		return
	}
	maintenanceRounds.WithLabelValues("ok").Inc()
}

// SetConsoleSubscribers records how many console event streams are open.
func SetConsoleSubscribers(n int) {
	consoleSubscribers.Set(float64(n))
}
