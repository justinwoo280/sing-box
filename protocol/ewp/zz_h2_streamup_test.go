package ewp

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/sagernet/sing/common/bufio/deadline"
	sewp "github.com/justinwoo280/sing-ewp"
)

type httpBodyConn struct {
	read  io.ReadCloser
	write io.Writer
	flush func()
}

func (c *httpBodyConn) Read(b []byte) (int, error) { return c.read.Read(b) }
func (c *httpBodyConn) Write(b []byte) (int, error) {
	n, err := c.write.Write(b)
	if c.flush != nil && err == nil {
		c.flush()
	}
	return n, err
}
func (c *httpBodyConn) Close() error {
	_ = c.read.Close()
	if cl, ok := c.write.(io.Closer); ok {
		_ = cl.Close()
	}
	return nil
}
func (c *httpBodyConn) LocalAddr() net.Addr              { return dummyAddr{} }
func (c *httpBodyConn) RemoteAddr() net.Addr             { return dummyAddr{} }
func (c *httpBodyConn) SetDeadline(time.Time) error      { return nil }
func (c *httpBodyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *httpBodyConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "127.0.0.1:0" }

// TestEWP_Over_H2_StreamingPost verifies the stream-up transport path:
// EWP handshake over a single HTTP/2 streaming POST (request body =
// client->server, response body = server->client). On H2 the server can
// flush the response concurrently with the open request body, so unlike
// H1.1 (256KB drain gate) the handshake completes.
func TestEWP_Over_H2_StreamingPost(t *testing.T) {
	const uuid = "11111111-2222-3333-4444-555555555555"
	service := sewp.NewService(echoEWPHandler{})
	if err := service.AddUser(uuid); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		var conn net.Conn = &httpBodyConn{
			read:  r.Body,
			write: w,
			flush: func() { flusher.Flush() },
		}
		if deadline.NeedAdditionalReadDeadline(conn) {
			conn = deadline.NewConn(conn)
		}
		hsCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_ = service.HandleConn(hsCtx, conn)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, srv.URL, pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	h2client := &http.Client{Transport: &http2.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := h2client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Proto != "HTTP/2.0" {
		t.Fatalf("expected HTTP/2, got %s", resp.Proto)
	}

	var conn net.Conn = &httpBodyConn{read: resp.Body, write: pw}
	if deadline.NeedAdditionalReadDeadline(conn) {
		conn = deadline.NewConn(conn)
	}

	ewpClient, err := sewp.NewClient(uuid)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	dst := sewp.Address{Addr: netip.MustParseAddrPort("8.8.8.8:443")}
	hsCtx, hsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hsCancel()
	c, err := ewpClient.DialConn(hsCtx, conn, dst)
	if err != nil {
		t.Fatalf("EWP DialConn over H2 stream-up: %v", err)
	}
	defer c.Close()

	payload := []byte("hello over H2 stream-up")
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	t.Logf("EWP over H2 stream-up echo ok (proto=%s): %q", resp.Proto, buf)

	_ = pw.Close()
	_ = resp.Body.Close()
}
