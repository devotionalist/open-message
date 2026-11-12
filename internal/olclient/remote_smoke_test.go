package olclient_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/devotionalist/open-message/internal/olclient"
)

func TestRemoteSmokeOptional(t *testing.T) {
	meta := os.Getenv("OL_SMOKE_META")
	addrs := os.Getenv("OL_SMOKE_BOOKIES")
	if meta == "" || addrs == "" {
		t.Skip("set OL_SMOKE_META and OL_SMOKE_BOOKIES")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bookies, closers, err := olclient.DialBookies(ctx, strings.Split(addrs, ","))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	ol := olclient.NewRemote(meta, bookies, 1)
	e := uint32(len(bookies))
	if e > 3 {
		e = 3
	}
	id, err := ol.Create(ctx, olclient.QuorumPolicy{E: e, Qw: 2, Qa: 2})
	if err != nil {
		t.Fatal("create", err)
	}
	eid, err := ol.Append(ctx, id, []byte("hi-e3"))
	if err != nil {
		t.Fatal("append", err)
	}
	t.Log("ok", id, eid, "bookies", len(bookies))
}
