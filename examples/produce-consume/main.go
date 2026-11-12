package main

import (
	"context"
	"fmt"
	"os"
	"time"

	om "github.com/devotionalist/open-message-client-go"
)

// Customer acceptance smoke: produce → subscribe/flow → receive → ack.
func main() {
	url := env("OM_SERVICE_URL", "127.0.0.1:6650")
	topic := env("OM_TOPIC", "public/default/demo")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	c, err := om.Connect(ctx, om.Config{ServiceURL: url, AuthToken: os.Getenv("OM_AUTH_TOKEN")})
	if err != nil {
		fatal(err)
	}
	defer c.Close()

	p, err := c.CreateProducer(ctx, topic, "demo-producer", true)
	if err != nil {
		fatal(err)
	}
	lid, eid, err := p.Send(ctx, []byte("hello-open-message"))
	if err != nil {
		fatal(err)
	}
	fmt.Printf("receipt ledger=%d entry=%d\n", lid, eid)

	co, err := c.Subscribe(ctx, topic, env("OM_SUB", "demo-sub"), "Exclusive")
	if err != nil {
		fatal(err)
	}
	if err := co.Flow(ctx, 10); err != nil {
		fatal(err)
	}
	msg, err := co.Receive(ctx)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("got %q at %d:%d\n", string(msg.Payload), msg.LedgerID, msg.EntryID)
	if err := co.AckCumulative(ctx, msg.LedgerID, int64(msg.EntryID)); err != nil {
		fatal(err)
	}
	fmt.Println("customer-acceptance: PASS")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
