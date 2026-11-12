package metadata

import (
	"context"
	"strings"
	"sync"
)

type memEntry struct {
	value   []byte
	version uint64
}

// MemoryStore is process-local CAS KV for tests and single-broker embeds.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]memEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]memEntry{}}
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return nil, 0, ErrNotFound
	}
	out := append([]byte(nil), e.value...)
	return out, e.version, nil
}

func (s *MemoryStore) Cas(_ context.Context, key string, expectVersion uint64, value []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		if expectVersion != 0 {
			return 0, ErrVersionConflict
		}
		s.data[key] = memEntry{value: append([]byte(nil), value...), version: 1}
		return 1, nil
	}
	if e.version != expectVersion {
		return 0, ErrVersionConflict
	}
	nv := e.version + 1
	s.data[key] = memEntry{value: append([]byte(nil), value...), version: nv}
	return nv, nil
}

func (s *MemoryStore) Delete(_ context.Context, key string, expectVersion uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok {
		return ErrNotFound
	}
	if e.version != expectVersion {
		return ErrVersionConflict
	}
	delete(s.data, key)
	return nil
}

func (s *MemoryStore) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
