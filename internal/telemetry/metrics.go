// Package telemetry holds the bridge's Prometheus metrics.
//
// The set is what CONCEPT.md §18 names: whether polls work, how long the queue
// is and how old its oldest entry, what reconciles end in, and how much of
// the GitHub rate limit is left. Together they answer the one question an
// operator has about a bridge — is anything stuck, and where.
package telemetry

import (
	"net/http"
	"strconv"
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
)

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

// ObserveGitHub reads the rate-limit header from a GitHub response.
func ObserveGitHub(resp *http.Response) {
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			rateRemaining.Set(n)
		}
	}
}
