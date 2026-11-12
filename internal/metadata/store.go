package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrNotFound       = errors.New("metadata: not found")
	ErrVersionConflict = errors.New("metadata: version conflict")
	ErrCasRejected    = errors.New("metadata: cas rejected")
)

// RawStore is the low-level CAS KV used by typed stores.
type RawStore interface {
	Get(ctx context.Context, key string) (value []byte, version uint64, err error)
	Cas(ctx context.Context, key string, expectVersion uint64, value []byte) (newVersion uint64, err error)
	Delete(ctx context.Context, key string, expectVersion uint64) error
	ListPrefix(ctx context.Context, prefix string) (keys []string, err error)
}

// Store groups typed OM metadata APIs.
type Store struct {
	Raw   RawStore
	Clock interface{ NowUnixMilli() int64 }
}

func OwnershipKey(ns, bundleID string) string {
	return fmt.Sprintf("own/%s/%s", ns, bundleID)
}

func IndexKey(tenant, ns, topic string) string {
	return fmt.Sprintf("idx/%s/%s/%s", tenant, ns, topic)
}

func CursorKey(tenant, ns, topic, sub string) string {
	return fmt.Sprintf("cur/%s/%s/%s/%s", tenant, ns, topic, sub)
}

func NodeKey(brokerID string) string {
	return fmt.Sprintf("node/broker/%s", brokerID)
}

func getJSON[T any](ctx context.Context, raw RawStore, key string) (Record[T], error) {
	b, ver, err := raw.Get(ctx, key)
	if err != nil {
		return Record[T]{}, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return Record[T]{}, err
	}
	return Record[T]{Value: v, Version: ver}, nil
}

func casJSON[T any](ctx context.Context, raw RawStore, key string, expect uint64, v T) (uint64, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return raw.Cas(ctx, key, expect, b)
}
