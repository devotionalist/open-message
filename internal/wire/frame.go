package wire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Magic   = 0x4F4D // 'O''M'
	Version = 1
)

var ErrInvalidFrame = errors.New("wire: invalid frame")

// Type is command type.
type Type string

const (
	TypeConnect          Type = "Connect"
	TypeConnected        Type = "Connected"
	TypePing             Type = "Ping"
	TypePong             Type = "Pong"
	TypeLookup           Type = "Lookup"
	TypeLookupResponse   Type = "LookupResponse"
	TypeProducer         Type = "Producer"
	TypeProducerSuccess  Type = "ProducerSuccess"
	TypeSend             Type = "Send"
	TypeSendReceipt      Type = "SendReceipt"
	TypeSendError        Type = "SendError"
	TypeSubscribe        Type = "Subscribe"
	TypeSubscribeSuccess Type = "SubscribeSuccess"
	TypeFlow             Type = "Flow"
	TypeMessage          Type = "Message"
	TypeAck              Type = "Ack"
	TypeAckResponse      Type = "AckResponse"
	TypeCloseProducer    Type = "CloseProducer"
	TypeCloseConsumer    Type = "CloseConsumer"
)

// ErrorCode matches design/21 §5.
type ErrorCode string

const (
	OK             ErrorCode = "OK"
	NotOwner       ErrorCode = "NotOwner"
	Fenced         ErrorCode = "Fenced"
	Timeout        ErrorCode = "Timeout"
	Unavailable    ErrorCode = "Unavailable"
	QuotaExceeded  ErrorCode = "QuotaExceeded"
	AuthFailed     ErrorCode = "AuthFailed"
	TopicNotFound  ErrorCode = "TopicNotFound"
	InvalidFrame   ErrorCode = "InvalidFrame"
	UnsupportedVer ErrorCode = "UnsupportedVersion"
	ProducerBusy   ErrorCode = "ProducerBusy"
	ConsumerBusy   ErrorCode = "ConsumerBusy"
	ProducerFenced ErrorCode = "ProducerFenced"
	ChecksumError  ErrorCode = "ChecksumError"
)

func (e ErrorCode) Retryable() bool {
	switch e {
	case NotOwner, Fenced, Timeout, Unavailable, QuotaExceeded:
		return true
	default:
		return false
	}
}

// Header is the JSON command metadata inside an OM frame.
type Header struct {
	Type        Type            `json:"type"`
	RequestID   uint64          `json:"requestId"`
	Correlation uint64          `json:"correlation,omitempty"`
	BrokerErr   ErrorCode       `json:"brokerErr,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
}

// Frame is a decoded OM frame.
type Frame struct {
	Header  Header
	Payload []byte
}

func Encode(h Header, payload []byte) ([]byte, error) {
	hb, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	if len(hb) > 0xffff {
		return nil, ErrInvalidFrame
	}
	tl := 2 + len(hb) + len(payload)
	out := make([]byte, 7+tl)
	binary.BigEndian.PutUint16(out[0:2], Magic)
	out[2] = Version
	binary.BigEndian.PutUint32(out[3:7], uint32(tl))
	binary.BigEndian.PutUint16(out[7:9], uint16(len(hb)))
	copy(out[9:], hb)
	copy(out[9+len(hb):], payload)
	return out, nil
}

func Decode(r io.Reader) (Frame, error) {
	var pref [7]byte
	if _, err := io.ReadFull(r, pref[:]); err != nil {
		return Frame{}, err
	}
	if binary.BigEndian.Uint16(pref[0:2]) != Magic {
		return Frame{}, fmt.Errorf("%w: bad magic", ErrInvalidFrame)
	}
	if pref[2] != Version {
		return Frame{}, fmt.Errorf("%w: bad version", ErrInvalidFrame)
	}
	total := binary.BigEndian.Uint32(pref[3:7])
	if total < 2 || total > 8<<20 {
		return Frame{}, ErrInvalidFrame
	}
	body := make([]byte, total)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	hlen := binary.BigEndian.Uint16(body[0:2])
	if int(hlen)+2 > len(body) {
		return Frame{}, ErrInvalidFrame
	}
	var h Header
	if err := json.Unmarshal(body[2:2+hlen], &h); err != nil {
		return Frame{}, err
	}
	payload := append([]byte(nil), body[2+hlen:]...)
	return Frame{Header: h, Payload: payload}, nil
}

func EncodeBody(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

func DecodeBody[T any](h Header) (T, error) {
	var v T
	if len(h.Body) == 0 {
		return v, nil
	}
	err := json.Unmarshal(h.Body, &v)
	return v, err
}
