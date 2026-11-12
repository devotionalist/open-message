package olclient_test

import (
	"context"
	"testing"

	"github.com/devotionalist/open-message/internal/olclient"
)

func TestEnsembleRotatesOnAdvance(t *testing.T) {
	ol := olclient.NewMemoryBookiePool(6, 3, 2, 2)
	ctx := context.Background()
	id1, err := ol.Create(ctx, olclient.QuorumPolicy{E: 3, Qw: 2, Qa: 2})
	if err != nil {
		t.Fatal(err)
	}
	ens1, ok := ol.EnsembleOf(id1)
	if !ok || len(ens1) != 3 {
		t.Fatalf("ens1=%v ok=%v", ens1, ok)
	}
	ol.AdvanceEnsemble(3)
	id2, err := ol.Create(ctx, olclient.QuorumPolicy{E: 3, Qw: 2, Qa: 2})
	if err != nil {
		t.Fatal(err)
	}
	ens2, ok := ol.EnsembleOf(id2)
	if !ok || len(ens2) != 3 {
		t.Fatalf("ens2=%v ok=%v", ens2, ok)
	}
	if ens1[0] == ens2[0] && ens1[1] == ens2[1] && ens1[2] == ens2[2] {
		t.Fatalf("ensemble did not rotate: %v vs %v", ens1, ens2)
	}
}

func TestRollAfterUnavailableUsesNewEnsemble(t *testing.T) {
	// Covered at ManagedLedger layer; here ensure Append after Advance+new Create works.
	ol := olclient.NewMemoryBookiePool(6, 3, 2, 2)
	ctx := context.Background()
	id, err := ol.Create(ctx, olclient.QuorumPolicy{E: 3, Qw: 2, Qa: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ol.Append(ctx, id, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := ol.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	ol.AdvanceEnsemble(3)
	id2, err := ol.Create(ctx, olclient.QuorumPolicy{E: 3, Qw: 2, Qa: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ol.Append(ctx, id2, []byte("b")); err != nil {
		t.Fatal(err)
	}
}
