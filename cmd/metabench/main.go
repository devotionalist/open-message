// metabench compares Oxia vs OpenKV under OM/OL-like Meta access patterns.
//
// OM: Ownership Get+Cas renew, Lookup Get, Cursor Get+Cas, ListPrefix own/
// OL-like (same RawStore ops OL coord uses): bookie heartbeat Cas, ListPrefix bookie/,
// ledger-meta sized PutCAS + Get.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devotionalist/open-message/internal/clock"
	"github.com/devotionalist/open-message/internal/metadata"
)

type result struct {
	Backend   string  `json:"backend"`
	Scenario  string  `json:"scenario"`
	Conc      int     `json:"concurrency"`
	Ops       int64   `json:"ops"`
	Errors    int64   `json:"errors"`
	ElapsedS  float64 `json:"elapsed_s"`
	OpsPerSec float64 `json:"ops_per_sec"`
	P50Us     float64 `json:"p50_us"`
	P95Us     float64 `json:"p95_us"`
	P99Us     float64 `json:"p99_us"`
	MeanUs    float64 `json:"mean_us"`
}

func main() {
	backend := flag.String("backend", "oxia", "oxia|openkv")
	oxiaAddr := flag.String("oxia", "127.0.0.1:6648", "Oxia service address")
	okvGW := flag.String("openkv", "127.0.0.1:19000", "OpenKV gateway")
	prefix := flag.String("prefix", "/om-bench/", "key prefix (isolated)")
	ops := flag.Int("ops", 2000, "ops per latency scenario (serial)")
	duration := flag.Duration("duration", 5*time.Second, "duration for concurrent scenarios")
	conc := flag.String("conc", "1,8,32", "concurrency list for mixed/throughput")
	jsonOut := flag.Bool("json", false, "emit JSON lines")
	flag.Parse()

	ctx := context.Background()
	raw, closer, err := dial(*backend, *oxiaAddr, *okvGW, *prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer closer()

	store := &metadata.Store{Raw: raw, Clock: clock.System{}}
	if err := seed(ctx, store, raw); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}

	var concs []int
	for _, p := range strings.Split(*conc, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		fmt.Sscanf(p, "%d", &n)
		if n > 0 {
			concs = append(concs, n)
		}
	}

	var results []result
	run := func(r result) {
		results = append(results, r)
		if *jsonOut {
			b, _ := json.Marshal(r)
			fmt.Println(string(b))
			return
		}
		fmt.Printf("%-8s %-18s c=%-3d ops=%-6d err=%-4d  %8.1f ops/s  p50=%7.0fµs p95=%7.0fµs p99=%7.0fµs mean=%7.0fµs\n",
			r.Backend, r.Scenario, r.Conc, r.Ops, r.Errors, r.OpsPerSec, r.P50Us, r.P95Us, r.P99Us, r.MeanUs)
	}

	backendName := *backend
	fmt.Fprintf(os.Stderr, "==> metabench backend=%s prefix=%s ops=%d duration=%s\n", backendName, *prefix, *ops, *duration)

	ownEpoch := atomic.Uint64{}
	{
		rec, err := store.Lookup(ctx, "default", "00000000-ffffffff")
		if err != nil {
			fmt.Fprintf(os.Stderr, "lookup ownership after seed: %v\n", err)
			os.Exit(1)
		}
		ownEpoch.Store(rec.Epoch)
	}
	doRenew := func(ctx context.Context) error {
		_, err := store.Renew(ctx, "default", "00000000-ffffffff", "broker-bench", ownEpoch.Load(), 30_000)
		return err
	}

	// Serial latency (concurrency=1): pure primitives + OM/OL patterns.
	run(benchSerial(backendName, "get_lookup", *ops, func(ctx context.Context) error {
		_, err := store.Lookup(ctx, "default", "00000000-ffffffff")
		return err
	}))
	run(benchSerial(backendName, "om_renew", *ops, doRenew))
	run(benchSerial(backendName, "om_cursor", *ops, func(ctx context.Context) error {
		return store.CasAdvance(ctx, "public", "default", "bench-topic", "sub", metadata.Position{
			LedgerID: 1,
			EntryID:  time.Now().UnixNano(),
		})
	}))
	run(benchSerial(backendName, "om_list_own", max(1, *ops/10), func(ctx context.Context) error {
		_, err := raw.ListPrefix(ctx, "own/")
		return err
	}))
	run(benchSerial(backendName, "ol_ledger_get", *ops, func(ctx context.Context) error {
		_, _, err := raw.Get(ctx, "ol/ledger/1")
		return err
	}))
	run(benchSerialCAS(backendName, "ol_ledger_cas", *ops, raw, "ol/ledger/hot"))
	run(benchSerial(backendName, "ol_list_bookie", max(1, *ops/10), func(ctx context.Context) error {
		_, err := raw.ListPrefix(ctx, "ol/bookie/")
		return err
	}))

	// Concurrent throughput mixes.
	for _, c := range concs {
		run(benchConcurrent(backendName, "mixed_om", c, *duration, func(ctx context.Context, i int64) error {
			switch i % 20 {
			case 0, 1:
				_, err := raw.ListPrefix(ctx, "own/")
				return err
			case 2, 3, 4:
				// Per-bundle renew avoids cross-worker epoch/version pile-up on one key.
				bid := fmt.Sprintf("%08x-%08x", int(i%16)*0x10000000, (int(i%16)+1)*0x10000000-1)
				rec, err := store.Lookup(ctx, "default", bid)
				if err != nil {
					return err
				}
				_, err = store.Renew(ctx, "default", bid, "broker-bench", rec.Epoch, 30_000)
				return err
			case 5, 6, 7, 8, 9, 10:
				return store.CasAdvance(ctx, "public", "default", "bench-topic", fmt.Sprintf("sub-%d", i%8), metadata.Position{
					LedgerID: 1,
					EntryID:  time.Now().UnixNano() + i,
				})
			default:
				_, err := store.Lookup(ctx, "default", "00000000-ffffffff")
				return err
			}
		}))
		run(benchConcurrent(backendName, "mixed_ol", c, *duration, func(ctx context.Context, i int64) error {
			switch i % 20 {
			case 0:
				_, err := raw.ListPrefix(ctx, "ol/bookie/")
				return err
			case 1, 2, 3:
				key := fmt.Sprintf("ol/bookie/%d", i%8)
				return putCASLatest(ctx, raw, key, []byte(fmt.Sprintf(`{"id":%d,"ts":%d}`, i%8, time.Now().UnixMilli())))
			case 4, 5, 6, 7, 8, 9, 10, 11:
				_, _, err := raw.Get(ctx, fmt.Sprintf("ol/ledger/%d", (i%16)+1))
				return err
			default:
				key := fmt.Sprintf("ol/ledger/%d", (i%16)+1)
				return putCASLatest(ctx, raw, key, []byte(fmt.Sprintf(`{"ledgerId":%d,"ensemble":["b0","b1","b2"],"qw":2,"qa":2,"ts":%d}`, (i%16)+1, time.Now().UnixMilli())))
			}
		}))
	}

	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "==> done %d rows\n", len(results))
	}
}

