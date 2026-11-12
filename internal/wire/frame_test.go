package wire_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/devotionalist/open-message/internal/wire"
)

func TestFrameRoundTripMagic(t *testing.T) {
	body, _ := wire.EncodeBody(wire.ConnectBody{ProtocolVersion: 1, ClientVersion: "t"})
	buf, err := wire.Encode(wire.Header{Type: wire.TypeConnect, RequestID: 7, Body: body}, []byte("pay"))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(buf[0:2]) != wire.Magic {
		t.Fatal("magic")
	}
	fr, err := wire.Decode(bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	if fr.Header.RequestID != 7 || string(fr.Payload) != "pay" {
		t.Fatalf("%+v", fr)
	}
}
