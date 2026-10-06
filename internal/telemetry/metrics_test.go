package telemetry

import (
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// testutil is the one helper these tests need from client_golang's testutil
// package, which would add dependencies for nothing else.
var testutil = struct {
	ToFloat64 func(prometheus.Metric) float64
}{
	ToFloat64: func(m prometheus.Metric) float64 {
		var out dto.Metric
		if err := m.Write(&out); err != nil {
			panic(err)
		}
		switch {
		case out.Gauge != nil:
			return out.Gauge.GetValue()
		case out.Counter != nil:
			return out.Counter.GetValue()
		}
		return 0
	},
}

func TestRateLimitWindowsAreReadFromResponses(t *testing.T) {
	for _, resource := range []string{"core", "search"} {
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("X-RateLimit-Resource", resource)
		resp.Header.Set("X-RateLimit-Limit", "5000")
		resp.Header.Set("X-RateLimit-Remaining", "4321")
		resp.Header.Set("X-RateLimit-Used", "679")
		resp.Header.Set("X-RateLimit-Reset", "1800000000")
		ObserveGitHub(resp)
	}
	if v := testutil.ToFloat64(rateWindowRemaining.WithLabelValues("search")); v != 4321 {
		t.Errorf("remaining = %v", v)
	}
	if v := testutil.ToFloat64(rateWindowLimit.WithLabelValues("search")); v != 5000 {
		t.Errorf("limit = %v", v)
	}
	if v := testutil.ToFloat64(rateWindowReset.WithLabelValues("core")); v != 1800000000 {
		t.Errorf("reset = %v", v)
	}
	if v := testutil.ToFloat64(rateRemaining); v != 4321 {
		t.Errorf("the core gauge = %v", v)
	}
}

func TestMaintenanceRounds(t *testing.T) {
	Register(nil)
	RecordMaintenance(2*time.Second, 0)
	RecordMaintenance(time.Second, 3)
	if ok, failed := testutil.ToFloat64(maintenanceRounds.WithLabelValues("ok")), testutil.ToFloat64(maintenanceRounds.WithLabelValues("failed")); ok != 1 || failed != 1 {
		t.Errorf("rounds ok %v, failed %v", ok, failed)
	}
	if v := testutil.ToFloat64(maintenanceDuration); v != 1 {
		t.Errorf("duration = %v", v)
	}
	at := time.Unix(1_800_000_000, 0)
	SetLastMaintenance(at)
	if v := testutil.ToFloat64(maintenanceLast); v != 1_800_000_000 {
		t.Errorf("last = %v", v)
	}
	SetMaintenanceInterval(24 * time.Hour)
	if v := testutil.ToFloat64(maintenanceInterval); v != 86400 {
		t.Errorf("interval = %v", v)
	}
}
