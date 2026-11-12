package metadata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	kvv1 "github.com/devotionalist/openkv-client-go/gen/kv/v1"
	okv "github.com/devotionalist/openkv-client-go/kv"
)

// OpenKVStore implements RawStore on OpenKV Gateway (METADATA_BACKEND=openkv).
// Keys are prefixed (default "/om/").
//
// Mapping (design/02 §9.3):
//
//	Cas  → PutCAS(expected_revision); expect=0 create-only
//	Get  → Get.revision (Gateway MVCC commit_ts)
//	Delete → DeleteCAS / Delete
//	ListPrefix → native Gateway Scan (Prefix + StartAfter pagination)
type OpenKVStore struct {
	client *okv.Client
	prefix string
}

// NewOpenKVStore dials OpenKV Compute Gateway (host:port, typically :19000).
func NewOpenKVStore(ctx context.Context, gateway, prefix string) (*OpenKVStore, error) {
	if gateway == "" {
		return nil, fmt.Errorf("openkv: empty gateway address")
	}
	if prefix == "" {
		prefix = "/om/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	cli, err := okv.Dial(ctx, gateway, okv.WithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	return &OpenKVStore{client: cli, prefix: prefix}, nil
}

func (s *OpenKVStore) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *OpenKVStore) full(key string) string {
	return s.prefix + key
}

func (s *OpenKVStore) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	val, found, rev, err := s.client.Get(ctx, []byte(s.full(key)))
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, ErrNotFound
	}
	return append([]byte(nil), val...), rev, nil
}

func (s *OpenKVStore) Cas(ctx context.Context, key string, expectVersion uint64, value []byte) (uint64, error) {
	rev, err := s.client.PutCAS(ctx, []byte(s.full(key)), value, expectVersion)
	if err != nil {
		var ce *okv.CasError
		if errors.As(err, &ce) {
			return 0, ErrVersionConflict
		}
		return 0, err
	}
	return rev, nil
}

func (s *OpenKVStore) Delete(ctx context.Context, key string, expectVersion uint64) error {
	fk := []byte(s.full(key))
	var (
		deleted bool
		err     error
	)
	if expectVersion == 0 {
		deleted, _, err = s.client.Delete(ctx, fk)
	} else {
		deleted, _, err = s.client.DeleteCAS(ctx, fk, expectVersion)
	}
	if err != nil {
		var ce *okv.CasError
		if errors.As(err, &ce) {
			return ErrVersionConflict
		}
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

func (s *OpenKVStore) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := []byte(s.full(prefix))
	var (
		out        []string
		startAfter []byte
	)
	for {
		res, err := s.client.Scan(ctx, &kvv1.ScanRequest{
			Prefix:     fullPrefix,
			StartAfter: startAfter,
			Limit:      256,
			KeysOnly:   true,
		})
		if err != nil {
			return nil, err
		}
		for _, p := range res.Pairs {
			k := string(p.Key)
			if !strings.HasPrefix(k, s.prefix) {
				continue
			}
			rel := strings.TrimPrefix(k, s.prefix)
			if strings.HasPrefix(rel, prefix) {
				out = append(out, rel)
			}
		}
		if !res.Truncated || len(res.NextKey) == 0 {
			break
		}
		startAfter = append([]byte(nil), res.NextKey...)
	}
	return out, nil
}