func dial(backend, oxiaAddr, okvGW, prefix string) (metadata.RawStore, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch backend {
	case "oxia":
		s, err := metadata.NewOxiaStore(oxiaAddr, prefix)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	case "openkv":
		s, err := metadata.NewOpenKVStore(ctx, okvGW, prefix)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unknown backend %q", backend)
	}
}

func seed(ctx context.Context, store *metadata.Store, raw metadata.RawStore) error {
	// OM ownership + 16 bundles for ListPrefix.
	_ = raw.Delete(ctx, metadata.OwnershipKey("default", "00000000-ffffffff"), 0)
	if _, _, err := store.TryAcquire(ctx, "default", "00000000-ffffffff", "broker-bench", "bench:1", 3600_000); err != nil {
		return fmt.Errorf("seed ownership: %w", err)
	}
	for i := 0; i < 16; i++ {
		bid := fmt.Sprintf("%08x-%08x", i*0x10000000, (i+1)*0x10000000-1)
		_ = raw.Delete(ctx, metadata.OwnershipKey("default", bid), 0)
		_, _, _ = store.TryAcquire(ctx, "default", bid, "broker-bench", "bench:1", 3600_000)
	}
	// Cursor seeds (8 subs).
	for i := 0; i < 8; i++ {
		_ = store.CasAdvance(ctx, "public", "default", "bench-topic", fmt.Sprintf("sub-%d", i), metadata.Position{LedgerID: 1, EntryID: 0})
	}
	_ = store.CasAdvance(ctx, "public", "default", "bench-topic", "sub", metadata.Position{LedgerID: 1, EntryID: 0})

	// OL-like bookies + ledgers.
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("ol/bookie/%d", i)
		_ = putCASLatest(ctx, raw, key, []byte(fmt.Sprintf(`{"id":%d,"addr":"bookie-%d:3181"}`, i, i)))
	}
	for i := 1; i <= 16; i++ {
		key := fmt.Sprintf("ol/ledger/%d", i)
		val := fmt.Sprintf(`{"ledgerId":%d,"ensemble":["b0","b1","b2"],"qw":2,"qa":2,"state":"OPEN"}`, i)
		_ = putCASLatest(ctx, raw, key, []byte(val))
	}
	_ = putCASLatest(ctx, raw, "ol/ledger/hot", []byte(`{"ledgerId":999,"ensemble":["b0","b1","b2"],"qw":2,"qa":2}`))
	return nil
}

