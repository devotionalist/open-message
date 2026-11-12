package metadata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oxia-db/oxia/oxia"
)

// OxiaStore implements RawStore on Oxia (default METADATA_BACKEND=oxia).
// Keys are prefixed (default "om/") so they do not collide with open-ledger ledgers.
type OxiaStore struct {
	client oxia.SyncClient
	prefix string
}

// NewOxiaStore dials Oxia serviceAddress (host:port).
func NewOxiaStore(serviceAddress, prefix string) (*OxiaStore, error) {
	if prefix == "" {
		prefix = "/om/" // isolate from open-ledger `/open-ledger/...`
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	c, err := oxia.NewSyncClient(serviceAddress)
	if err != nil {
		return nil, err
	}
	return &OxiaStore{client: c, prefix: prefix}, nil
}

func (s *OxiaStore) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *OxiaStore) full(key string) string {
	return s.prefix + key
}

func (s *OxiaStore) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	_, val, ver, err := s.client.Get(ctx, s.full(key))
	if err != nil {
		if errors.Is(err, oxia.ErrKeyNotFound) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	if ver.VersionId < 0 {
		return nil, 0, fmt.Errorf("oxia: negative version id %d", ver.VersionId)
	}
	return append([]byte(nil), val...), uint64(ver.VersionId), nil
}

func (s *OxiaStore) Cas(ctx context.Context, key string, expectVersion uint64, value []byte) (uint64, error) {
	fk := s.full(key)
	var opts []oxia.PutOption
	if expectVersion == 0 {
		opts = append(opts, oxia.ExpectedRecordNotExists())
	} else {
		opts = append(opts, oxia.ExpectedVersionId(int64(expectVersion)))
	}
	_, ver, err := s.client.Put(ctx, fk, value, opts...)
	if err != nil {
		if errors.Is(err, oxia.ErrUnexpectedVersionId) {
			return 0, ErrVersionConflict
		}
		return 0, err
	}
	if ver.VersionId < 0 {
		return 0, fmt.Errorf("oxia: negative version id %d", ver.VersionId)
	}
	return uint64(ver.VersionId), nil
}

func (s *OxiaStore) Delete(ctx context.Context, key string, expectVersion uint64) error {
	fk := s.full(key)
	var opts []oxia.DeleteOption
	if expectVersion != 0 {
		opts = append(opts, oxia.ExpectedVersionId(int64(expectVersion)))
	}
	err := s.client.Delete(ctx, fk, opts...)
	if err != nil {
		if errors.Is(err, oxia.ErrKeyNotFound) {
			return ErrNotFound
		}
		if errors.Is(err, oxia.ErrUnexpectedVersionId) {
			return ErrVersionConflict
		}
		return err
	}
	return nil
}

func (s *OxiaStore) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	min := s.full(prefix)
	// Exclusive end just past the prefix for hierarchical keys.
	max := min + "\xff"
	keys, err := s.client.List(ctx, min, max)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.HasPrefix(k, s.prefix) {
			out = append(out, strings.TrimPrefix(k, s.prefix))
		}
	}
	return out, nil
}
