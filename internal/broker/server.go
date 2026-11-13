package broker

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/devotionalist/open-message/internal/metadata"
	"github.com/devotionalist/open-message/internal/olclient"
	"github.com/devotionalist/open-message/internal/wire"
)

func (b *Broker) acceptLoop(ctx context.Context) {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		if !b.accepting.Load() {
			_ = conn.Close()
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.serveConn(ctx, conn)
		}()
	}
}

type connState struct {
	b          *Broker
	conn       net.Conn
	mu         sync.Mutex
	producers  map[uint64]prod
	consumers  map[uint64]*liveConsumer
	nextProdID atomic.Uint64
	nextConsID atomic.Uint64
}

type prod struct {
	topic      string
	name       string
	idempotent bool
}

type liveConsumer struct {
	id     uint64
	topic  string
	sub    string
	ch     chan delivered
	cancel context.CancelFunc
}

func (b *Broker) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	cs := &connState{b: b, conn: conn, producers: map[uint64]prod{}, consumers: map[uint64]*liveConsumer{}}
	for {
		fr, err := wire.Decode(conn)
		if err != nil {
			return
		}
		if err := cs.handle(ctx, fr); err != nil {
			return
		}
	}
}

func (cs *connState) write(h wire.Header, payload []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	buf, err := wire.Encode(h, payload)
	if err != nil {
		return err
	}
	_, err = cs.conn.Write(buf)
	return err
}

