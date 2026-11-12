package opsdrill_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

// O1 rolling drain unload under traffic.
func TestO1_RollingDrain(t *testing.T) {
	raw := metadata.NewMemoryStore()
	ol := olclient.NewMemoryQa1()
	ctx := context.Background()
	a := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, &metadata.Store{Raw: raw, Clock: clock.System{}}, ol, nil)
	b := broker.New(broker.Config{BrokerID: "b", Advertise: "b:1", ListenAddr: "127.0.0.1:0", LeaseTTL: 15 * time.Second, RenewEvery: time.Hour}, &metadata.Store{Raw: raw, Clock: clock.System{}}, ol, nil)
	_ = a.Start(ctx)
	_ = b.Start(ctx)
	defer a.Stop(ctx)
	defer b.Stop(ctx)
	errN := 0
	for i := 0; i < 20; i++ {
		if i == 10 {
			_ = a.Drain(ctx)
		}
		owner := a
		if i >= 10 {
			owner = b
		}
		if _, _, err := owner.Produce(ctx, "public/default/o1", "p", uint64(i+1), true, []byte("x")); err != nil {
			errN++
		}
	}
	if errN > 2 {
		t.Fatalf("too many errors during drain: %d", errN)
	}
}

func TestO8_QuotaFill(t *testing.T) {
	ol := olclient.NewMemoryQa1()
	br := broker.New(broker.Config{BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0", LeaseTTL: time.Minute, RenewEvery: time.Hour}, &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}, ol, nil)
	ctx := context.Background()
	_ = br.Start(ctx)
	defer br.Stop(ctx)
	br.Quota().SetPolicy("default", br.Quota().Policy("default"))
	p := br.Quota().Policy("default")
	p.PublishRateMsg = 2
	br.Quota().SetPolicy("default", p)
	ok := 0
	for i := 0; i < 5; i++ {
		if _, _, err := br.Produce(ctx, "public/default/o8", "p", uint64(i+1), false, []byte("x")); err == nil {
			ok++
		}
	}
	if ok > 3 {
		t.Fatalf("quota not enforced, ok=%d", ok)
	}
}

func TestO14_BackupRestoreDryRun(t *testing.T) {
	// Dry-run: export ownership/index keys listing as backup artifact.
	raw := metadata.NewMemoryStore()
	meta := &metadata.Store{Raw: raw, Clock: clock.System{}}
	ctx := context.Background()
	_, _, _ = meta.TryAcquire(ctx, "default", "00000000-ffffffff", "a", "a:1", 15000)
	keys, err := raw.ListPrefix(ctx, "own/")
	if err != nil || len(keys) == 0 {
		t.Fatalf("backup dry-run list: %v %v", keys, err)
	}
}
