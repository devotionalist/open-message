package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/client"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
)

func TestSDKProduceConsume(t *testing.T) {
	meta := &metadata.Store{Raw: metadata.NewMemoryStore(), Clock: clock.System{}}
	ol := olclient.NewMemoryQa1()
	b := broker.New(broker.Config{
		BrokerID: "b0", Advertise: "127.0.0.1:19650", ListenAddr: "127.0.0.1:19650",
		AdminAddr: "127.0.0.1:19080", LeaseTTL: 30 * time.Second, RenewEvery: time.Hour,
	}, meta, ol, nil)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	c, err := client.Connect(ctx, client.Config{ServiceURL: "127.0.0.1:19650"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := c.CreateProducer(ctx, "public/default/sdk", "sdk-p", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Send(ctx, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	co, err := c.Subscribe(ctx, "public/default/sdk", "s1", "Exclusive")
	if err != nil {
		t.Fatal(err)
	}
	if err := co.Flow(ctx, 5); err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	msg, err := co.Receive(rctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.Payload) != "hi" {
		t.Fatalf("got %q", msg.Payload)
	}
}
