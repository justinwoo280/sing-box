package v2rayhttpupgrade

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

type pipeDialer struct{ net.Conn }

func (d pipeDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return d.Conn, nil
}
func (pipeDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

func TestHTTPUpgradePreservesBufferedResponse(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	client := &Client{dialer: pipeDialer{raw}, requestURL: url.URL{Scheme: "http", Host: "localhost", Path: "/"}, headers: make(http.Header)}
	serverDone := make(chan error, 1)
	go func() {
		_, err := http.ReadRequest(bufio.NewReader(peer))
		if err == nil {
			_, err = io.WriteString(peer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\npayload")
		}
		serverDone <- err
	}()
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	data := make([]byte, 7)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "payload" {
		t.Fatalf("buffered response: %q, %v", data, err)
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestHTTPUpgradeCancelHandshake(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	client := &Client{dialer: pipeDialer{raw}, requestURL: url.URL{Scheme: "http", Host: "localhost", Path: "/"}, headers: make(http.Header)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.DialContext(ctx); done <- err }()
	if _, err := http.ReadRequest(bufio.NewReader(peer)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handshake cancellation: %v", err)
		}
	case <-time.After(time.Second):
		peer.Close()
		<-done
		t.Fatal("canceled handshake kept waiting for response headers")
	}
}
