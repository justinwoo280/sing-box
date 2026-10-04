package v2raywebsocket

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/ws"
)

func TestUpgradePreservesSharedHeaders(t *testing.T) {
	client := &Client{}
	headers := make(http.Header)
	headers.Set("Sec-WebSocket-Protocol", "test-protocol")
	for range 2 {
		conn, peer := net.Pipe()
		peer.Close()
		_, err := client.upgrade(context.Background(), conn, &url.URL{Scheme: "ws", Host: "localhost", Path: "/"}, headers)
		if err == nil {
			t.Fatal("handshake on a closed connection succeeded")
		}
		if headers.Get("Sec-WebSocket-Protocol") != "test-protocol" {
			t.Fatal("handshake mutated headers shared by subsequent dials")
		}
	}
}

type pipeDialer struct{ net.Conn }

func (d pipeDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return d.Conn, nil
}
func (pipeDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

func TestWebsocketCancelHandshake(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	client := &Client{dialer: pipeDialer{raw}, requestURL: url.URL{Scheme: "ws", Host: "localhost", Path: "/"}}
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

func TestEarlyWebsocketSurvivesDialContextCancel(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	client := &Client{dialer: pipeDialer{raw}, requestURL: url.URL{Scheme: "ws", Host: "localhost", Path: "/"}, maxEarlyData: 32}
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := client.DialContext(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	serverDone := make(chan error, 1)
	go func() {
		_, err := ws.Upgrade(peer)
		serverDone <- err
	}()
	if _, err = conn.Write([]byte("payload")); err != nil {
		t.Fatalf("completed dial's canceled context interrupted lazy handshake: %v", err)
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestWebsocketPreservesBufferedResponse(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	serverDone := make(chan error, 1)
	go func() {
		var response bytes.Buffer
		_, err := ws.Upgrade(struct {
			io.Reader
			io.Writer
		}{peer, &response})
		if err == nil {
			err = ws.WriteFrame(&response, ws.NewBinaryFrame([]byte("payload")))
		}
		if err == nil {
			_, err = peer.Write(response.Bytes())
		}
		serverDone <- err
	}()
	client := &Client{dialer: pipeDialer{raw}, requestURL: url.URL{Scheme: "ws", Host: "localhost", Path: "/"}}
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw.SetReadDeadline(time.Now().Add(time.Second))
	data := make([]byte, 7)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "payload" {
		t.Fatalf("buffered frame: %q, %v", data, err)
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}

type yieldingConn struct {
	net.Conn
	access sync.Mutex
	output bytes.Buffer
}

func (c *yieldingConn) Write(p []byte) (int, error) {
	c.access.Lock()
	n, err := c.output.Write(p)
	c.access.Unlock()
	runtime.Gosched()
	return n, err
}

func TestWebsocketConcurrentFrames(t *testing.T) {
	raw := new(yieldingConn)
	conn := NewConn(raw, nil, ws.StateServerSide)
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			if _, err := conn.Write(bytes.Repeat([]byte("x"), 128)); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	for range 32 {
		frame, err := ws.ReadFrame(&raw.output)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Header.OpCode != ws.OpBinary || !bytes.Equal(frame.Payload, bytes.Repeat([]byte("x"), 128)) {
			t.Fatal("concurrent writes interleaved frame headers and payloads")
		}
	}
}

func TestWebsocketCloseUnblocksWrite(t *testing.T) {
	raw, peer := net.Pipe()
	defer peer.Close()
	conn := NewConn(raw, nil, ws.StateServerSide)
	written := make(chan error, 1)
	go func() { _, err := conn.Write([]byte("payload")); written <- err }()
	// Receiving the header establishes that the writer is blocked in its payload.
	if _, err := ws.ReadHeader(peer); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { conn.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		peer.Close()
		<-closed
		t.Error("Close waited for a blocked frame write")
	}
	if err := <-written; err == nil || err == io.EOF {
		t.Fatalf("blocked write: %v", err)
	}
}
