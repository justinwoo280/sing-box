package trojan

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type fragmentedConn struct {
	net.Conn
	reader io.Reader
	writer io.Writer
}

func (c fragmentedConn) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return c.reader.Read(p)
}
func (c fragmentedConn) Write(p []byte) (int, error) { return c.writer.Write(p) }

type serviceTestHandler struct {
	Handler
	t      *testing.T
	called bool
}

func (h *serviceTestHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	h.called = true
	payload, err := io.ReadAll(conn)
	if err != nil || string(payload) != "payload" {
		h.t.Errorf("payload: %q, %v", payload, err)
	}
}

func TestServiceFragmentedAuthentication(t *testing.T) {
	var wire bytes.Buffer
	if err := ClientHandshake(fragmentedConn{writer: &wire}, Key("test"), M.ParseSocksaddr("example.com:443"), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	handler := &serviceTestHandler{t: t}
	service := NewService[string](handler, nil, nil)
	if err := service.UpdateUsers([]string{"test"}, []string{"test"}); err != nil {
		t.Fatal(err)
	}
	if err := service.NewConnection(context.Background(), fragmentedConn{reader: &wire}, M.Socksaddr{}, nil); err != nil {
		t.Fatal(err)
	}
	if !handler.called {
		t.Fatal("fragmented key did not reach the connection handler")
	}
}

func TestServiceShortRequestFallback(t *testing.T) {
	handler := &serviceTestHandler{t: t}
	service := NewService[string](handler, handler, nil)
	if err := service.UpdateUsers([]string{"test"}, []string{"test"}); err != nil {
		t.Fatal(err)
	}
	if err := service.NewConnection(context.Background(), fragmentedConn{reader: bytes.NewBufferString("payload")}, M.Socksaddr{}, nil); err != nil {
		t.Fatal(err)
	}
	if !handler.called {
		t.Fatal("short non-protocol request was not passed to fallback")
	}
}
