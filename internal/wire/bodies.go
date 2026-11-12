package wire

// Connect / Connected
type ConnectBody struct {
	ProtocolVersion uint32 `json:"protocolVersion"`
	ClientVersion   string `json:"clientVersion"`
	AuthToken       string `json:"authToken,omitempty"`
	ProxyToBroker   string `json:"proxyToBroker,omitempty"`
}

type ConnectedBody struct {
	ProtocolVersion uint32 `json:"protocolVersion"`
	MaxMessageSize  uint32 `json:"maxMessageSize"`
	ServerVersion   string `json:"serverVersion"`
}

type LookupBody struct {
	Topic string `json:"topic"`
}

type LookupResponseBody struct {
	Topic     string `json:"topic"`
	BrokerURL string `json:"brokerUrl"`
}

type ProducerBody struct {
	Topic              string `json:"topic"`
	ProducerName       string `json:"producerName"`
	ProducerID         uint64 `json:"producerId"`
	EnableIdempotency  bool   `json:"enableIdempotency"`
	InitialSequenceID  uint64 `json:"initialSequenceId,omitempty"`
}

type ProducerSuccessBody struct {
	ProducerID   uint64 `json:"producerId"`
	ProducerName string `json:"producerName"`
}

type SendBody struct {
	ProducerID uint64 `json:"producerId"`
	SequenceID uint64 `json:"sequenceId"`
}

type SendReceiptBody struct {
	ProducerID uint64 `json:"producerId"`
	SequenceID uint64 `json:"sequenceId"`
	LedgerID   uint64 `json:"ledgerId"`
	EntryID    uint64 `json:"entryId"`
}

type SendErrorBody struct {
	ProducerID uint64    `json:"producerId"`
	SequenceID uint64    `json:"sequenceId"`
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
}

type SubscribeBody struct {
	Topic            string `json:"topic"`
	Subscription     string `json:"subscription"`
	Mode             string `json:"mode"` // Exclusive|Failover|Shared|Key_Shared
	InitialPosition  string `json:"initialPosition"`
	ConsumerID       uint64 `json:"consumerId"`
	ConsumerName     string `json:"consumerName,omitempty"`
}

type SubscribeSuccessBody struct {
	ConsumerID uint64 `json:"consumerId"`
}

type FlowBody struct {
	ConsumerID uint64 `json:"consumerId"`
	Permits    int32  `json:"permits"`
}

type MessageBody struct {
	ConsumerID  uint64 `json:"consumerId"`
	LedgerID    uint64 `json:"ledgerId"`
	EntryID     uint64 `json:"entryId"`
	Redelivery  uint32 `json:"redelivery"`
}

type AckBody struct {
	ConsumerID uint64 `json:"consumerId"`
	Cumulative *struct {
		LedgerID uint64 `json:"ledgerId"`
		EntryID  int64  `json:"entryId"`
	} `json:"cumulative,omitempty"`
	Individual []struct {
		LedgerID uint64 `json:"ledgerId"`
		EntryID  int64  `json:"entryId"`
	} `json:"individual,omitempty"`
}

type AckResponseBody struct {
	ConsumerID uint64 `json:"consumerId"`
}
