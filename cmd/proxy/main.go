package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/devotionalist/open-message/internal/proxy"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	p := proxy.New(proxy.Config{
		ListenAddr:    env("LISTEN_ADDR", ":6650"),
		DefaultBroker: env("DEFAULT_BROKER", "127.0.0.1:6651"),
	}, log)
	if err := p.Start(ctx); err != nil {
		log.Error("proxy start", "err", err)
		os.Exit(1)
	}
	<-ctx.Done()
	_ = p.Stop()
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
