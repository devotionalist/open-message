package broker

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// TopicName is tenant/namespace/topic.
type TopicName struct {
	Tenant    string
	Namespace string
	Topic     string
}

func ParseTopic(s string) (TopicName, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return TopicName{}, fmt.Errorf("topic must be tenant/namespace/topic")
	}
	return TopicName{Tenant: parts[0], Namespace: parts[1], Topic: parts[2]}, nil
}

func (t TopicName) String() string {
	return t.Tenant + "/" + t.Namespace + "/" + t.Topic
}

func (t TopicName) BundleID() string {
	// Partitioned physical topics hash into bundles; default single full-range bundle.
	if strings.Contains(t.Topic, "-partition-") {
		h := fnv.New32a()
		_, _ = h.Write([]byte(t.Topic))
		// Still one bundle for M2 simplicity; split API can refine ranges later.
		_ = h
	}
	return "00000000-ffffffff"
}

// PartitionName returns physical partition topic.
func PartitionName(base TopicName, i int) TopicName {
	return TopicName{Tenant: base.Tenant, Namespace: base.Namespace, Topic: fmt.Sprintf("%s-partition-%d", base.Topic, i)}
}
