package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

// provisionContrib gives every repository that imports pull requests its
// contrib fork (docs/design.md §11): the configured one, or the contrib
// account's fork under the canonical repository's name, created when it is
// missing. Every fork is checked before the bridge pushes anything into it.
func (b *Bridge) provisionContrib(ctx context.Context) error {
	claimed := map[string]string{}
	for _, key := range b.order {
		r := b.repos[key]
		if !r.cfg.ImportsPullRequests() {
			continue
		}
		if r.cfg.Contrib.Empty() {
			r.cfg.Contrib = config.RepoRef{Owner: b.contribSelf, Name: r.cfg.Forgejo.Name}
		}
		// Two canonical repositories of the same name in different
		// organisations would default to the same fork. Configuration
		// validation cannot see this: it does not know the contrib login.
		fork := strings.ToLower(r.cfg.Contrib.String())
		if other, ok := claimed[fork]; ok {
			return fmt.Errorf("%s and %s would both import pull requests into %s; set contrib for one of them",
				other, r.cfg.Forgejo, r.cfg.Contrib)
		}
		claimed[fork] = r.cfg.Forgejo.String()
		if err := b.ensureContrib(ctx, r); err != nil {
			return fmt.Errorf("%s: %w", r.cfg.Key(), err)
		}
	}
	return nil
}

func (b *Bridge) ensureContrib(ctx context.Context, r *repo) error {
	if err := b.checkContribRights(ctx, r); err != nil {
		return err
	}
	fork, err := b.pulls.Contrib.Repository(ctx, r.cfg.Contrib)
	switch {
	case isNotFound(err) && r.cfg.Contrib.Owner == b.contribSelf:
		if fork, err = b.pulls.Contrib.Fork(ctx, r.cfg.Forgejo, r.cfg.Contrib.Name); err != nil {
			return fmt.Errorf("forking %s into %s: %w", r.cfg.Forgejo, r.cfg.Contrib, err)
		}
		b.log.Info("created the contrib fork", "repository", r.cfg.Key(), "fork", r.cfg.Contrib)
	case isNotFound(err):
		return fmt.Errorf("the contrib repository %s does not exist, and the bridge only creates forks in %s's own namespace",
			r.cfg.Contrib, b.contribSelf)
	case err != nil:
		return fmt.Errorf("reading the contrib repository %s: %w", r.cfg.Contrib, err)
	}
	if !fork.Fork || !strings.EqualFold(fork.Parent, r.cfg.Forgejo.String()) {
		return fmt.Errorf("%s is not a fork of %s; pull requests from it would not be treated as untrusted", r.cfg.Contrib, r.cfg.Forgejo)
	}
	if !fork.CanPush {
		return fmt.Errorf("%s cannot push to the contrib repository %s", b.contribSelf, r.cfg.Contrib)
	}
	// Everything the bridge pushes into the fork is a stranger's code; a
	// push mirror would carry it somewhere nobody reviewed it for.
	n, err := b.pulls.Contrib.PushMirrors(ctx, r.cfg.Contrib)
	if err != nil {
		return fmt.Errorf("reading the push mirrors of %s: %w", r.cfg.Contrib, err)
	}
	if n > 0 {
		return fmt.Errorf("the contrib repository %s has a push mirror; remove it", r.cfg.Contrib)
	}
	return nil
}

// errContribTrusted is returned when the contrib account may write to the
// canonical repository.
var errContribTrusted = errors.New("the contrib account has write access to the canonical repository")

// checkContribRights refuses when the contrib account can write to the
// canonical repository. Forgejo runs the workflows of a pull request whose
// author has write access without approval (docs/design.md §12), so this is
// what keeps a stranger's code off the CI until a maintainer has read it.
func (b *Bridge) checkContribRights(ctx context.Context, r *repo) error {
	canonical, err := b.pulls.Contrib.Repository(ctx, r.cfg.Forgejo)
	if err != nil {
		return fmt.Errorf("reading %s as %s: %w", r.cfg.Forgejo, b.contribSelf, err)
	}
	if canonical.CanPush || canonical.IsAdmin {
		return fmt.Errorf("%w: %s on %s; give it read access only", errContribTrusted, b.contribSelf, r.cfg.Forgejo)
	}
	return nil
}
