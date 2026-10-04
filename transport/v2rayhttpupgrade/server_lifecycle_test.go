package v2rayhttpupgrade

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type hijackWriter struct {
	*httptest.ResponseRecorder
	conn   net.Conn
	buffer *bufio.ReadWriter
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, w.buffer, nil
}

type bufferedHandler struct{ t *testing.T }

func (h bufferedHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	defer conn.Close()
	payload := make([]byte, 7)
	if _, err := io.ReadFull(conn, payload); err != nil || string(payload) != "payload" {
		h.t.Errorf("hijacked payload: %q, %v", payload, err)
	}
}

func TestHTTPUpgradeServerPreservesBufferedRequest(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	peer.Close()
	reader := bufio.NewReader(strings.NewReader("payload"))
	if _, err := reader.Peek(7); err != nil {
		t.Fatal(err)
	}
	writer := hijackWriter{httptest.NewRecorder(), raw, bufio.NewReadWriter(reader, bufio.NewWriter(io.Discard))}
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	server := &Server{path: "/", handler: bufferedHandler{t}}
	server.ServeHTTP(writer, request)
}
