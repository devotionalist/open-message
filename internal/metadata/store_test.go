package metadata_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
)

func TestOwnershipAcquireRenewUnload(t *testing.T) {
	clk := &clock.Fixed{Ms: 1_000_000}
	s := &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clk}
	ctx := context.Background()

	rec, _, err := s.TryAcquire(ctx, "ns", "b", "broker-a", "a:1", 15_000)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Epoch != 1 {
		t.Fatalf("epoch %d", rec.Epoch)
	}
	_, _, err = s.TryAcquire(ctx, "ns", "b", "broker-b", "b:1", 15_000)
	if err != metadata.ErrCasRejected {
		t.Fatalf("want reject got %v", err)
	}
	if _, err := s.Renew(ctx, "ns", "b", "broker-a", 1, 15_000); err != nil {
		t.Fatal(err)
	}
	clk.Advance(20 * time.Second)
	rec2, _, err := s.TryAcquire(ctx, "ns", "b", "broker-b", "b:1", 15_000)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Epoch != 2 {
		t.Fatalf("epoch after expire %d", rec2.Epoch)
	}
	if err := s.Unload(ctx, "ns", "b", "broker-b", 2); err != nil {
		t.Fatal(err)
	}
}

func TestCursorNoRegress(t *testing.T) {
	s := &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}
	ctx := context.Background()
	if err := s.CasAdvance(ctx, "t", "n", "tp", "sub", metadata.Position{LedgerID: 1, EntryID: 5}); err != nil {
		t.Fatal(err)
	}
	if err := s.CasAdvance(ctx, "t", "n", "tp", "sub", metadata.Position{LedgerID: 1, EntryID: 3}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetCursor(ctx, "t", "n", "tp", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Value.MarkDelete.EntryID != 5 {
		t.Fatalf("regressed to %d", rec.Value.MarkDelete.EntryID)
	}
}
