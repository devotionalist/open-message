package broker_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

func TestInProcessProduceConsumeExclusive(t *testing.T) {
	meta := &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}
	ol := olclient.NewMemoryQa1()
	b := broker.New(broker.Config{
		BrokerID: "b0", Advertise: "127.0.0.1:0", ListenAddr: "127.0.0.1:0",
		LeaseTTL: 15 * time.Second, RenewEvery: time.Hour,
	}, meta, ol, nil)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(ctx)

	lid, eid, err := b.Produce(ctx, "public/default/demo", "p1", 1, true, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	// idempotent re-send
	lid2, eid2, err := b.Produce(ctx, "public/default/demo", "p1", 1, true, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if lid != lid2 || eid != eid2 {
		t.Fatalf("idempotency broken %d:%d vs %d:%d", lid, eid, lid2, eid2)
	}
}

func TestDualBrokerFailover(t *testing.T) {
	clk := &clock.Fixed{Ms: time.Now().UnixMilli()}
	raw := metadata.NewMemoryStore()
	metaA := &metadata.Store{Raw: raw, Clock: clk}
	metaB := &metadata.Store{Raw: raw, Clock: clk}
	ol := olclient.NewMemoryQa1()

	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 5 * time.Second, RenewEvery: time.Hour}, metaA, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 5 * time.Second, RenewEvery: time.Hour}, metaB, ol, nil)
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(ctx)

	if _, _, err := a.Produce(ctx, "public/default/fo", "p", 1, false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	// B cannot produce while A holds lease
	if _, _, err := b.Produce(ctx, "public/default/fo", "p", 2, false, []byte("y")); err == nil {
		t.Fatal("expected not owner")
	}
	_ = a.UnloadBundle(ctx, "default", "00000000-ffffffff")
	if _, _, err := b.Produce(ctx, "public/default/fo", "p", 3, false, []byte("z")); err != nil {
		t.Fatal(err)
	}
}
