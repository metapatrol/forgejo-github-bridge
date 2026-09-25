"""Generates forgejo-github-bridge.json, the dashboard this chart ships.

    make dashboard

Written as a generator rather than by hand because a Grafana panel is forty
lines of field configuration that has to be identical across all of them, and
because the interesting part of a dashboard — which metric, which labels, which
unit — is then the only part visible in the diff.

forgejo-github-bridge.json is the committed artefact and what the chart
embeds; this is how it is produced. tests/chart/dashboard_test.go reads the
committed JSON and fails if a panel queries a metric internal/telemetry does
not publish, if a metric is published with no panel, if a panel pins a
datasource, or if a panel runs past the edge of the grid. Those are the
mistakes a dashboard makes silently: an empty panel looks exactly like a
bridge with nothing to do.

Python because it is a build-time tool that never runs in CI; the checks that
do run there are Go tests over the committed output.
"""
import json

DS = {"type": "prometheus", "uid": "${datasource}"}
BASE = 'job=~"$job", namespace=~"$namespace"'
REPO = 'repository=~"$repository"'
RATE = "[$__rate_interval]"
P_ = "forgejo_github_bridge_"


def M(metric, extra=""):
    """metric{job=..., namespace=...[, extra]} — by concatenation, because a
    PromQL selector inside a Python f-string is three levels of braces."""
    return P_ + metric + "{" + BASE + (", " + extra if extra else "") + "}"


def rate(metric, by, extra=""):
    return "sum by (" + by + ") (rate(" + M(metric, extra) + RATE + "))"


def increase(metric, by, extra=""):
    return "sum by (" + by + ") (increase(" + M(metric, extra) + "[$__range]))"


def quantile(q, metric, by, extra=""):
    return ("histogram_quantile(" + q + ", sum by (" + by + ", le) (rate("
            + M(metric + "_bucket", extra) + RATE + ")))")


_id = [0]


def nid():
    _id[0] += 1
    return _id[0]