func putCASLatest(ctx context.Context, raw metadata.RawStore, key string, val []byte) error {
	_, ver, err := raw.Get(ctx, key)
	if err == metadata.ErrNotFound {
		_, err = raw.Cas(ctx, key, 0, val)
		return err
	}
	if err != nil {
		return err
	}
	_, err = raw.Cas(ctx, key, ver, val)
	if err == metadata.ErrVersionConflict {
		// one retry
		_, ver, err = raw.Get(ctx, key)
		if err != nil {
			return err
		}
		_, err = raw.Cas(ctx, key, ver, val)
	}
	return err
}

func benchSerial(backend, name string, n int, fn func(context.Context) error) result {
	lat := make([]float64, 0, n)
	var errs int64
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < n; i++ {
		t0 := time.Now()
		if err := fn(ctx); err != nil {
			errs++
		}
		lat = append(lat, float64(time.Since(t0).Microseconds()))
	}
	elapsed := time.Since(start).Seconds()
	return summarize(backend, name, 1, int64(n), errs, elapsed, lat)
}

func benchSerialCAS(backend, name string, n int, raw metadata.RawStore, key string) result {
	return benchSerial(backend, name, n, func(ctx context.Context) error {
		return putCASLatest(ctx, raw, key, []byte(fmt.Sprintf(`{"ts":%d}`, time.Now().UnixNano())))
	})
}

func benchConcurrent(backend, name string, conc int, d time.Duration, fn func(context.Context, int64) error) result {
	var (
		ops, errs atomic.Int64
		mu        sync.Mutex
		lat       []float64
		wg        sync.WaitGroup
	)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	start := time.Now()
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			local := make([]float64, 0, 1024)
			var i int64
			for {
				if ctx.Err() != nil {
					break
				}
				seq := ops.Add(1)
				t0 := time.Now()
				if err := fn(ctx, seq+int64(worker)*1_000_000_000); err != nil {
					errs.Add(1)
				}
				local = append(local, float64(time.Since(t0).Microseconds()))
				i++
				_ = i
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	return summarize(backend, name, conc, ops.Load(), errs.Load(), elapsed, lat)
}

func summarize(backend, name string, conc int, ops, errs int64, elapsed float64, lat []float64) result {
	r := result{
		Backend:  backend,
		Scenario: name,
		Conc:     conc,
		Ops:      ops,
		Errors:   errs,
		ElapsedS: elapsed,
	}
	if elapsed > 0 {
		r.OpsPerSec = float64(ops-errs) / elapsed
	}
	if len(lat) == 0 {
		return r
	}
	sort.Float64s(lat)
	r.P50Us = percentile(lat, 0.50)
	r.P95Us = percentile(lat, 0.95)
	r.P99Us = percentile(lat, 0.99)
	var sum float64
	for _, v := range lat {
		sum += v
	}
	r.MeanUs = sum / float64(len(lat))
	return r
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	w := idx - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}
