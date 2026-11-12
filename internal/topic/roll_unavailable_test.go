package topic_test

import (
	"context"
	"testing"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
	"github.com/devotionalist/open-message/internal/topic"
)

func TestAppendTriggersRollWithEnsembleAdvance(t *testing.T) {
	meta := &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}
	ol := olclient.NewMemoryBookiePool(6, 3, 2, 2)
	ml := topic.NewManagedLedger("public", "default", "roll-unavail", meta, ol, olclient.QuorumPolicy{E: 3, Qw: 2, Qa: 2})
	ctx := context.Background()
	if err := ml.Load(ctx); err != nil {
		t.Fatal(err)
	}
	old, _ := ml.OpenLedgerID()
	ensOld, _ := ol.EnsembleOf(old)
	// Force roll path by fencing: close under the adapter then Append should roll.
	if _, err := ol.Close(ctx, old); err != nil {
		t.Fatal(err)
	}
	// Writer gone → Append returns Unavailable → ManagedLedger rolls.
	lid, _, err := ml.Append(ctx, []byte("after-seal"))
	if err != nil {
		t.Fatal(err)
	}
	if lid == old {
		t.Fatalf("expected new ledger after roll, still %d", lid)
	}
	ensNew, ok := ol.EnsembleOf(lid)
	if !ok {
		t.Fatal("missing new ensemble")
	}
	if len(ensOld) == 3 && ensOld[0] == ensNew[0] && ensOld[1] == ensNew[1] && ensOld[2] == ensNew[2] {
		t.Fatalf("expected rotated ensemble after unavailable roll: %v", ensNew)
	}
}
