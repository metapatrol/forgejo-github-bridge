package memory

import (
	"testing"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return New() })
}
