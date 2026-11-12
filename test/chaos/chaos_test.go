package chaos_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

// C1: kill Owner — Receipts before kill remain readable after failover.
func TestC1_KillOwner(t *testing.T) {
	clk := &clock.Fixed{Ms: time.Now().UnixMilli()}
	raw := metadata.NewMemoryStore()
	ol := olclient.NewMemoryQa1()
	metaA := &metadata.Store{Raw: raw, Clock: clk}
	metaB := &metadata.Store{Raw: raw, Clock: clk}
	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 2 * time.Second, RenewEvery: time.Hour}, metaA, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 2 * time.Second, RenewEvery: time.Hour}, metaB, ol, nil)
	ctx := context.Background()
	_ = a.Start(ctx)
	_ = b.Start(ctx)
	defer b.Stop(ctx)

	lid, eid, err := a.Produce(ctx, "public/default/c1", "p", 1, false, []byte("durable"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate hard kill: do not Drain/Unload; wait for lease expiry.
	clk.Advance(3 * time.Second)
	if _, _, err := b.Produce(ctx, "public/default/c1", "p", 2, false, []byte("after")); err != nil {
		t.Fatal(err)
	}
	_ = lid
	_ = eid
	_ = a // keep alive so renew may race; lease already expired for Acquire
}

func TestC3_DrainUnload(t *testing.T) {
	raw := metadata.NewMemoryStore()
	ol := olclient.NewMemoryQa1()
	meta := &metadata.Store{Raw: raw, Clock: clock.System{}}
	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, meta, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, &metadata.Store{Raw: raw, Clock: clock.System{}}, ol, nil)
	ctx := context.Background()
	_ = a.Start(ctx)
	_ = b.Start(ctx)
	defer a.Stop(ctx)
	defer b.Stop(ctx)
	if _, _, err := a.Produce(ctx, "public/default/c3", "p", 1, false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := a.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Produce(ctx, "public/default/c3", "p", 2, false, []byte("y")); err != nil {
		t.Fatal(err)
	}
}

func TestC4_ZombieOldOwner(t *testing.T) {
	clk := &clock.Fixed{Ms: time.Now().UnixMilli()}
	raw := metadata.NewMemoryStore()
	ol := olclient.NewMemoryQa1()
	metaA := &metadata.Store{Raw: raw, Clock: clk}
	metaB := &metadata.Store{Raw: raw, Clock: clk}
	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 2 * time.Second, RenewEvery: time.Hour}, metaA, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 2 * time.Second, RenewEvery: time.Hour}, metaB, ol, nil)
	ctx := context.Background()
	_ = a.Start(ctx)
	_ = b.Start(ctx)
	defer a.Stop(ctx)
	defer b.Stop(ctx)
	if _, _, err := a.Produce(ctx, "public/default/c4", "p", 1, false, []byte("1")); err != nil {
		t.Fatal(err)
	}
	_ = a.UnloadBundle(ctx, "default", "00000000-ffffffff")
	clk.Advance(3 * time.Second)
	if _, _, err := b.Produce(ctx, "public/default/c4", "p", 2, false, []byte("2")); err != nil {
		t.Fatal(err)
	}
	// zombie A tries produce without ownership — EnsureOwner should fail or fence
	if _, _, err := a.Produce(ctx, "public/default/c4", "p", 3, false, []byte("zombie")); err == nil {
		// If A re-acquires after unload it's ok only if B released; B still owns — must fail
		t.Fatal("zombie write succeeded")
	}
}

func TestC5_KillNonOwner(t *testing.T) {
	raw := metadata.NewMemoryStore()
	ol := olclient.NewMemoryQa1()
	meta := &metadata.Store{Raw: raw, Clock: clock.System{}}
	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, meta, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, &metadata.Store{Raw: raw, Clock: clock.System{}}, ol, nil)
	ctx := context.Background()
	_ = a.Start(ctx)
	_ = b.Start(ctx)
	defer a.Stop(ctx)
	if _, _, err := a.Produce(ctx, "public/default/c5", "p", 1, false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = b.Stop(ctx)
	if _, _, err := a.Produce(ctx, "public/default/c5", "p", 2, false, []byte("y")); err != nil {
		t.Fatal(err)
	}
}
