package metadata

import "context"

func (s *Store) PutNode(ctx context.Context, brokerID string, node BrokerNode) error {
	key := NodeKey(brokerID)
	rec, err := getJSON[BrokerNode](ctx, s.Raw, key)
	expect := uint64(0)
	if err == nil {
		expect = rec.Version
	} else if err != ErrNotFound && !isNotFound(err) {
		return err
	}
	_, err = casJSON(ctx, s.Raw, key, expect, node)
	return err
}

func (s *Store) ListBrokers(ctx context.Context) ([]string, error) {
	return s.Raw.ListPrefix(ctx, "node/broker/")
}

func isNotFound(err error) bool {
	return err == ErrNotFound
}
