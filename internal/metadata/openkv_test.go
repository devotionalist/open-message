package metadata_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
)

func TestOpenKVStoreCAS(t *testing.T) {
	gw := os.Getenv("OPENKV_GATEWAY")
	if gw == "" {
		t.Skip("set OPENKV_GATEWAY to run (e.g. 127.0.0.1:19000 after port-forward)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := metadata.NewOpenKVStore(ctx, gw, "/om-test/")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	key := "own/default/bundle-okv"
	_, _ = store.Cas(ctx, key, 0, []byte(`{"brokerId":"a"}`)) // may already exist from prior run
	// Ensure clean create path: delete then create-only.
	_ = store.Delete(ctx, key, 0)

	ver, err := store.Cas(ctx, key, 0, []byte(`{"brokerId":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	val, got, err := store.Get(ctx, key)
	if err != nil || string(val) == "" || got == 0 {
		t.Fatalf("get %v %q ver=%d", err, val, got)
	}
	if got != ver {
		t.Fatalf("version mismatch cas=%d get=%d", ver, got)
	}
	_, err = store.Cas(ctx, key, 0, []byte(`nope`))
	if err != metadata.ErrVersionConflict {
		t.Fatalf("want conflict on create-exists, got %v", err)
	}
	ver2, err := store.Cas(ctx, key, ver, []byte(`{"brokerId":"b"}`))
	if err != nil || ver2 == ver {
		t.Fatalf("cas update %v ver %d->%d", err, ver, ver2)
	}

	keys, err := store.ListPrefix(ctx, "own/")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		if k == key {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListPrefix missing %s in %v", key, keys)
	}

	s := &metadata.Store{Raw: store, Clock: clock.System{}}
	rec, _, err := s.TryAcquire(ctx, "okv-ns", "00000000-ffffffff", "broker-okv", "x:1", 15_000)
	if err != nil {
		t.Fatal(err)
	}
	if rec.BrokerID != "broker-okv" {
		t.Fatalf("%+v", rec)
	}
}
