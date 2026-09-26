package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge/fake"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/memory"
)

var defaultFork = config.RepoRef{Owner: contribUser, Name: "foo"}

// contribInit builds a bridge over repos with pull requests enabled, with
// prepare run against the Forgejo fake first, and returns what Init says.
func contribInit(t *testing.T, repos []config.Repository, prepare func(fj *fake.Forge)) (*fake.Forge, error) {
	t.Helper()
	gh, fj := fake.New(appBot, 41), fake.New(bridgeUser, 71)
	if prepare != nil {
		prepare(fj)
	}
	for i := range repos {
		repos[i].TargetBranches = []string{"develop"}
		repos[i].GitHub = config.RepoRef{Owner: "metapatrol", Name: repos[i].Forgejo.Owner + "-" + repos[i].Forgejo.Name}
	}
	b := New(memory.New(), gh, fj, repos, Options{PollInterval: time.Minute, FullResync: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.WithPullRequests(Pulls{GitHub: gh, Forgejo: fj, Contrib: fj.As(contribUser), Git: &fake.Git{GitHub: gh, Forgejo: fj}})
	return fj, b.Init(context.Background())
}

func TestTheContribForkIsCreatedOnce(t *testing.T) {
	p := newPullHarness(t, nil)
	if n := p.fj.Forks(); n != 1 {
		t.Fatalf("%d forks", n)
	}
	info, err := p.fj.As(contribUser).Repository(context.Background(), defaultFork)
	if err != nil || info.Parent != fjRepo.String() {
		t.Fatalf("fork = %+v, %v", info, err)
	}
	p.restart()
	if n := p.fj.Forks(); n != 1 {
		t.Errorf("%d forks after a restart; the existing one must be reused", n)
	}
}

func TestAConfiguredContribRepositoryIsUsed(t *testing.T) {
	fork := config.RepoRef{Owner: contribUser, Name: "foo-contrib"}
	fj, err := contribInit(t, []config.Repository{{Forgejo: fjRepo, Contrib: fork}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := fj.As(contribUser).Repository(context.Background(), fork); err != nil || !info.Fork {
		t.Errorf("fork = %+v, %v", info, err)
	}
}

func TestContribRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		repos   []config.Repository
		prepare func(fj *fake.Forge)
		want    string
	}{
		{
			name:    "write access to the canonical repository",
			repos:   []config.Repository{{Forgejo: fjRepo}},
			prepare: func(fj *fake.Forge) { fj.Grant(fjRepo, contribUser, true, false) },
			want:    "read access only",
		},
		{
			name:    "admin on the canonical repository",
			repos:   []config.Repository{{Forgejo: fjRepo}},
			prepare: func(fj *fake.Forge) { fj.Grant(fjRepo, contribUser, false, true) },
			want:    "read access only",
		},
		{
			name:    "a repository of that name that is not a fork",
			repos:   []config.Repository{{Forgejo: fjRepo}},
			prepare: func(fj *fake.Forge) { fj.AddRepository(defaultFork, forge.RepoInfo{}) },
			want:    "is not a fork of platform/foo",
		},
		{
			name:    "a fork of something else",
			repos:   []config.Repository{{Forgejo: fjRepo}},
			prepare: func(fj *fake.Forge) { fj.AddRepository(defaultFork, forge.RepoInfo{Fork: true, Parent: "other/foo"}) },
			want:    "is not a fork of platform/foo",
		},
		{
			name:  "a push mirror on the fork",
			repos: []config.Repository{{Forgejo: fjRepo}},
			prepare: func(fj *fake.Forge) {
				fj.AddRepository(defaultFork, forge.RepoInfo{Fork: true, Parent: "platform/foo"})
				fj.AddPushMirror(defaultFork)
			},
			want: "push mirror",
		},
		{
			name:  "two repositories of the same name",
			repos: []config.Repository{{Forgejo: fjRepo}, {Forgejo: config.RepoRef{Owner: "tools", Name: "foo"}}},
			want:  "set contrib for one of them",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := contribInit(t, tc.repos, tc.prepare)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Init = %v; want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestTwoRepositoriesOfTheSameNameWithAnExplicitContrib(t *testing.T) {
	other := config.RepoRef{Owner: "tools", Name: "foo"}
	_, err := contribInit(t, []config.Repository{{Forgejo: fjRepo}, {Forgejo: other, Contrib: config.RepoRef{Owner: contribUser, Name: "tools-foo"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNothingIsImportedOnceTheContribAccountCanWrite(t *testing.T) {
	p := newPullHarness(t, nil)
	p.fj.Grant(fjRepo, contribUser, true, false)
	p.gh.OpenPull(ghRepo, "alice", "Fix the parser", "", "develop", "sha1", "parser.go")
	p.stepAll()
	for _, is := range p.fj.Issues(fjRepo) {
		if is.IsPullRequest {
			t.Fatalf("imported #%d although %s can write to %s", is.Number, contribUser, fjRepo)
		}
	}
	if _, ok := p.fj.Branch("pr/1"); ok {
		t.Error("the contribution was pushed although it will not be imported")
	}
	if err := p.b.checkContribRights(context.Background(), p.b.repos[ghRepo.String()]); !errors.Is(err, errContribTrusted) {
		t.Errorf("checkContribRights = %v", err)
	}
}
