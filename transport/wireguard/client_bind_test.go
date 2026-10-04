package wireguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/wireguard-go/conn"
)

type bindTestDialer struct{}

func (bindTestDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, net.ErrClosed
}

type blockedBindDialer struct{ started chan struct{} }

func (d blockedBindDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	close(d.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockedBindDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

func TestClientBindCloseCancelsConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := blockedBindDialer{started: make(chan struct{})}
	bind := NewClientBind(ctx, nil, dialer, true, netip.MustParseAddrPort("127.0.0.1:1234"), [3]byte{})
	bind.Open(0)
	done := make(chan error, 1)
	go func() { _, err := bind.connect(); done <- err }()
	<-dialer.started
	closed := make(chan struct{})
	go func() { bind.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		cancel()
		<-closed
		<-done
		t.Fatal("Close could not cancel an admitted dial")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestClientBindConcurrentClose(t *testing.T) {
	bind := NewClientBind(context.Background(), nil, bindTestDialer{}, false, netip.AddrPort{}, [3]byte{})
	for range 32 {
		bind.Open(0)
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() { bind.Close() })
		}
		group.Wait()
	}
}

func TestClientBindReceiveDoesNotCrossReopen(t *testing.T) {
	bind := NewClientBind(context.Background(), nil, bindTestDialer{}, false, netip.AddrPort{}, [3]byte{})
	receive, _, err := bind.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	bind.Close()
	if _, _, err = bind.Open(0); err != nil {
		t.Fatal(err)
	}
	defer bind.Close()
	done := make(chan error, 1)
	go func() {
		_, err := receive[0]([][]byte{make([]byte, 64)}, make([]int, 1), make([]conn.Endpoint, 1))
		done <- err
	}()
	select {
	case err = <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("old receiver: %v", err)
		}
	case <-time.After(time.Second):
		bind.Close()
		<-done
		t.Fatal("old receive function started using the reopened bind")
	}
}

func (bindTestDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return net.ListenPacket("udp", "127.0.0.1:0")
}

func TestClientBindConcurrentConnect(t *testing.T) {
	bind := NewClientBind(context.Background(), nil, bindTestDialer{}, false, netip.AddrPort{}, [3]byte{})
	if _, _, err := bind.Open(0); err != nil {
		t.Fatal(err)
	}
	defer bind.Close()
	for range 16 {
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() {
				conn, err := bind.connect()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
			})
		}
		group.Wait()
	}
}
