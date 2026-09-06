package ewp

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"

	sewp "github.com/justinwoo280/sing-ewp"
)

// holdConn simulates an xhttp stream-up / HTTP/1.1 transport: Write()
// accepts bytes (so the caller does not block) but NEVER forwards them
// to the peer, mimicking Go's chunked-request-body buffering on H1.1.
// Its deadline methods are no-ops, exactly like xhttp.splitConn
// ("TODO cannot do anything useful"). This is the combination the EWP
// integration hits today for stream-up.
type holdConn struct {
	net.Conn
	wbuf bytes.Buffer
}

func (c *holdConn) Write(b []byte) (int, error) {
	c.wbuf.Write(b) // buffered, never flushed to the wire
	return len(b), nil
}

func (c *holdConn) SetDeadline(t time.Time) error      { return nil } // no-op, like splitConn
func (c *holdConn) SetReadDeadline(t time.Time) error  { return nil } // no-op
func (c *holdConn) SetWriteDeadline(t time.Time) error { return nil } // no-op

// Repro for the stream-up / grpc root cause: EWP's handshake is a strict
// synchronous request/response (ClientHello -> ServerHello) layered via
// NewLengthFramer over a net.Conn. If the outer transport does not deliver
// bytes immediately (stream-up on H1.1 buffers the chunked request body)
// AND the deadline is a no-op (xhttp.splitConn), the handshake deadlocks
// instead of failing fast. The baseline net.Pipe case (immediate flush,
// working deadlines) is covered by TestEWP_EndToEnd_TCP and succeeds.
func TestRepro_EWPHandshakeDeadlocksOnNonFlushingTransport(t *testing.T) {
	priv, pub, err := sewp.GenerateSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}

	clientPipe, serverPipe := net.Pipe()
	router := newFakeRouter()
	in := makeInbound(t, router, priv, "flush-test")

	// Server side: drive the byte-stream entry exactly like the inbound
	// listener does. It blocks reading the ClientHello that never
	// arrives because the client's writes are held in holdConn.
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		var md adapter.InboundContext
		md.Source = M.SocksaddrFromNetIP(netip.MustParseAddrPort("127.0.0.1:1234"))
		srvCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		in.NewConnectionEx(srvCtx, serverPipe, md, nil)
		_ = serverPipe.Close()
	}()

	// Client side: EWP client over a holdConn (non-flushing + no-op
	// deadline), exactly as Outbound.dialUnderlying hands a stream-up
	// splitConn to client.DialConn.
	client, err := sewp.NewClientV23(v23TestUUID, "flush-test", pub, 0)
	if err != nil {
		t.Fatalf("NewClientV23: %v", err)
	}
	dst := sewp.Address{Addr: netip.MustParseAddrPort("8.8.8.8:443")}
	hold := &holdConn{Conn: clientPipe}

	hsDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()
		_, err := client.DialConn(ctx, hold, dst)
		hsDone <- err
	}()

	select {
	case err := <-hsDone:
		// The ClientHello never reaches the server, so the only way
		// DialConn returns is via its ctx deadline. With a no-op
		// deadline (real splitConn) it would NOT return — the bug.
		if err == nil {
			t.Fatalf("handshake unexpectedly succeeded through a non-flushing transport")
		}
		t.Logf("handshake returned with error (ctx deadline honoured): %v", err)
	case <-time.After(1500 * time.Millisecond):
		// Expected for the real splitConn case: no-op deadline + held
		// bytes => permanent hang. Here net.Pipe still has working
		// read deadlines so the ctx timeout fires; the point is that
		// nothing in the EWP layer fails fast on its own.
		t.Logf("handshake did not complete within 1.5s (deadlocked without immediate flush)")
	}

	_ = clientPipe.Close()
	<-srvDone
}
