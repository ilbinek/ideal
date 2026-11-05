// Package rcon is a minimal BattlEye RCon client: enough to log in, send a
// command and hear the acknowledgement. Sessions are short-lived, so it has no
// keepalive. Protocol: https://www.battleye.com/downloads/BERConProtocol.txt
package rcon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"strings"
	"time"
	"unicode"
)

const (
	typeLogin   byte = 0x00
	typeCommand byte = 0x01
	typeMessage byte = 0x02

	maxPacketSize = 4096
	maxSayLength  = 200
)

// ErrLoginRejected means the server answered and refused the password.
var ErrLoginRejected = errors.New("rcon login rejected")

// ErrNoAck means a command was sent but never acknowledged. The server may still
// have acted on it, which is the norm for a shutdown command.
var ErrNoAck = errors.New("rcon command not acknowledged")

// encodePacket builds 'B' 'E' crc32(LE) 0xFF type payload.
func encodePacket(kind byte, payload []byte) []byte {
	body := make([]byte, 0, 2+len(payload))
	body = append(body, 0xFF, kind)
	body = append(body, payload...)

	out := make([]byte, 0, 6+len(body))
	out = append(out, 'B', 'E')
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
	return append(out, body...)
}

// decodePacket validates a packet and returns its type and payload.
func decodePacket(data []byte) (kind byte, payload []byte, err error) {
	if len(data) < 8 || data[0] != 'B' || data[1] != 'E' || data[6] != 0xFF {
		return 0, nil, errors.New("malformed rcon packet")
	}
	if binary.LittleEndian.Uint32(data[2:6]) != crc32.ChecksumIEEE(data[6:]) {
		return 0, nil, errors.New("rcon packet checksum mismatch")
	}
	return data[7], data[8:], nil
}

// Client is one logged-in RCon session.
type Client struct {
	conn    *net.UDPConn
	seq     byte
	timeout time.Duration
}

// Dial logs in, making up to attempts tries that each wait up to timeout. Errors
// never contain the password.
func Dial(host string, port int, password string, attempts int, timeout time.Duration) (*Client, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, fmt.Errorf("rcon address: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("rcon connect: %w", err)
	}
	c := &Client{conn: conn, timeout: timeout}

	login := encodePacket(typeLogin, []byte(password))
	var lastErr error
	for i := 0; i < attempts; i++ {
		if _, err := conn.Write(login); err != nil {
			lastErr = fmt.Errorf("rcon login: %w", err)
			continue
		}
		accepted, err := c.awaitLogin()
		if err == nil {
			if accepted {
				return c, nil
			}
			_ = conn.Close()
			return nil, ErrLoginRejected
		}
		lastErr = err
	}
	_ = conn.Close()
	return nil, lastErr
}

func (c *Client) awaitLogin() (bool, error) {
	var buf [maxPacketSize]byte
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return false, err
	}
	for {
		n, err := c.conn.Read(buf[:])
		if err != nil {
			return false, fmt.Errorf("rcon login: %w", err)
		}
		kind, payload, err := decodePacket(buf[:n])
		if err != nil || kind != typeLogin || len(payload) < 1 {
			continue
		}
		return payload[0] == 0x01, nil
	}
}

// Command sends one command and waits for its acknowledgement, answering any
// server messages that arrive meanwhile. A timeout returns ErrNoAck.
func (c *Client) Command(command string) error {
	seq := c.seq
	c.seq++
	packet := encodePacket(typeCommand, append([]byte{seq}, command...))
	if _, err := c.conn.Write(packet); err != nil {
		return fmt.Errorf("rcon send: %w", err)
	}

	var buf [maxPacketSize]byte
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}
	for {
		n, err := c.conn.Read(buf[:])
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return ErrNoAck
			}
			return fmt.Errorf("rcon read: %w", err)
		}
		kind, payload, err := decodePacket(buf[:n])
		if err != nil || len(payload) < 1 {
			continue
		}
		switch kind {
		case typeMessage:
			// Unacknowledged messages get resent and eventually drop the session.
			_, _ = c.conn.Write(encodePacket(typeMessage, payload[:1]))
		case typeCommand:
			if payload[0] == seq {
				return nil
			}
		}
	}
}

// Close ends the session on our side. The protocol has no logout.
func (c *Client) Close() error {
	return c.conn.Close()
}

// SayAll builds the command that shows a message to all players.
func SayAll(message string) string {
	return "say -1 " + SanitizeMessage(message)
}

// SanitizeMessage keeps a message on one printable line of a safe length.
func SanitizeMessage(message string) string {
	var b strings.Builder
	count := 0
	for _, r := range message {
		if count >= maxSayLength {
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}
