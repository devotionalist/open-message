package metadata_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
)

func TestOxiaStoreCAS(t *testing.T) {
	addr := os.Getenv("OXIA_ADDR")
	if addr == "" {
		t.Skip("set OXIA_ADDR to run (e.g. 127.0.0.1:16648 after port-forward)")
	}
	store, err := metadata.NewOxiaStore(addr, "/om-test/")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	key := "own/default/bundle-t"
	_, err = store.Cas(ctx, key, 0, []byte(`{"brokerId":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	val, ver, err := store.Get(ctx, key)
	if err != nil || string(val) == "" || ver == 0 {
		t.Fatalf("get %v %q ver=%d", err, val, ver)
	}
	_, err = store.Cas(ctx, key, 0, []byte(`nope`))
	if err != metadata.ErrVersionConflict {
		t.Fatalf("want conflict on create-exists, got %v", err)
	}
	ver2, err := store.Cas(ctx, key, ver, []byte(`{"brokerId":"b"}`))
	if err != nil || ver2 == ver {
		t.Fatalf("cas update %v ver %d->%d", err, ver, ver2)
	}

	s := &metadata.Store{Raw: store, Clock: clock.System{}}
	rec, _, err := s.TryAcquire(ctx, "oxia-ns", "00000000-ffffffff", "broker-x", "x:1", 15_000)
	if err != nil {
		t.Fatal(err)
	}
	if rec.BrokerID != "broker-x" {
		t.Fatalf("%+v", rec)
	}
}
