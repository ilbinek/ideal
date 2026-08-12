package steamrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/steamlang"
)

func TestCallDisconnectedReturnsImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Call(ctx, steam.NewClient(), steamlang.EMsg_ClientHeartBeat, "",
			&protobuf.CMsgClientHeartBeat{}, &protobuf.CMsgClientHeartBeat{})
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when CM is down")
		}
	case <-ctx.Done():
		t.Fatal("Call hung on a disconnected client")
	}
}

func TestCallNilClient(t *testing.T) {
	err := Call(context.Background(), nil, steamlang.EMsg_ClientHeartBeat, "",
		&protobuf.CMsgClientHeartBeat{}, &protobuf.CMsgClientHeartBeat{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallRespectsContextDeadline(t *testing.T) {
	// Disconnected path must not wait for DefaultCallTimeout.
	start := time.Now()
	err := Call(context.Background(), steam.NewClient(), steamlang.EMsg_ClientHeartBeat, "",
		&protobuf.CMsgClientHeartBeat{}, &protobuf.CMsgClientHeartBeat{})
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took too long: %v (err=%v)", time.Since(start), err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("should fail as not connected, not wait for deadline: %v", err)
	}
}