def tgt(expr, legend, ref):
    return {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": ref}


def thresholds(steps):
    return {"mode": "absolute", "steps": [{"color": c, "value": v} for c, v in steps]}


GREEN = [("green", None)]


def ts(title, x, y, w, h, targets, unit="short", desc="", stack=False, steps=None):
    return {
        "datasource": DS,
        "description": desc,
        "fieldConfig": {"defaults": {
            "color": {"mode": "palette-classic"},
            "custom": {
                "axisLabel": "", "axisPlacement": "auto", "barAlignment": 0,
                "drawStyle": "lines", "fillOpacity": 20 if stack else 10,
                "gradientMode": "none",
                "hideFrom": {"legend": False, "tooltip": False, "viz": False},
                "lineInterpolation": "linear", "lineWidth": 2, "pointSize": 5,
                "scaleDistribution": {"type": "linear"}, "showPoints": "never",
                "spanNulls": False,
                "stacking": {"group": "A", "mode": "normal" if stack else "none"},
                "thresholdsStyle": {"mode": "line" if steps else "off"},
            },
            "mappings": [], "min": 0,
            "thresholds": thresholds(steps or GREEN),
            "unit": unit,
        }, "overrides": []},
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "id": nid(),
        "options": {
            "legend": {"calcs": ["mean", "max"], "displayMode": "table", "placement": "bottom"},
            "tooltip": {"mode": "multi", "sort": "desc"},
        },
        "title": title, "type": "timeseries",
        "targets": [tgt(e, l, chr(65 + i)) for i, (e, l) in enumerate(targets)],
    }


def stat(title, x, y, w, h, expr, legend, unit="short", desc="", fields=None, steps=None, mappings=None):
    return {
        "datasource": DS,
        "description": desc,
        "fieldConfig": {"defaults": {
            "color": {"mode": "thresholds"}, "custom": {}, "mappings": mappings or [],
            "thresholds": thresholds(steps or GREEN),
            "unit": unit,
        }, "overrides": []},
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "id": nid(),
        "options": {
            "colorMode": "background" if (fields or steps) else "value",
            "graphMode": "none" if (fields or steps) else "area",
            "justifyMode": "center", "orientation": "auto",
            "reduceOptions": {"calcs": ["lastNotNull"], "fields": fields or "", "values": False},
            "textMode": "value_and_name" if fields else "auto",
        },
        "pluginVersion": "9.0.0",
        "targets": [tgt(expr, legend, "A")],
        "title": title, "type": "stat",
    }


def row(title, y):
    return {"collapsed": False, "gridPos": {"h": 1, "w": 24, "x": 0, "y": y},
            "id": nid(), "panels": [], "title": title, "type": "row"}


P, y = [], 0

# --- Overview ------------------------------------------------------------------
P.append(row("Overview", y)); y += 1
P.append(stat("Build", 0, y, 6, 4,
              "max by (version, commit) (" + M("build_info") + ")",
              "{{version}} ({{commit}})", fields="/^version$/",
              desc="The running version. Two values here means a rollout is in progress, or a second replica is waiting."))
P.append(stat("Active", 6, y, 3, 4, "sum(" + M("leader") + ")", "instances",
              desc="Instances holding the leader lock and doing the work. Exactly 1 is right; 0 means nothing crosses the bridge.",
              steps=[("red", None), ("green", 1), ("red", 2)]))
P.append(stat("Queued", 9, y, 3, 4, "sum(" + M("dirty_queue_depth") + ")", "queued",
              desc="Issues and pull requests waiting to be reconciled. Briefly above zero after every poll that saw a change."))
P.append(stat("Failing", 12, y, 3, 4, "sum(" + M("dirty_failing") + ")", "failing",
              desc="Queued resources whose last reconcile failed. A merge waiting for the push mirror counts for a minute; anything longer is stuck. The log line \"reconcile failed\" says why.",
              steps=[("green", None), ("orange", 1)]))
P.append(stat("Oldest wait", 15, y, 3, 4, "max(" + M("dirty_oldest_age_seconds") + ")", "oldest",
              unit="s", desc="How long the longest-waiting resource has been queued.",
              steps=[("green", None), ("orange", 600), ("red", 3600)]))
P.append(stat("Since last poll", 18, y, 3, 4,
              "time() - min(" + M("last_successful_poll_timestamp_seconds", REPO) + ")", "worst",
              unit="s", desc="Time since the least recent successful poll across the selected repositories and both sides. It should stay around the poll interval.",
              steps=[("green", None), ("orange", 300), ("red", 900)]))
P.append(stat("GitHub budget", 21, y, 3, 4, "min(" + M("github_rate_limit_remaining") + ")", "remaining",
              desc="Requests left in GitHub's current rate-limit window. Unchanged lists are answered from their ETag and cost nothing.",
              steps=[("red", None), ("orange", 500), ("green", 1500)]))
y += 4

# --- Polling -------------------------------------------------------------------
P.append(row("Polling", y)); y += 1
P.append(ts("Time since last successful poll", 0, y, 12, 8,
            [("time() - max by (repository, side) (" + M("last_successful_poll_timestamp_seconds", REPO) + ")",
              "{{repository}} {{side}}")],
            unit="s", steps=[("green", None), ("red", 900)],
            desc="A sawtooth with the poll interval as its height is healthy. A line that keeps rising is a side that cannot be polled; the log says \"poll failed\" with the reason."))
P.append(ts("Poll errors", 12, y, 6, 8,
            [(rate("poll_errors_total", "repository, side", REPO), "{{repository}} {{side}}")], unit="reqps",
            desc="Polls that failed: a forge unreachable, a token rejected, the rate limit exhausted. Retried at the next interval."))
P.append(ts("Poll duration (p95)", 18, y, 6, 8,
            [(quantile("0.95", "poll_duration_seconds", "repository, side", REPO), "{{repository}} {{side}}")],
            unit="s", desc="One poll of one side of one repository: two list requests, more pages when much changed."))
y += 8

# --- Queue ---------------------------------------------------------------------
P.append(row("Queue", y)); y += 1
P.append(ts("Queue", 0, y, 8, 8, [
    ("sum(" + M("dirty_queue_depth") + ")", "queued"),
    ("sum(" + M("dirty_failing") + ")", "failing"),
], desc="Both on one axis: failing is part of queued. Queued spikes and returns to zero; failing that stays up is something stuck."))
P.append(ts("Oldest wait", 8, y, 8, 8,
            [("max(" + M("dirty_oldest_age_seconds") + ")", "oldest")], unit="s",
            steps=[("green", None), ("orange", 600), ("red", 3600)],
            desc="Grows while something waits. Backoff tops out at 30 minutes, so a line well past that is a resource failing on every attempt."))
P.append(ts("Marked dirty", 16, y, 8, 8,
            [(rate("resources_marked_total", "source", REPO), "{{source}}")], unit="reqps", stack=True,
            desc="poll is changes seen on either forge; resync is the periodic pass over every open pair, which is what notices deletions."))
y += 8

# --- Reconciling -----------------------------------------------------------------
P.append(row("Reconciling", y)); y += 1
P.append(ts("Reconciles by outcome", 0, y, 12, 8,
            [(rate("reconciles_total", "outcome", REPO), "{{outcome}}")], unit="reqps", stack=True,
            desc="error includes waiting for the push mirror after a merge, which is expected and brief. Sustained errors show in Failing above."))
P.append(ts("Reconciles by repository", 12, y, 12, 8,
            [(rate("reconciles_total", "repository", REPO), "{{repository}}")], unit="reqps"))
y += 8

# --- What the bridge did ---------------------------------------------------------
P.append(row("What the bridge did", y)); y += 1
P.append(ts("Writes to GitHub", 0, y, 12, 8,
            [(rate("changes_total", "kind, action", REPO + ', side="github"'), "{{kind}} {{action}}")],
            unit="reqps", stack=True,
            desc="Everything the bridge wrote on GitHub: issues published, comments projected, notices, closes. A write without a matching change on the other side would be a loop."))
P.append(ts("Writes to Forgejo", 12, y, 12, 8,
            [(rate("changes_total", "kind, action", REPO + ', side="forgejo"'), "{{kind}} {{action}}")],
            unit="reqps", stack=True,
            desc="Everything the bridge wrote in Forgejo: imported issues and pull requests, pushed commits, labels, comments."))
y += 8
P.append(stat("Issues imported", 0, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', side="forgejo", kind="issue", action="create"') + ")) or vector(0)",
              "in range", desc="GitHub issues imported into Forgejo in the selected time range."))
