package bridge

import (
	"context"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
)

// pairRef names what a write belongs to: the pair, and the numbers on both
// sides as far as they are known.
type pairRef struct {
	id, github, forgejo int64
}

func refOf(m store.IssueMapping) pairRef {
	return pairRef{id: m.ID, github: m.GitHubNumber, forgejo: m.ForgejoNumber}
}

// changed counts a write the bridge made, and records it for the console's
// activity (docs/console.md). The record is best effort: a write that
// happened is not undone, nor retried, because its record could not be
// kept.
func (b *Bridge) changed(ctx context.Context, r *repo, side store.Side, kind, action string, ref pairRef) {
	telemetry.RecordChange(r.cfg.Key(), string(side), kind, action)
	if err := b.store.RecordActivity(ctx, store.Activity{
		Repository: r.cfg.Key(), PairID: ref.id, Side: side, Kind: kind, Action: action,
		GitHubNumber: ref.github, ForgejoNumber: ref.forgejo,
	}); err != nil {
		b.log.Warn("recording the bridge's activity failed", "repository", r.cfg.Key(), "kind", kind, "action", action, "error", err)
	}
}
