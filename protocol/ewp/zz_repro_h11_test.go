package ewp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Live evidence for the stream-up / HTTP/1.1 deadlock. The xhttp
// stream-up transport is a single long-lived streaming POST. On HTTP/1.1
// Go's net/http server cannot emit response headers while the request
// body is still open: it drains up to maxPostHandlerReadBytes (256KB+1)
// of the request body first (Go server.go:1462). An EWP handshake is a
// synchronous ClientHello -> ServerHello round trip; the client sends a
// ~1.2KB ClientHello and then waits for the ServerHello, so it never
// delivers 256KB. The server therefore can never flush the ServerHello
// => deadlock. These tests pin the exact mechanisms against a real
// httptest server (HTTP/1.1).

// TestRepro_H11_SmallRequestBodyIsDelivered checks the client side: a
// small write to a streaming (chunked) H1.1 request body IS delivered
// to the server promptly on Go 1.24 — i.e. the deadlock is NOT caused
// by client-side request-body buffering (the original hypothesis).
func TestRepro_H11_SmallRequestBodyIsDelivered(t *testing.T) {
	gotByte := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 16)
		if n, _ := r.Body.Read(buf); n > 0 {
			select {
			case gotByte <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }() // unblock the server body on early exit

	req, err := http.NewRequest(http.MethodPost, srv.URL, pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	type result struct {
		resp *http.Response
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		resCh <- result{resp, err}
	}()

	hello := make([]byte, 1200) // ~EWP ClientHello size
	if _, err := pw.Write(hello); err != nil {
		t.Fatalf("pw.Write: %v", err)
	}
	select {
	case <-gotByte:
		t.Logf("server received the ~1.2KB write promptly — client did NOT buffer it (client-side buffering hypothesis refuted)")
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("server did NOT receive the ~1.2KB write within 500ms — client buffered it (unexpected on Go 1.24)")
	}

	_ = pw.Close()
	if r := <-resCh; r.resp != nil {
		_ = r.resp.Body.Close()
	}
}

// TestRepro_H11_ServerFlushBlockedByRequestBody shows the actual
// deadlock mechanism: when a handler flushes response headers while the
// request body is still open, Go's net/http server drains up to
// maxPostHandlerReadBytes (256KB+1) of the request body first. On a
// stream-up POST the client never sends that much (it is blocked
// waiting for the ServerHello), so Flush never returns and the
// ServerHello never goes out.
func TestRepro_H11_ServerFlushBlockedByRequestBody(t *testing.T) {
	flushReturned := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		if flusher != nil {
			flusher.Flush() // blocks draining <=256KB+1 of r.Body on H1.1
		}
		select {
		case flushReturned <- struct{}{}:
		default:
		}
	}))
	defer srv.Close()

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	req, err := http.NewRequest(http.MethodPost, srv.URL, pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	type result struct {
		resp *http.Response
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		resCh <- result{resp, err}
	}()

	// ClientHello-sized body, then the client waits for the ServerHello.
	hello := make([]byte, 1200)
	if _, err := pw.Write(hello); err != nil {
		t.Fatalf("pw.Write hello: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	select {
	case <-flushReturned:
		t.Fatalf("Flush returned with only ~1.2KB of body — server did NOT block on body drain (unexpected)")
	default:
		t.Logf("server Flush() has NOT returned after 400ms with only ~1.2KB of body — blocked draining the open request body (deadlock mechanism confirmed)")
	}

	// Push >256KB so the drain completes and Flush can return.
	big := make([]byte, 300*1024)
	go func() { _, _ = pw.Write(big) }()
	select {
	case <-flushReturned:
		t.Logf("Flush returned after >256KB of request body was drained — confirms the 256KB drain gate")
	case <-time.After(3 * time.Second):
		t.Fatalf("Flush never returned even after 300KB")
	}

	_ = pw.Close()
	if r := <-resCh; r.resp != nil {
		_ = r.resp.Body.Close()
	}
}
