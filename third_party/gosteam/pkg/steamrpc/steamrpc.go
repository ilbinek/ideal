// Package steamrpc provides a small synchronous request/response helper over the
// vendored go-steam client's job mechanism, used by the cdn and workshop
// packages to call Steam service methods and classic client messages.
package steamrpc

import (
	"context"
	"fmt"
	"time"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol"
	"github.com/0xAozora/go-steam/protocol/steamlang"
	"google.golang.org/protobuf/proto"
)

// DefaultCallTimeout is used when Call's context has no deadline, so a dead
// CM connection cannot block a download forever.
const DefaultCallTimeout = 60 * time.Second

// Call sends a job-based request to Steam over the CM connection and waits for
// the matching response, unmarshalling it into resp.
//
// For unified service methods (e.g. "ContentServerDirectory.GetManifestRequestCode#1")
// pass reqEMsg = EMsg_ServiceMethodCallFromClient and the method name as jobName.
// For classic client messages (e.g. the depot key request) pass the specific
// EMsg and an empty jobName. Responses are correlated by source/target job id,
// which the fork's read loop dispatches via its JobHandlers map.
func Call(ctx context.Context, sc *steam.Client, reqEMsg steamlang.EMsg, jobName string, req, resp proto.Message) error {
	if sc == nil || !sc.Connected() {
		return fmt.Errorf("steamrpc: not connected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCallTimeout)
		defer cancel()
	}

	msg := protocol.NewClientMsgProtobuf(reqEMsg, req)
	if jobName != "" {
		msg.Header.Proto.TargetJobName = &jobName
	}
	jobID := sc.GetNextJobId()
	msg.SetSourceJobId(jobID)

	done := make(chan struct{})
	sc.JobMutex.Lock()
	sc.JobHandlers[uint64(jobID)] = func(p *protocol.Packet) error {
		p.ReadProtoMsg(resp)
		close(done)
		return nil
	}
	sc.JobMutex.Unlock()

	unregister := func() {
		sc.JobMutex.Lock()
		delete(sc.JobHandlers, uint64(jobID))
		sc.JobMutex.Unlock()
	}

	if err := sc.Send(msg); err != nil {
		unregister()
		return err
	}

	select {
	case <-ctx.Done():
		unregister()
		return ctx.Err()
	case <-done:
		return nil
	}
}
