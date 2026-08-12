package steam

import (
	"errors"
	"testing"

	"github.com/0xAozora/go-steam/protocol"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/steamlang"
)

func TestSendNotConnected(t *testing.T) {
	c := NewClient()
	err := c.Send(protocol.NewClientMsgProtobuf(steamlang.EMsg_ClientHeartBeat, new(protobuf.CMsgClientHeartBeat)))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Send = %v, want ErrNotConnected", err)
	}
}
