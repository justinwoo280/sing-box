package vless

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

type trackedTransportConn struct {
	net.Conn
	writeErr error
	closed   atomic.Bool
}

func (c *trackedTransportConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}

func (c *trackedTransportConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

type fixedClientTransport struct{ conn net.Conn }

func (t fixedClientTransport) DialContext(context.Context) (net.Conn, error) { return t.conn, nil }
func (fixedClientTransport) Close() error                                    { return nil }

func TestOutboundTransportOwnership(t *testing.T) {
	writeErr := errors.New("write failed")
	for _, tc := range []struct {
		name         string
		network      string
		flow         string
		xudp         bool
		packetAddr   bool
		listenPacket bool
		writeErr     error
		wantError    string
	}{
		{name: "xudp-write-dial", network: "udp", xudp: true, writeErr: writeErr, wantError: "write failed"},
		{name: "xudp-write-listen", listenPacket: true, xudp: true, writeErr: writeErr, wantError: "write failed"},
		{name: "vision-tcp", network: "tcp", flow: vless.FlowVision, wantError: "initialize vision"},
		{name: "vision-xudp-dial", network: "udp", flow: vless.FlowVision, xudp: true, wantError: "initialize vision"},
		{name: "vision-xudp-listen", listenPacket: true, flow: vless.FlowVision, xudp: true, wantError: "initialize vision"},
		{name: "packetaddr-domain-dial", network: "udp", packetAddr: true, wantError: "domain destination is not supported"},
		{name: "packetaddr-domain-listen", listenPacket: true, packetAddr: true, wantError: "domain destination is not supported"},
		{name: "unknown-network", network: "invalid", wantError: "unknown network"},
		{name: "tcp-success", network: "tcp"},
		{name: "udp-success", network: "udp"},
		{name: "udp-listen-success", listenPacket: true},
		{name: "xudp-dial-success", network: "udp", xudp: true},
		{name: "xudp-listen-success", listenPacket: true, xudp: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := vless.NewClient("", tc.flow, logger.NOP())
			if err != nil {
				t.Fatal(err)
			}
			raw, peer := net.Pipe()
			defer raw.Close()
			defer peer.Close()
			tracked := &trackedTransportConn{Conn: raw, writeErr: tc.writeErr}
			h := &Outbound{
				Adapter: outbound.NewAdapter("vless", "test", nil, nil),
				logger:  logger.NOP(), client: client, xudp: tc.xudp, packetAddr: tc.packetAddr,
				transport: fixedClientTransport{tracked},
			}
			destination := M.Socksaddr{Fqdn: "example.com", Port: 443}
			var conn io.Closer
			if tc.listenPacket {
				conn, err = h.ListenPacket(context.Background(), destination)
			} else {
				conn, err = h.DialContext(context.Background(), tc.network, destination)
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q error, got %v", tc.wantError, err)
				}
				if tc.writeErr != nil && !errors.Is(err, tc.writeErr) {
					t.Fatalf("write error was not preserved: %v", err)
				}
				if !tracked.closed.Load() {
					t.Fatal("underlying transport connection left open after failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tracked.closed.Load() {
				t.Fatal("underlying transport connection closed before caller used it")
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			if !tracked.closed.Load() {
				t.Fatal("closing the returned connection did not close its transport")
			}
		})
	}
}
