package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/devotionalist/open-message/internal/broker"
	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/metakv"
	"github.com/devotionalist/open-message/internal/olclient"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	raw, closer, err := openMetaStore(log)
	if err != nil {
		log.Error("metadata backend", "err", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer.Close()
	}
	meta := &metadata.Store{Raw: raw, Clock: clock.System{}}

	e := envUint("OL_E", 1)
	qw := envUint("OL_QW", 1)
	qa := envUint("OL_QA", 1)
	var ol olclient.Client
	if addrs := strings.TrimSpace(os.Getenv("BOOKIE_ADDRS")); addrs != "" {
		bookies, closers, err := olclient.DialBookies(ctx, strings.Split(addrs, ","))
		if err != nil {
			log.Error("dial bookies", "err", err)
			os.Exit(1)
		}
		defer func() {
			for _, c := range closers {
				_ = c.Close()
			}
		}()
		olMetaURL := env("OL_META_URL", "")
		if olMetaURL == "" {
			log.Error("BOOKIE_ADDRS requires OL_META_URL (HTTP LedgerMeta store)")
			os.Exit(1)
		}
		ol = olclient.NewRemote(olMetaURL, bookies, 1)
		log.Info("using remote open-ledger", "bookies", len(bookies), "E", e, "Qw", qw, "Qa", qa)
	} else if e > 1 {
		ol = olclient.NewMemoryQuorum(e, qw, qa)
		log.Info("using memory quorum OL", "E", e)
	} else {
		ol = olclient.NewMemoryQa1()
		log.Info("using memory Qa=1 OL")
	}

	advertise := env("ADVERTISE_ADDR", "")
	if advertise == "" {
		advertise = env("HOSTNAME", env("BROKER_ID", "broker-0")) + env("ADVERTISE_SUFFIX", ":6650")
	}
	cfg := broker.Config{
		BrokerID:    env("BROKER_ID", env("HOSTNAME", "broker-0")),
		Advertise:   advertise,
		ListenAddr:  env("LISTEN_ADDR", ":6650"),
		AdminAddr:   env("ADMIN_ADDR", ":8080"),
		AuthToken:   env("AUTH_TOKEN", ""),
		LeaseTTL:    durationEnv("LEASE_TTL", 15*time.Second),
		RenewEvery:  durationEnv("RENEW_EVERY", 3*time.Second),
		RollEvery:   durationEnv("ROLL_EVERY", 0),
		Quorum:      olclient.QuorumPolicy{E: e, Qw: qw, Qa: qa},
		PersistIdem: env("PERSIST_IDEM", "true") == "true",
		MinBrokers:  int(envUint("MIN_BROKERS", 1)),
		MinBookies:  int(envUint("MIN_BOOKIES", 1)),
	}
	b := broker.New(cfg, meta, ol, log)
	if err := b.Start(ctx); err != nil {
		log.Error("start failed", "err", err)
		os.Exit(1)
	}
	<-ctx.Done()
	shctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = b.Stop(shctx)
}

func openMetaStore(log *slog.Logger) (metadata.RawStore, io.Closer, error) {
	backend := strings.ToLower(env("METADATA_BACKEND", "oxia"))
	switch backend {
	case "oxia":
		addr := env("OXIA_ADDR", "oxia.open-ledger.svc.cluster.local:6648")
		prefix := env("OXIA_PREFIX", "/om/")
		store, err := metadata.NewOxiaStore(addr, prefix)
		if err != nil {
			return nil, nil, err
		}
		log.Info("using Oxia metadata", "addr", addr, "prefix", prefix)
		return store, store, nil
	case "metakv", "http":
		url := env("METADATA_URL", "http://metakv.open-message.svc.cluster.local:7700")
		log.Info("using metakv metadata", "url", url)
		return &metakv.HTTPStore{Base: url, Prefix: "om/"}, nil, nil
	case "memory":
		log.Info("using memory metadata")
		return metadata.NewMemoryStore(), nil, nil
	case "openkv":
		gw := env("OPENKV_GATEWAY", "gateway.openkv.svc.cluster.local:19000")
		prefix := env("OPENKV_PREFIX", "/om/")
		store, err := metadata.NewOpenKVStore(context.Background(), gw, prefix)
		if err != nil {
			return nil, nil, err
		}
		log.Info("using OpenKV metadata", "gateway", gw, "prefix", prefix)
		return store, store, nil
	default:
		return nil, nil, fmt.Errorf("unknown METADATA_BACKEND: %s", backend)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envUint(k string, def uint32) uint32 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return def
	}
	return uint32(n)
}

func durationEnv(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
