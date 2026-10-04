package v2raywebsocket

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http/httptest"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/ws"
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
		h.t.Errorf("hijacked frame: %q, %v", payload, err)
	}
}

func TestWebsocketServerPreservesBufferedRequest(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	peer.Close()
	var wire bytes.Buffer
	if err := ws.WriteFrame(&wire, ws.MaskFrameInPlace(ws.NewBinaryFrame([]byte("payload")))); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&wire)
	if _, err := reader.Peek(wire.Len()); err != nil {
		t.Fatal(err)
	}
	writer := hijackWriter{httptest.NewRecorder(), raw, bufio.NewReadWriter(reader, bufio.NewWriter(io.Discard))}
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	server := &Server{path: "/", handler: bufferedHandler{t}}
	server.ServeHTTP(writer, request)
}
