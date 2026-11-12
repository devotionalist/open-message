package stress_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

func TestS_BaselineThroughput(t *testing.T) {
	ol := olclient.NewMemoryQa1()
	br := broker.New(broker.Config{
		BrokerID: "a", Advertise: "a:1", ListenAddr: "127.0.0.1:0",
		LeaseTTL: time.Minute, RenewEvery: time.Hour,
	}, &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}, ol, nil)
	ctx := context.Background()
	_ = br.Start(ctx)
	defer br.Stop(ctx)

	const n = 2000
	start := time.Now()
	for i := 0; i < n; i++ {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			_, _, err = br.Produce(ctx, "public/default/stress", "p", uint64(i+1), true, []byte("payload"))
			if err == nil {
				break
			}
		}
		if err != nil {
			t.Fatalf("seq %d: %v", i+1, err)
		}
	}
	elapsed := time.Since(start)
	t.Logf("S-baseline: %d msgs in %s (%.0f msg/s)", n, elapsed, float64(n)/elapsed.Seconds())
}
