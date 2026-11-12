// faultdrill: sustained produce/consume with safety/liveness windows for K8s fault drills.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	om "github.com/devotionalist/open-message-client-go"
)

type window struct {
	mu      sync.Mutex
	prodOK  int64
	prodErr int64
	consOK  int64
	consErr int64
	since   time.Time
}

func (w *window) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prodOK, w.prodErr, w.consOK, w.consErr = 0, 0, 0, 0
	w.since = time.Now()
}

func (w *window) snap() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	elapsed := time.Since(w.since).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	total := w.prodOK + w.prodErr
	rate := 1.0
	if total > 0 {
		rate = float64(w.prodOK) / float64(total)
	}
	return map[string]any{
		"elapsed_s":       elapsed,
		"produce_ok":      w.prodOK,
		"produce_err":     w.prodErr,
		"produce_ok_rate": rate,
		"consume_ok":      w.consOK,
		"consume_err":     w.consErr,
		"produce_rps":     float64(w.prodOK) / elapsed,
	}
}

func main() {
	url := env("OM_SERVICE_URL", "proxy.open-message.svc.cluster.local:6650")
	topic := env("OM_TOPIC", "public/default/fault-drill")
	auth := os.Getenv("OM_AUTH_TOKEN")
	duration := durationEnv("DRILL_DURATION", 3*time.Minute)
	pace := durationEnv("PRODUCE_EVERY", 50*time.Millisecond)
	admin := env("STATS_ADDR", ":9090")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var receipts sync.Map // seq -> "ledger:entry"
	var seen sync.Map     // seq -> struct{}
	var receiptN atomic.Int64
	var seenN atomic.Int64
	win := &window{since: time.Now()}
	var phase atomic.Value
	phase.Store("baseline")

	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		s := win.snap()
		s["phase"] = phase.Load()
		s["receipts"] = receiptN.Load()
		s["consumed_unique"] = seenN.Load()
		_ = json.NewEncoder(w).Encode(s)
	})
	mux.HandleFunc("/phase", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", 400)
			return
		}
		phase.Store(name)
		win.reset()
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, _ *http.Request) {
		phase.Store("stopping")
		cancel()
		_, _ = w.Write([]byte("ok"))
	})
	go func() { _ = http.ListenAndServe(admin, mux) }()

	cfg := om.Config{
		ServiceURL:     url,
		AuthToken:      auth,
		RequestTimeout: 5 * time.Second,
		MaxRetries:     12,
		DialTimeout:    3 * time.Second,
	}

	var clientMu sync.Mutex
	client, err := dialWithRetry(runCtx, cfg)
	if err != nil {
		fatal(err)
	}
	defer func() {
		clientMu.Lock()
		_ = client.Close()
		clientMu.Unlock()
	}()

	prod, err := client.CreateProducer(runCtx, topic, "fault-drill-p", true)
	if err != nil {
		fatal(err)
	}
	subName := env("OM_SUB", "fault-drill-sub")
	cons, err := client.Subscribe(runCtx, topic, subName, "Failover")
	if err != nil {
		fatal(err)
	}
	_ = cons.Flow(runCtx, 64)

	reconnect := func() {
		clientMu.Lock()
		defer clientMu.Unlock()
		_ = client.Close()
		nc, err := dialWithRetry(runCtx, cfg)
		if err != nil {
			return
		}
		client = nc
		if np, err := client.CreateProducer(runCtx, topic, "fault-drill-p", true); err == nil {
			prod = np
		}
		if nc2, err := client.Subscribe(runCtx, topic, subName, "Failover"); err == nil {
			cons = nc2
			_ = cons.Flow(runCtx, 64)
		}
	}

	var seq atomic.Uint64
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		t := time.NewTicker(pace)
		defer t.Stop()
		failStreak := 0
		for {
			select {
			case <-runCtx.Done():
				return
			case <-t.C:
				n := seq.Add(1)
				payload := []byte(fmt.Sprintf("seq=%d", n))
				sctx, cancel := context.WithTimeout(runCtx, 8*time.Second)
				clientMu.Lock()
				p := prod
				clientMu.Unlock()
				lid, eid, err := p.Send(sctx, payload)
				cancel()
				win.mu.Lock()
				if err != nil {
					win.prodErr++
					win.mu.Unlock()
					failStreak++
					if failStreak >= 3 {
						reconnect()
						failStreak = 0
					}
					continue
				}
				failStreak = 0
				win.prodOK++
				win.mu.Unlock()
				receipts.Store(n, fmt.Sprintf("%d:%d", lid, eid))
				receiptN.Add(1)
			}
		}
	}()

	go func() {
		defer wg.Done()
		failStreak := 0
		for {
			select {
			case <-runCtx.Done():
				return
			default:
			}
			clientMu.Lock()
			c := cons
			clientMu.Unlock()
			_ = c.Flow(runCtx, 32)
			rctx, ccancel := context.WithTimeout(runCtx, 3*time.Second)
			msg, err := c.Receive(rctx)
			ccancel()
			win.mu.Lock()
			if err != nil {
				win.consErr++
				win.mu.Unlock()
				failStreak++
				if failStreak >= 3 {
					reconnect()
					failStreak = 0
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			failStreak = 0
			win.consOK++
			win.mu.Unlock()
			var n uint64
			if _, scanErr := fmt.Sscanf(string(msg.Payload), "seq=%d", &n); scanErr == nil && n > 0 {
				if _, loaded := seen.LoadOrStore(n, struct{}{}); !loaded {
					seenN.Add(1)
				}
			}
			_ = c.AckCumulative(runCtx, msg.LedgerID, int64(msg.EntryID))
		}
	}()

	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-t.C:
				s := win.snap()
				s["phase"] = phase.Load()
				s["receipts"] = receiptN.Load()
				s["consumed_unique"] = seenN.Load()
				b, _ := json.Marshal(s)
				fmt.Printf("stats %s\n", b)
			}
		}
	}()

	<-runCtx.Done()
	wg.Wait()

	// Safety: every receipt seq must be consumed (allow drain window).
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer drainCancel()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		missing := 0
		receipts.Range(func(k, _ any) bool {
			if _, ok := seen.Load(k); !ok {
				missing++
			}
			return true
		})
		if missing == 0 {
			break
		}
		clientMu.Lock()
		c := cons
		clientMu.Unlock()
		_ = c.Flow(drainCtx, 64)
		rctx, cancel := context.WithTimeout(drainCtx, 2*time.Second)
		msg, err := c.Receive(rctx)
		cancel()
		if err == nil {
			var n uint64
			if _, scanErr := fmt.Sscanf(string(msg.Payload), "seq=%d", &n); scanErr == nil {
				if _, loaded := seen.LoadOrStore(n, struct{}{}); !loaded {
					seenN.Add(1)
				}
			}
			_ = c.AckCumulative(drainCtx, msg.LedgerID, int64(msg.EntryID))
		}
	}

	missing := make([]uint64, 0)
	receipts.Range(func(k, _ any) bool {
		n := k.(uint64)
		if _, ok := seen.Load(n); !ok {
			missing = append(missing, n)
		}
		return true
	})

	report := map[string]any{
		"receipts":         receiptN.Load(),
		"consumed_unique":  seenN.Load(),
		"missing_receipts": len(missing),
		"safety_pass":      len(missing) == 0,
	}
	if len(missing) > 0 && len(missing) <= 20 {
		report["missing_seq"] = missing
	}
	b, _ := json.Marshal(report)
	fmt.Printf("REPORT %s\n", b)
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "safety FAIL: %d receipts not consumed\n", len(missing))
		os.Exit(2)
	}
	fmt.Println("faultdrill: PASS")
}

func dialWithRetry(ctx context.Context, cfg om.Config) (*om.Client, error) {
	var last error
	for i := 0; i < 30; i++ {
		c, err := om.Connect(ctx, cfg)
		if err == nil {
			// Prefer Owner direct connect so Subscribe survives Proxy bootstrap skew.
			if url, lerr := c.Lookup(ctx, env("OM_TOPIC", "public/default/fault-drill")); lerr == nil && url != "" && url != cfg.ServiceURL {
				if err := c.Close(); err == nil {
					cfg2 := cfg
					cfg2.ServiceURL = url
					if c2, err2 := om.Connect(ctx, cfg2); err2 == nil {
						return c2, nil
					}
				}
			}
			return c, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, last
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func durationEnv(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	return def
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