P.append(stat("Issues published", 4, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', side="github", kind="issue", action="create"') + ")) or vector(0)",
              "in range", desc="Forgejo issues published to GitHub with the publish label, in the selected time range."))
P.append(stat("Pull requests imported", 8, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', kind="pull", action="create"') + ")) or vector(0)",
              "in range"))
P.append(stat("Commits followed", 12, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', kind="pull", action="push"') + ")) or vector(0)",
              "in range", desc="Pushes of new contributor commits into contrib branches."))
P.append(stat("Touching CI", 16, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', kind="pull", action="touches-ci"') + ")) or vector(0)",
              "in range", desc="Imported pull requests that change CI configuration. Each needs a careful read before its workflow run is approved.",
              steps=[("green", None), ("orange", 1)]))
P.append(stat("Comments crossed", 20, y, 4, 4,
              "(sum(" + increase("changes_total", "side", REPO + ', kind="comment", action="create"') + ")) or vector(0)",
              "in range"))
y += 4

# --- GitHub API ------------------------------------------------------------------
P.append(row("GitHub API", y)); y += 1
P.append(ts("Rate-limit budget", 0, y, 24, 6,
            [("min(" + M("github_rate_limit_remaining") + ")", "remaining")],
            steps=[("red", None), ("orange", 500), ("green", 1500)],
            desc="GitHub gives an App installation at least 5000 requests an hour. The budget refills every hour; a steady descent towards zero means polling too often or too many repositories for one installation."))
y += 6

dashboard = {
    "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "description": "",
                  "type": "datasource", "pluginId": "prometheus", "pluginName": "Prometheus"}],
    "__requires": [
        {"type": "grafana", "id": "grafana", "name": "Grafana", "version": "10.0.0"},
        {"type": "datasource", "id": "prometheus", "name": "Prometheus", "version": "1.0.0"},
        {"type": "panel", "id": "stat", "name": "Stat", "version": ""},
        {"type": "panel", "id": "timeseries", "name": "Time series", "version": ""},
        {"type": "panel", "id": "row", "name": "Row", "version": ""},
    ],
    "annotations": {"list": [{
        "builtIn": 1, "datasource": {"type": "grafana", "uid": "-- Grafana --"},
        "enable": True, "hide": True, "iconColor": "rgba(0, 211, 255, 1)",
        "name": "Annotations & Alerts", "type": "dashboard"}]},
    "description": "The bridge between the private Forgejo and GitHub: whether it is working, whether polls succeed, what is queued or stuck, and what it wrote on either side.",
    "editable": True, "fiscalYearStartMonth": 0, "graphTooltip": 1, "id": None, "links": [],
    "panels": P, "refresh": "30s", "schemaVersion": 38,
    "tags": ["forgejo", "github", "platform"],
    "templating": {"list": [
        {"current": {}, "hide": 0, "includeAll": False, "name": "datasource", "options": [],
         "query": "prometheus", "refresh": 1, "type": "datasource", "label": "Data Source"},
        {"current": {}, "datasource": DS, "definition": "label_values(" + P_ + "build_info, job)",
         "hide": 0, "includeAll": False, "name": "job", "options": [],
         "query": "label_values(" + P_ + "build_info, job)", "refresh": 2, "type": "query", "label": "Job"},
        {"current": {}, "datasource": DS,
         "definition": "label_values(" + P_ + 'build_info{job=~"$job"}, namespace)',
         "hide": 0, "includeAll": True, "multi": True, "name": "namespace", "options": [],
         "query": "label_values(" + P_ + 'build_info{job=~"$job"}, namespace)',
         "refresh": 2, "type": "query", "label": "Namespace"},
        {"current": {}, "datasource": DS,
         "definition": "label_values(" + P_ + 'last_successful_poll_timestamp_seconds{job=~"$job", namespace=~"$namespace"}, repository)',
         "hide": 0, "includeAll": True, "multi": True, "name": "repository", "options": [],
         "query": "label_values(" + P_ + 'last_successful_poll_timestamp_seconds{job=~"$job", namespace=~"$namespace"}, repository)',
         "refresh": 2, "type": "query", "label": "Repository"},
    ]},
    "time": {"from": "now-6h", "to": "now"},
    "timepicker": {}, "timezone": "", "title": "Forgejo GitHub Bridge", "uid": "forgejo-github-bridge", "version": 1,
}


def clean(o):
    if isinstance(o, dict):
        return {k: clean(v) for k, v in o.items() if not (k == "description" and v == "")}
    if isinstance(o, list):
        return [clean(v) for v in o]
    return o


print(json.dumps(clean(dashboard), indent=2))
