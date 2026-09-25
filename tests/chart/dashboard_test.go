// Package chart checks the Grafana dashboard the dashboard chart ships.
//
// A dashboard is the one artefact in this repository whose mistakes are
// silent. A panel querying a metric that does not exist renders an empty
// graph for ever and looks exactly like a bridge with nothing to do. Nothing
// about deploying it fails, so the check has to happen here.
package chart

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	dashboardPath = "../../charts/forgejo-github-bridge-grafana-dashboard/dashboards/forgejo-github-bridge.json"
	metricsSource = "../../internal/telemetry/metrics.go"
)

type dashboard struct {
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Panels []struct {
		Type       string `json:"type"`
		Title      string `json:"title"`
		Datasource *struct {
			UID string `json:"uid"`
		} `json:"datasource"`
		GridPos struct {
			H, W, X, Y int
		} `json:"gridPos"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
	Templating struct {
		List []struct {
			Name  string `json:"name"`
			Query any    `json:"query"`
		} `json:"list"`
	} `json:"templating"`
}

func load(t *testing.T) dashboard {
	t.Helper()
	raw, err := os.ReadFile(dashboardPath)
	if err != nil {
		t.Fatal(err)
	}
	var d dashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("the committed dashboard is not valid JSON: %v", err)
	}
	return d
}

// declaredMetrics reads the names the bridge publishes, from the source: a
// vector with no children yet is registered but reports nothing, so gathering
// would list only whichever metrics this test happened to touch.
func declaredMetrics(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(metricsSource)
	if err != nil {
		t.Fatal(err)
	}
	ns := regexp.MustCompile(`namespace\s*=\s*"([^"]+)"`).FindSubmatch(src)
	if ns == nil {
		t.Fatal("no metrics namespace found")
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`Name:\s*"([a-z0-9_]+)"`).FindAllSubmatch(src, -1) {
		names[string(ns[1])+"_"+string(m[1])] = true
	}
	if len(names) == 0 {
		t.Fatal("no metrics found")
	}
	return names
}

// queriedMetrics reads every metric the dashboard's PromQL names, in panels
// and in variables: a repository selector built on a metric that does not
// exist leaves every panel unfilterable.
func queriedMetrics(t *testing.T, d dashboard) map[string]bool {
	t.Helper()
	var promQL []string
	for _, p := range d.Panels {
		for _, target := range p.Targets {
			promQL = append(promQL, target.Expr)
		}
	}
	for _, v := range d.Templating.List {
		if q, ok := v.Query.(string); ok {
			promQL = append(promQL, q)
		}
	}
	metric := regexp.MustCompile(`\bforgejo_github_bridge_[a-z0-9_]+`)
	names := map[string]bool{}
	for _, expr := range promQL {
		for _, found := range metric.FindAllString(expr, -1) {
			// Histograms are queried through the series Prometheus derives.
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				found = strings.TrimSuffix(found, suffix)
			}
			names[found] = true
		}
	}
	return names
}

func TestDashboardOnlyQueriesMetricsThatExist(t *testing.T) {
	d := load(t)
	declared := declaredMetrics(t)
	var unknown []string
	for name := range queriedMetrics(t, d) {
		if !declared[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Errorf("the dashboard queries metrics the bridge does not publish; those panels would stay empty: %v", unknown)
	}
}

// The other direction: a metric added without a panel is invisible, which is
// the same failure and even easier to miss.
func TestEveryMetricAppearsOnTheDashboard(t *testing.T) {
	d := load(t)
	queried := queriedMetrics(t, d)
	var missing []string
	for name := range declaredMetrics(t) {
		if !queried[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these metrics are published but appear on no panel: %v", missing)
	}
}

func TestDashboardIsWellFormed(t *testing.T) {
	d := load(t)
	if d.UID != "forgejo-github-bridge" {
		t.Errorf("uid = %q; a stable uid is what keeps links to the dashboard working", d.UID)
	}
	if d.Title == "" || len(d.Panels) == 0 {
		t.Fatal("the dashboard has no title or no panels")
	}
	for _, p := range d.Panels {
		if p.Title == "" {
			t.Error("an untitled panel says nothing about what it shows")
		}
		if p.GridPos.X+p.GridPos.W > 24 {
			t.Errorf("panel %q runs past the right edge of the 24-column grid", p.Title)
		}
		if p.Type == "row" {
			continue
		}
		if p.Datasource == nil || p.Datasource.UID != "${datasource}" {
			t.Errorf("panel %q does not use the datasource variable, which breaks on every other installation", p.Title)
		}
		if len(p.Targets) == 0 {
			t.Errorf("panel %q has no query", p.Title)
		}
	}
}

// Panels in one row must not overlap: Grafana moves overlapping panels
// around, and the layout that was reviewed is not the one people see.
func TestPanelsDoNotOverlap(t *testing.T) {
	d := load(t)
	type cell struct{ x, y int }
	taken := map[cell]string{}
	for _, p := range d.Panels {
		for dx := 0; dx < p.GridPos.W; dx++ {
			for dy := 0; dy < p.GridPos.H; dy++ {
				c := cell{p.GridPos.X + dx, p.GridPos.Y + dy}
				if other, ok := taken[c]; ok {
					t.Fatalf("panel %q overlaps %q at %v", p.Title, other, c)
				}
				taken[c] = p.Title
			}
		}
	}
}
