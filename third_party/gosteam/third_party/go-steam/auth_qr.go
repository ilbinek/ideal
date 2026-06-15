package steam

import (
	"sync/atomic"
	"time"

	"github.com/0xAozora/go-steam/protocol"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/steamlang"
	"github.com/0xAozora/go-steam/steamid"
	"google.golang.org/protobuf/proto"
)

// LogOnQR starts a QR-code login. onChallenge is invoked with the challenge URL
// (encode it as a QR code for the Steam mobile app to scan); it may be called
// again if Steam rotates the challenge URL while polling. On a successful scan
// and approval, details is populated with the resulting RefreshToken/GuardData
// and a normal logon is performed, which emits a LoggedOnEvent.
func (a *Auth) LogOnQR(details *LogOnDetails, onChallenge func(string)) error {
	atomic.StoreUint64(&a.Client.steamId, uint64(steamid.NewIdAdv(0, 1, int32(steamlang.EUniverse_Public), int32(steamlang.EAccountType_Individual))))

	hello := &protobuf.CMsgClientHello{ProtocolVersion: proto.Uint32(steamlang.MsgClientLogon_CurrentProtocol)}
	if err := a.Client.Send(protocol.NewClientMsgProtobuf(steamlang.EMsg_ClientHello, hello)); err != nil {
		return err
	}

	a.Details = details
	a.onChallenge = onChallenge
	return a.beginQRSession()
}

func (a *Auth) beginQRSession() error {
	platformType := protobuf.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient.Enum()
	deviceFriendlyName := "DESKTOP-HELLO"

	req := &protobuf.CAuthentication_BeginAuthSessionViaQR_Request{
		DeviceFriendlyName: &deviceFriendlyName,
		PlatformType:       platformType,
		WebsiteId:          proto.String("Client"),
		DeviceDetails: &protobuf.CAuthentication_DeviceDetails{
			DeviceFriendlyName: &deviceFriendlyName,
			PlatformType:       platformType,
			OsType:             proto.Int32(16),
		},
	}

	msg := protocol.NewClientMsgProtobuf(steamlang.EMsg_ServiceMethodCallFromClientNonAuthed, req)
	jobname := "Authentication.BeginAuthSessionViaQR#1"
	msg.Header.Proto.TargetJobName = &jobname
	jobID := a.Client.GetNextJobId()
	msg.SetSourceJobId(jobID)

	a.Client.JobMutex.Lock()
	a.Client.JobHandlers[uint64(jobID)] = a.handleQRSession
	a.Client.JobMutex.Unlock()

	return a.Client.Send(msg)
}

func (a *Auth) handleQRSession(packet *protocol.Packet) error {
	body := new(protobuf.CAuthentication_BeginAuthSessionViaQR_Response)
	_ = packet.ReadProtoMsg(body)

	a.qrClientID = body.GetClientId()
	a.qrRequestID = body.RequestId
	a.qrInterval = body.GetInterval()

	if a.onChallenge != nil && body.ChallengeUrl != nil {
		a.onChallenge(*body.ChallengeUrl)
	}

	go a.qrPollLoop()
	return nil
}

func (a *Auth) qrPollLoop() {
	interval := time.Duration(a.qrInterval * float32(time.Second))
	if interval <= 0 {
		interval = 2 * time.Second
	}
	a.pollStop = make(chan struct{})

	for {
		select {
		case <-a.pollStop:
			return
		case <-time.After(interval):
			if err := a.sendQRPoll(); err != nil {
				a.Client.Fatalf("qr poll: %v", err)
				return
			}
		}
	}
}

func (a *Auth) sendQRPoll() error {
	req := &protobuf.CAuthentication_PollAuthSessionStatus_Request{
		ClientId:  proto.Uint64(a.qrClientID),
		RequestId: a.qrRequestID,
	}

	msg := protocol.NewClientMsgProtobuf(steamlang.EMsg_ServiceMethodCallFromClientNonAuthed, req)
	jobname := "Authentication.PollAuthSessionStatus#1"
	msg.Header.Proto.TargetJobName = &jobname
	jobID := a.Client.GetNextJobId()
	msg.SetSourceJobId(jobID)

	a.Client.JobMutex.Lock()
	a.Client.JobHandlers[uint64(jobID)] = a.handleQRPoll
	a.Client.JobMutex.Unlock()

	return a.Client.Send(msg)
}

func (a *Auth) handleQRPoll(packet *protocol.Packet) error {
	body := new(protobuf.CAuthentication_PollAuthSessionStatus_Response)
	_ = packet.ReadProtoMsg(body)

	if body.NewClientId != nil {
		a.qrClientID = *body.NewClientId
	}
	if body.NewChallengeUrl != nil && a.onChallenge != nil {
		a.onChallenge(*body.NewChallengeUrl)
	}

	if body.RefreshToken == nil {
		// Not approved yet; the poll loop will try again.
		return nil
	}

	close(a.pollStop)

	a.Details.AccessToken = body.GetAccessToken()
	a.Details.RefreshToken = *body.RefreshToken
	if body.NewGuardData != nil {
		a.Details.GuardData = *body.NewGuardData
	}
	if body.AccountName != nil {
		a.Details.Username = *body.AccountName
	}

	return a.LogOn(a.Details)
}