func (cs *connState) handle(ctx context.Context, fr wire.Frame) error {
	switch fr.Header.Type {
	case wire.TypeConnect:
		body, _ := wire.DecodeBody[wire.ConnectBody](fr.Header)
		if cs.b.cfg.AuthToken != "" && body.AuthToken != cs.b.cfg.AuthToken {
			h := wire.Header{Type: wire.TypeConnected, RequestID: fr.Header.RequestID, BrokerErr: wire.AuthFailed}
			return cs.write(h, nil)
		}
		bb, _ := wire.EncodeBody(wire.ConnectedBody{ProtocolVersion: 1, MaxMessageSize: 5 << 20, ServerVersion: "open-message/0.1"})
		return cs.write(wire.Header{Type: wire.TypeConnected, RequestID: fr.Header.RequestID, Body: bb}, nil)
	case wire.TypePing:
		return cs.write(wire.Header{Type: wire.TypePong, RequestID: fr.Header.RequestID}, nil)
	case wire.TypeLookup:
		body, _ := wire.DecodeBody[wire.LookupBody](fr.Header)
		url, err := cs.b.Lookup(ctx, body.Topic)
		code := wire.OK
		if err != nil {
			code = wire.Unavailable
		}
		bb, _ := wire.EncodeBody(wire.LookupResponseBody{Topic: body.Topic, BrokerURL: url})
		return cs.write(wire.Header{Type: wire.TypeLookupResponse, RequestID: fr.Header.RequestID, BrokerErr: code, Body: bb}, nil)
	case wire.TypeProducer:
		body, _ := wire.DecodeBody[wire.ProducerBody](fr.Header)
		if _, err := cs.b.getOrLoadTopic(ctx, mustParse(body.Topic)); err != nil {
			if isNotOwner(err) {
				return cs.write(wire.Header{Type: wire.TypeProducerSuccess, RequestID: fr.Header.RequestID, BrokerErr: wire.NotOwner}, nil)
			}
			return cs.write(wire.Header{Type: wire.TypeProducerSuccess, RequestID: fr.Header.RequestID, BrokerErr: wire.Unavailable}, nil)
		}
		pid := body.ProducerID
		if pid == 0 {
			pid = cs.nextProdID.Add(1)
		}
		cs.mu.Lock()
		cs.producers[pid] = prod{topic: body.Topic, name: body.ProducerName, idempotent: body.EnableIdempotency}
		cs.mu.Unlock()
		bb, _ := wire.EncodeBody(wire.ProducerSuccessBody{ProducerID: pid, ProducerName: body.ProducerName})
		return cs.write(wire.Header{Type: wire.TypeProducerSuccess, RequestID: fr.Header.RequestID, Body: bb}, nil)
	case wire.TypeSend:
		body, _ := wire.DecodeBody[wire.SendBody](fr.Header)
		cs.mu.Lock()
		p, ok := cs.producers[body.ProducerID]
		cs.mu.Unlock()
		if !ok {
			bb, _ := wire.EncodeBody(wire.SendErrorBody{ProducerID: body.ProducerID, SequenceID: body.SequenceID, Code: wire.InvalidFrame, Message: "unknown producer"})
			return cs.write(wire.Header{Type: wire.TypeSendError, RequestID: fr.Header.RequestID, Body: bb}, nil)
		}
		lid, eid, err := cs.b.Produce(ctx, p.topic, p.name, body.SequenceID, p.idempotent, fr.Payload)
		if err != nil {
			code := mapProduceErr(err)
			bb, _ := wire.EncodeBody(wire.SendErrorBody{ProducerID: body.ProducerID, SequenceID: body.SequenceID, Code: code, Message: err.Error()})
			return cs.write(wire.Header{Type: wire.TypeSendError, RequestID: fr.Header.RequestID, BrokerErr: code, Body: bb}, nil)
		}
		bb, _ := wire.EncodeBody(wire.SendReceiptBody{ProducerID: body.ProducerID, SequenceID: body.SequenceID, LedgerID: lid, EntryID: eid})
		return cs.write(wire.Header{Type: wire.TypeSendReceipt, RequestID: fr.Header.RequestID, Body: bb}, nil)
	case wire.TypeSubscribe:
		body, _ := wire.DecodeBody[wire.SubscribeBody](fr.Header)
		tn := mustParse(body.Topic)
		rt, err := cs.b.getOrLoadTopic(ctx, tn)
		if err != nil {
			code := wire.Unavailable
			if isNotOwner(err) {
				code = wire.NotOwner
			}
			return cs.write(wire.Header{Type: wire.TypeSubscribeSuccess, RequestID: fr.Header.RequestID, BrokerErr: code}, nil)
		}
		cid := body.ConsumerID
		if cid == 0 {
			cid = cs.nextConsID.Add(1)
		}
		ch := make(chan delivered, 64)
		cctx, cancel := context.WithCancel(ctx)
		lc := &liveConsumer{id: cid, topic: body.Topic, sub: body.Subscription, ch: ch, cancel: cancel}
		mode := body.Mode
		if mode == "" {
			mode = "Exclusive"
		}
		csState := &consumerState{id: cid, sub: body.Subscription, ch: ch, mode: mode, read: metadata.Position{EntryID: -1}}
		if err := rt.disp.subscribe(csState); err != nil {
			cancel()
			return cs.write(wire.Header{Type: wire.TypeSubscribeSuccess, RequestID: fr.Header.RequestID, BrokerErr: wire.ConsumerBusy}, nil)
		}
		cs.mu.Lock()
		cs.consumers[cid] = lc
		cs.mu.Unlock()
		go cs.pushMessages(cctx, cid, ch)
		bb, _ := wire.EncodeBody(wire.SubscribeSuccessBody{ConsumerID: cid})
		return cs.write(wire.Header{Type: wire.TypeSubscribeSuccess, RequestID: fr.Header.RequestID, Body: bb}, nil)
	case wire.TypeFlow:
		body, _ := wire.DecodeBody[wire.FlowBody](fr.Header)
		tnTopic := ""
		cs.mu.Lock()
		if c, ok := cs.consumers[body.ConsumerID]; ok {
			tnTopic = c.topic
		}
		cs.mu.Unlock()
		if tnTopic == "" {
			return nil
		}
		rt, err := cs.b.getOrLoadTopic(ctx, mustParse(tnTopic))
		if err == nil {
			rt.disp.flow(body.ConsumerID, body.Permits)
		}
		return nil
	case wire.TypeAck:
		body, _ := wire.DecodeBody[wire.AckBody](fr.Header)
		cs.mu.Lock()
		c, ok := cs.consumers[body.ConsumerID]
		cs.mu.Unlock()
		if ok {
			rt, err := cs.b.getOrLoadTopic(ctx, mustParse(c.topic))
			if err == nil {
				tn := mustParse(c.topic)
				if body.Cumulative != nil {
					pos := metadata.Position{LedgerID: body.Cumulative.LedgerID, EntryID: body.Cumulative.EntryID}
					cs.b.flush.Enqueue(tn.Tenant, tn.Namespace, tn.Topic, c.sub, pos)
					_ = rt.disp.ackCumulative(ctx, body.ConsumerID, pos)
				}
				for _, ind := range body.Individual {
					pos := metadata.Position{LedgerID: ind.LedgerID, EntryID: ind.EntryID}
					_ = rt.disp.ackIndividual(ctx, body.ConsumerID, pos)
				}
			}
		}
		bb, _ := wire.EncodeBody(wire.AckResponseBody{ConsumerID: body.ConsumerID})
		return cs.write(wire.Header{Type: wire.TypeAckResponse, RequestID: fr.Header.RequestID, Body: bb}, nil)
	default:
		return cs.write(wire.Header{Type: fr.Header.Type, RequestID: fr.Header.RequestID, BrokerErr: wire.UnsupportedVer}, nil)
	}
}

func (cs *connState) pushMessages(ctx context.Context, cid uint64, ch <-chan delivered) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-ch:
			if !ok {
				return
			}
			bb, _ := wire.EncodeBody(wire.MessageBody{ConsumerID: cid, LedgerID: d.LedgerID, EntryID: d.EntryID})
			if err := cs.write(wire.Header{Type: wire.TypeMessage, Body: bb}, d.Payload); err != nil {
				return
			}
		}
	}
}

func mustParse(s string) TopicName {
	tn, err := ParseTopic(s)
	if err != nil {
		return TopicName{}
	}
	return tn
}

func isNotOwner(err error) bool {
	return err != nil && (errors.Is(err, metadata.ErrCasRejected) || contains(err.Error(), "not owner"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && containsFold(s, sub)))
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mapProduceErr(err error) wire.ErrorCode {
	if errors.Is(err, olclient.ErrFenced) {
		return wire.Fenced
	}
	if errors.Is(err, olclient.ErrUnavailable) {
		return wire.Unavailable
	}
	if isNotOwner(err) {
		return wire.NotOwner
	}
	return wire.Unavailable
}

// Keep unused import happy when Decode returns EOF
var _ = io.EOF
