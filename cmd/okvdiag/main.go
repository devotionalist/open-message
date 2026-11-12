package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	okv "github.com/devotionalist/openkv-client-go/kv"
	"github.com/oxia-db/oxia/oxia"
)

func pct(a []float64, p float64) float64 {
	if len(a) == 0 {
		return 0
	}
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	return b[int(p * float64(len(b)-1))]
}

func sumStats(name string, lat []float64, errs int) {
	var s float64
	for _, v := range lat {
		s += v
	}
	mean := 0.0
	if len(lat) > 0 {
		mean = s / float64(len(lat))
	}
	fmt.Printf("%-22s n=%-4d err=%-3d ok=%-4d  p50=%8.0fµs p95=%8.0fµs p99=%8.0fµs mean=%8.0fµs\n",
		name, len(lat), errs, len(lat)-errs, pct(lat, 0.50), pct(lat, 0.95), pct(lat, 0.99), mean)
}

func benchOK(n int, fn func() error) (lat []float64, errs int, first error) {
	lat = make([]float64, 0, n)
	for i := 0; i < n; i++ {
		t0 := time.Now()
		err := fn()
		lat = append(lat, float64(time.Since(t0).Microseconds()))
		if err != nil {
			errs++
			if first == nil {
				first = err
			}
		}
	}
	return lat, errs, first
}

func main() {
	ctx := context.Background()
	cli, err := okv.Dial(ctx, "127.0.0.1:19000", okv.WithTimeout(5*time.Second))
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	ox, err := oxia.NewSyncClient("127.0.0.1:16648")
	if err != nil {
		panic(err)
	}
	defer ox.Close()

	key := []byte(fmt.Sprintf("/diag/openkv/%d", time.Now().UnixNano()))
	oxKey := fmt.Sprintf("/diag/oxia/%d", time.Now().UnixNano())
	val := []byte(`{"ensemble":["b0","b1","b2"],"qw":2}`)
	N := 50

	fmt.Println("seed key", string(key))
	rev, err := cli.Put(ctx, key, val)
	fmt.Println("seed Put:", rev, err)
	_, _, err = ox.Put(ctx, oxKey, val)
	fmt.Println("seed Oxia Put:", err)

	fmt.Println("=== OpenKV ===")
	lat, e, fe := benchOK(N, func() error {
		_, _, _, err := cli.GetOpt(ctx, key, true)
		return err
	})
	sumStats("okv Get lin=true", lat, e)
	if fe != nil {
		fmt.Println("  first err:", fe)
	}
	lat, e, fe = benchOK(N, func() error {
		_, _, _, err := cli.GetOpt(ctx, key, false)
		return err
	})
	sumStats("okv Get lin=false", lat, e)
	if fe != nil {
		fmt.Println("  first err:", fe)
	}

	lat, e, fe = benchOK(N, func() error {
		_, err := cli.Put(ctx, key, []byte(fmt.Sprintf(`{"ts":%d}`, time.Now().UnixNano())))
		return err
	})
	sumStats("okv Put (2PC)", lat, e)
	if fe != nil {
		fmt.Println("  first err:", fe)
	}

	latG := make([]float64, 0, N)
	errs := 0
	var first error
	for i := 0; i < N; i++ {
		t0 := time.Now()
		_, _, r, err := cli.Get(ctx, key)
		if err != nil {
			errs++
			if first == nil {
				first = err
			}
			latG = append(latG, float64(time.Since(t0).Microseconds()))
			continue
		}
		_, err = cli.PutCAS(ctx, key, []byte(fmt.Sprintf(`{"ts":%d}`, time.Now().UnixNano())), r)
		latG = append(latG, float64(time.Since(t0).Microseconds()))
		if err != nil {
			errs++
			if first == nil {
				first = err
			}
		}
	}
	sumStats("okv Get+PutCAS", latG, errs)
	if first != nil {
		fmt.Println("  first err:", first)
	}

	_, _, r0, _ := cli.Get(ctx, key)
	latP := make([]float64, 0, N)
	errs = 0
	first = nil
	cur := r0
	for i := 0; i < N; i++ {
		t0 := time.Now()
		nr, err := cli.PutCAS(ctx, key, []byte(fmt.Sprintf(`{"ts":%d}`, time.Now().UnixNano())), cur)
		dt := float64(time.Since(t0).Microseconds())
		latP = append(latP, dt)
		if err != nil {
			errs++
			if first == nil {
				first = fmt.Errorf("%v (after %.0fµs)", err, dt)
			}
			_, _, cur, _ = cli.Get(ctx, key)
			continue
		}
		cur = nr
	}
	sumStats("okv PutCAS serial", latP, errs)
	if first != nil {
		fmt.Println("  first err:", first)
	}

	fmt.Println("=== Oxia ===")
	lat, e, fe = benchOK(N, func() error {
		_, _, _, err := ox.Get(ctx, oxKey)
		return err
	})
	sumStats("oxia Get", lat, e)
	_, v0, _ := ox.Put(ctx, oxKey, val)
	ver := v0.VersionId
	lat, e, fe = benchOK(N, func() error {
		_, nv, err := ox.Put(ctx, oxKey, []byte(fmt.Sprintf(`{"ts":%d}`, time.Now().UnixNano())), oxia.ExpectedVersionId(ver))
		if err != nil {
			return err
		}
		ver = nv.VersionId
		return nil
	})
	sumStats("oxia Put expectedVer", lat, e)
	if fe != nil {
		fmt.Println("  first err:", fe)
	}
}
