// Command seed fills a development database with what a bridge that has been
// running for a while holds, so that the console can be looked at without
// one (make console-dev). It empties the database first, and refuses any
// database that is not on this machine.
//
// With -lead it then holds the leader lock until interrupted, so that the
// console shows an active instance.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/postgres"
)

func main() {
	lead := flag.Bool("lead", false, "hold the leader lock until interrupted")
	flag.Parse()
	if err := run(os.Getenv("BRIDGE_DATABASE_URL"), *lead); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(dsn string, lead bool) error {
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("BRIDGE_DATABASE_URL must point at a database on localhost; this empties it")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := postgres.Open(ctx, dsn, 4, quiet)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seed(ctx, st); err != nil {
		return err
	}
	fmt.Println("seeded")
	if !lead {
		return nil
	}
	if _, err := postgres.AcquireLeader(ctx, st.Pool(), quiet); err != nil {
		return err
	}
	fmt.Println("holding the leader lock; interrupt to release it")
	<-ctx.Done()
	return nil
}

type pair struct {
	repo, title, author string
	kind                store.Kind
	origin              store.Side
	closed              bool
	age                 time.Duration
	comments            int
	pull                *store.PullRequestState
}

var issues = []string{
	"Crash on start when the config file is empty",
	"Support GitLab as a second mirror target",
	"Document the required GitHub App permissions",
	"Rate limit warnings in the log after an upgrade",
	"Mentions of renamed users are not translated",
	"Comments edited on GitHub are not updated in Forgejo",
	"Helm chart: allow extra environment variables",
	"Attachments are replaced by a placeholder",
	"Roadmap for the next minor release",
	"Polling interval below ten seconds is refused",
	"Metrics endpoint returns 500 during startup",
	"Labels with spaces break the import filter",
	"Grafana dashboard: add a panel for the queue age",
	"Recover mappings after a database restore",
	"Typo in the installation guide",
	"Issue closed on GitHub is reopened by the bridge",
	"Support for Forgejo 17",
	"Long issue titles are cut in notifications",
}

var pulls = []string{
	"Fix the parser for nested code spans",
	"docs: clarify the contrib fork setup",
	"Add a Dutch translation of the messages",
	"chore(deps): bump golang.org/x/net",
	"ci: cache the Go build",
	"feat: configurable user agent",
	"Fix race in the poll window",
	"Update the logo in the README",
}

var authors = []string{"alice", "octocat", "bob-dev", "mschmidt", "jdoe", "renovate[bot]", "carol", "dkowalski"}

var repos = []struct {
	key   string
	pulls bool
}{
	{"metapatrol/forgejo-github-bridge", true},
	{"metapatrol/mcp-gateway", true},
	{"metapatrol/engram", false},
}

func seed(ctx context.Context, st *postgres.Store) error {
	if _, err := st.Pool().Exec(ctx, `TRUNCATE issue_mapping, comment_mapping, pull_request_state, poll_cursor, dirty`); err != nil {
		return err
	}
	var pairs []pair
	for ri, r := range repos {
		for i, title := range issues {
			if (i+ri)%3 == 2 {
				continue
			}
			origin := store.GitHub
			author := authors[(i+ri)%len(authors)]
			if i%5 == 4 {
				origin, author = store.Forgejo, "jdoe"
			}
			pairs = append(pairs, pair{repo: r.key, title: title, author: author, kind: store.KindIssue, origin: origin,
				closed: i%4 == 1, age: time.Duration(i*37+ri*11) * time.Hour, comments: (i * 3) % 7})
		}
		if !r.pulls {
			continue
		}
		for i, title := range pulls {
			if (i+ri)%4 == 3 {
				continue
			}
			pr := &store.PullRequestState{ContribBranch: "", ImportedHeadSHA: fmt.Sprintf("%07x%033x", 0x3a5f1c0+i*7919, i), State: store.PullOpen}
			switch i % 6 {
			case 0:
				pr.State, pr.OutcomeReported = store.PullMerged, true
			case 1:
				pr.RebaseRequested, pr.RebaseCommentID = true, int64(5000+i)
			case 2:
				pr.TouchesCI = true
			case 3:
				pr.HeadFrozen = true
			case 4:
				pr.State, pr.OutcomeReported = store.PullClosed, true
			}
			pairs = append(pairs, pair{repo: r.key, title: title, author: authors[(i+3)%len(authors)], kind: store.KindPull,
				origin: store.GitHub, closed: pr.State != store.PullOpen, age: time.Duration(i*29+ri*5+2) * time.Hour,
				comments: i % 4, pull: pr})
		}
	}

	numbers := map[string]int64{}
	commentID := int64(2_000_000)
	var firstFailing []store.DirtyKey
	for _, p := range pairs {
		numbers[p.repo]++
		gh, fj := numbers[p.repo], 100+numbers[p.repo]
		m, err := st.InsertIssue(ctx, store.IssueMapping{Repository: p.repo, Kind: p.kind, GitHubNumber: gh,
			ForgejoNumber: fj, Origin: p.origin, SyncedClosed: p.closed})
		if err != nil {
			return err
		}
		if err := st.SetIssueSummary(ctx, m.ID, p.title, p.author); err != nil {
			return err
		}
		created := time.Now().Add(-p.age)
		if _, err := st.Pool().Exec(ctx, `UPDATE issue_mapping SET created_at = $2, updated_at = $3 WHERE id = $1`,
			m.ID, created, created.Add(p.age/3)); err != nil {
			return err
		}
		if p.pull != nil {
			p.pull.IssueMappingID, p.pull.ContribBranch = m.ID, fmt.Sprintf("pr/%d", gh)
			if err := st.SavePullRequest(ctx, *p.pull); err != nil {
				return err
			}
		}
		for c := 0; c < p.comments; c++ {
			commentID++
			origin := store.GitHub
			if c%2 == 1 {
				origin = store.Forgejo
			}
			if _, err := st.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: commentID,
				ForgejoCommentID: commentID + 500_000, Origin: origin}); err != nil {
				return err
			}
		}
		if gh%9 == 4 {
			firstFailing = append(firstFailing, store.DirtyKey{Repository: p.repo, Side: store.GitHub, Number: gh})
		}
	}

	for _, r := range repos {
		for i, side := range []store.Side{store.GitHub, store.Forgejo} {
			for j, stream := range []store.Stream{store.Issues, store.Comments} {
				if err := st.SetCursor(ctx, r.key, side, stream, time.Now().Add(-time.Duration(i*3+j+1)*time.Minute)); err != nil {
					return err
				}
			}
		}
	}

	// A few entries due now, a few that keep failing with what the
	// operations guide lists as common causes, and one unpaired issue.
	errorsSeen := []string{
		"HTTP 403 from Forgejo: user does not have permission to edit this issue",
		"fetching GitHub pull request #12, anonymously and with the App's token: exit status 128",
		"branch pr/7 already exists in github-contrib/forgejo-github-bridge and was not made by the bridge; GitHub pull request #7 cannot be imported until it is removed: lease rejected",
		"waiting for the push mirror to bring a1b2c3d to GitHub",
	}
	keys := append([]store.DirtyKey{
		{Repository: "metapatrol/engram", Side: store.Forgejo, Number: 140},
		{Repository: "metapatrol/mcp-gateway", Side: store.GitHub, Number: 2},
	}, firstFailing...)
	if err := st.MarkDirty(ctx, keys...); err != nil {
		return err
	}
	for i, k := range firstFailing {
		if i >= len(errorsSeen) {
			break
		}
		if _, err := st.Pool().Exec(ctx, `
			UPDATE dirty SET attempts = $4, last_error = $5, not_before = now() + $6::interval,
			                 marked_at = now() - $7::interval
			WHERE repository = $1 AND side = $2 AND number = $3`,
			k.Repository, string(k.Side), k.Number, i+2, errorsSeen[i],
			fmt.Sprintf("%d minutes", (i+1)*4), fmt.Sprintf("%d minutes", (i+1)*23)); err != nil {
			return err
		}
	}
	return nil
}
