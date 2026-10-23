package core

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestParseDCTarget(t *testing.T) {
	for _, test := range []struct {
		input string
		all   bool
		index int
	}{
		{input: "dc", all: true},
		{input: "all", all: true},
		{input: "dc1", index: 0},
		{input: "dc5", index: 4},
	} {
		got, ok := parseDCTarget(test.input)
		if !ok || got.all != test.all || got.index != test.index {
			t.Fatalf("parseDCTarget(%q) = %#v, %v", test.input, got, ok)
		}
	}
	for _, input := range []string{"dc0", "dc6", "telegram"} {
		if _, ok := parseDCTarget(input); ok {
			t.Fatalf("parseDCTarget(%q) unexpectedly matched", input)
		}
	}
}

func TestProbeDCUsesPort80When443Fails(t *testing.T) {
	var addresses []string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		addresses = append(addresses, network+" "+address)
		if address[len(address)-4:] == ":443" {
			return nil, errors.New("blocked")
		}
		return fakeConn{}, nil
	}
	result := probeDCWith(context.Background(), dcTargets[0], dial, func(context.Context, string) (int64, error) {
		return 0, errors.New("must not use fallback")
	})
	if result.err != nil || result.port != 80 {
		t.Fatalf("probe result = %#v", result)
	}
	if len(addresses) != 2 || addresses[1] != "tcp "+dcTargets[0]+":80" {
		t.Fatalf("dial addresses = %#v", addresses)
	}
}

type fakeConn struct{}

func (fakeConn) Read([]byte) (int, error)           { return 0, errors.New("eof") }
func (fakeConn) Write([]byte) (int, error)          { return 0, errors.New("closed") }
func (fakeConn) Close() error                       { return nil }
func (fakeConn) LocalAddr() net.Addr                { return fakeAddr(0) }
func (fakeConn) RemoteAddr() net.Addr               { return fakeAddr(0) }
func (fakeConn) SetDeadline(_ time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(_ time.Time) error { return nil }

type fakeAddr int

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "fake" }
