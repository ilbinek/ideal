package rcon

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 300 * time.Millisecond

// fakeServer is a UDP peer that speaks just enough BE RCon.
type fakeServer struct {
	conn     *net.UDPConn
	password string
	silent   atomic.Bool
	commands chan string
	acks     chan byte // sequence numbers of message acks received
	preAck   atomic.Bool
	noAck    atomic.Bool
}

func newFakeServer(t *testing.T, password string) *fakeServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{conn: conn, password: password, commands: make(chan string, 8), acks: make(chan byte, 8)}
	t.Cleanup(func() { conn.Close() })
	go s.serve()
	return s
}

func (s *fakeServer) port() int { return s.conn.LocalAddr().(*net.UDPAddr).Port }

func (s *fakeServer) serve() {
	var buf [maxPacketSize]byte
	for {
		n, addr, err := s.conn.ReadFromUDP(buf[:])
		if err != nil {
			return
		}
		kind, payload, err := decodePacket(buf[:n])
		if err != nil || s.silent.Load() {
			continue
		}
		switch kind {
		case typeLogin:
			result := byte(0x00)
			if string(payload) == s.password {
				result = 0x01
			}
			_, _ = s.conn.WriteToUDP(encodePacket(typeLogin, []byte{result}), addr)
		case typeCommand:
			seq := payload[0]
			s.commands <- string(payload[1:])
			if s.preAck.Load() {
				// A chat message crossing the command on the wire, then an ack for some
				// other command, neither of which may be taken for this one's.
				_, _ = s.conn.WriteToUDP(encodePacket(typeMessage, []byte{7, 'h', 'i'}), addr)
				_, _ = s.conn.WriteToUDP(encodePacket(typeCommand, []byte{seq + 1}), addr)
			}
			if !s.noAck.Load() {
				_, _ = s.conn.WriteToUDP(encodePacket(typeCommand, []byte{seq}), addr)
			}
		case typeMessage:
			s.acks <- payload[0]
		}
	}
}

func TestPacketCRC(t *testing.T) {
	// Login with password "testme". The checksum was computed separately, with
	// Python's zlib.crc32 over FF 00 "testme", packed little-endian.
	got := encodePacket(typeLogin, []byte("testme"))
	want := []byte{'B', 'E', 0xA6, 0x21, 0xF1, 0x45, 0xFF, 0x00, 't', 'e', 's', 't', 'm', 'e'}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  % x\nwant % x", got, want)
	}
	kind, payload, err := decodePacket(got)
	if err != nil || kind != typeLogin || string(payload) != "testme" {
		t.Fatalf("round trip: %v %v %q", err, kind, payload)
	}

	corrupted := append([]byte(nil), got...)
	corrupted[len(corrupted)-1] ^= 0xFF
	if _, _, err := decodePacket(corrupted); err == nil {
		t.Fatal("corrupted packet accepted")
	}
	if _, _, err := decodePacket([]byte("BE")); err == nil {
		t.Fatal("short packet accepted")
	}
}

func TestLoginOK(t *testing.T) {
	s := newFakeServer(t, "secret")
	c, err := Dial("127.0.0.1", s.port(), "secret", 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Command("#shutdown"); err != nil {
		t.Fatal(err)
	}
	if got := <-s.commands; got != "#shutdown" {
		t.Fatalf("server received %q", got)
	}
}

func TestLoginRejected(t *testing.T) {
	s := newFakeServer(t, "secret")
	_, err := Dial("127.0.0.1", s.port(), "wrong", 2, testTimeout)
	if !errors.Is(err, ErrLoginRejected) {
		t.Fatalf("got %v", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	s := newFakeServer(t, "secret")
	s.silent.Store(true)
	start := time.Now()
	_, err := Dial("127.0.0.1", s.port(), "secret", 2, testTimeout)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("stop chain relies on failing fast, took %s", elapsed)
	}
}

func TestAckDuringCommand(t *testing.T) {
	s := newFakeServer(t, "secret")
	s.preAck.Store(true)
	c, err := Dial("127.0.0.1", s.port(), "secret", 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Command("players"); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-s.acks:
		if seq != 7 {
			t.Fatalf("acked message %d", seq)
		}
	case <-time.After(time.Second):
		t.Fatal("server message was never acknowledged")
	}
}

func TestCommandNoAck(t *testing.T) {
	s := newFakeServer(t, "secret")
	s.noAck.Store(true)
	c, err := Dial("127.0.0.1", s.port(), "secret", 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Command("#shutdown"); !errors.Is(err, ErrNoAck) {
		t.Fatalf("got %v", err)
	}
}

func TestPasswordNotInErrors(t *testing.T) {
	const password = "hunter2-super-secret"
	s := newFakeServer(t, "other")
	_, err := Dial("127.0.0.1", s.port(), password, 1, testTimeout)
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("rejected: %v", err)
	}
	s.silent.Store(true)
	_, err = Dial("127.0.0.1", s.port(), password, 1, testTimeout)
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("timeout: %v", err)
	}
	_, err = Dial("not a host name", 1, password, 1, testTimeout)
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("bad address: %v", err)
	}
}

func TestSanitizeMessage(t *testing.T) {
	if got := SayAll("restart\r\nin  5\tminutes "); got != "say -1 restart  in  5 minutes" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("é", maxSayLength+50)
	if got := SanitizeMessage(long); len([]rune(got)) != maxSayLength {
		t.Fatalf("length %d", len([]rune(got)))
	}
}
