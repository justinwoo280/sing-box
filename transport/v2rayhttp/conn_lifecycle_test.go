package v2rayhttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
)

type countedBody struct{ closed atomic.Int32 }

func (*countedBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *countedBody) Close() error           { b.closed.Add(1); return nil }

func TestLateHTTPConnCloseBeforeSetup(t *testing.T) {
	conn := NewLateHTTPConn(io.Discard)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	body := new(countedBody)
	conn.Setup(body, nil)
	if body.closed.Load() != 1 {
		t.Fatal("response arriving after Close was not released")
	}
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Read after Close: %v", err)
	}
}

func TestLateHTTPConnCloseUnblocksRead(t *testing.T) {
	conn := NewLateHTTPConn(io.Discard)
	done := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); done <- err }()
	conn.Close()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Read after Close: %v", err)
		}
	case <-time.After(time.Second):
		conn.Setup(nil, net.ErrClosed)
		<-done
		t.Fatal("Close left Read waiting for response headers")
	}
}

func TestLateHTTPConnConcurrentSetupReadClose(t *testing.T) {
	for range 64 {
		conn := NewLateHTTPConn(io.Discard)
		body := new(countedBody)
		start := make(chan struct{})
		var group sync.WaitGroup
		group.Go(func() { <-start; conn.Setup(body, nil) })
		group.Go(func() { <-start; conn.Read(make([]byte, 1)) })
		group.Go(func() { <-start; conn.Close() })
		close(start)
		group.Wait()
		if body.closed.Load() != 1 {
			t.Fatal("concurrent Setup/Close leaked response body")
		}
	}
}

func TestHTTPClientConcurrentReset(t *testing.T) {
	client := &Client{transport: &http.Transport{}}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() { client.Close() })
	}
	group.Wait()
}

type testHTTPConn struct {
	net.Conn
	reader      io.Reader
	writeAccess sync.Mutex
	output      bytes.Buffer
}

func (c *testHTTPConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *testHTTPConn) Write(p []byte) (int, error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	return c.output.Write(p)
}
func (*testHTTPConn) Close() error { return nil }

func TestHTTP1ConnPreservesResponseCache(t *testing.T) {
	raw := &testHTTPConn{reader: strings.NewReader("HTTP/1.1 200 OK\r\n\r\npayload")}
	conn := NewHTTP1Conn(raw, &http.Request{Method: http.MethodGet, Header: make(http.Header)})
	defer conn.Close()
	first := make([]byte, 1)
	if _, err := conn.Read(first); err != nil {
		t.Fatal(err)
	}
	if conn.ReaderReplaceable() {
		t.Fatal("reader may be bypassed while response bytes remain cached")
	}
	rest, err := io.ReadAll(conn)
	if err != nil || string(append(first, rest...)) != "payload" {
		t.Fatalf("response: %q, %v", append(first, rest...), err)
	}
}

func TestHTTP1ConnConcurrentFirstWrite(t *testing.T) {
	raw := &testHTTPConn{}
	conn := NewHTTP1Conn(raw, &http.Request{Method: http.MethodPut, URL: &url.URL{Path: "/"}, Header: make(http.Header)})
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			conn.NeedHandshake()
			conn.WriterReplaceable()
			if _, err := conn.Write([]byte("x")); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if bytes.Count(raw.output.Bytes(), []byte("HTTP/1.1")) != 1 {
		t.Fatal("concurrent first writes sent multiple request headers")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTP2ConnCloseCancelsRoundTrip(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	client := &Client{http2: true, transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		close(started)
		<-request.Context().Done()
		close(canceled)
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := client.DialContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	conn.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the pending RoundTrip")
	}
}

func TestHTTP2WrapperCloseUnblocksWrite(t *testing.T) {
	raw, peer := net.Pipe()
	defer peer.Close()
	conn := NewHTTP2Wrapper(raw)
	done := make(chan struct{})
	go func() { conn.Write([]byte("payload")); close(done) }()
	// Leave the rest of the write blocked in the underlying connection.
	if _, err := peer.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { conn.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		peer.Close()
		<-closed
		t.Error("Close waited for the writer before closing its connection")
	}
	<-done
}

func TestHTTP2WrapperClosedBufferReleased(t *testing.T) {
	conn := NewHTTP2Wrapper(&testHTTPConn{})
	conn.CloseWrapper()
	buffer := buf.New()
	buffer.WriteString("payload")
	if err := conn.WriteBuffer(buffer); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	if buffer.Cap() != 0 {
		buffer.Release()
		t.Fatal("closed wrapper retained a write buffer")
	}
}

func TestLateHTTPConnSetupErrorUnblocksWrite(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	conn := NewLateHTTPConn(writer)
	done := make(chan error, 1)
	go func() { _, err := conn.Write([]byte("payload")); done <- err }()
	conn.Setup(nil, net.ErrClosed)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("write succeeded after a failed handshake")
		}
	case <-time.After(time.Second):
		conn.Close()
		<-done
		t.Fatal("failed handshake left its upload writer blocked")
	}
}
