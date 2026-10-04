package v2raygrpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type testGunService struct{}

func (testGunService) Context() context.Context { return context.Background() }
func (testGunService) Send(*Hunk) error         { return nil }
func (testGunService) Recv() (*Hunk, error)     { return &Hunk{Data: make([]byte, 1024)}, nil }

func TestGRPCConnConcurrentRead(t *testing.T) {
	conn := NewGRPCConn(testGunService{}, nil)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 64 {
				if _, err := conn.Read(make([]byte, 1)); err != nil {
					t.Error(err)
				}
			}
		})
	}
	group.Wait()
}

func TestClientResetDuringConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	defer server.Stop()
	go server.Serve(listener)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialing, proceed := make(chan struct{}), make(chan struct{})
	var dialOnce sync.Once
	client := &Client{ctx: ctx, serverAddr: listener.Addr().String(), dialOptions: []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		//nolint:staticcheck
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			dialOnce.Do(func() { close(dialing) })
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-proceed:
				return new(net.Dialer).DialContext(ctx, "tcp", addr)
			}
		}),
	}}
	defer client.Close()
	connected := make(chan *grpc.ClientConn, 1)
	go func() {
		conn, err := client.connect()
		if err != nil {
			t.Error(err)
		}
		connected <- conn
	}()
	<-dialing
	reset := make(chan struct{})
	go func() { client.Close(); close(reset) }()
	select {
	case <-reset:
		t.Error("reset returned before the admitted connection could be closed")
	case <-time.After(20 * time.Millisecond):
	}
	close(proceed)
	conn := <-connected
	<-reset
	if conn == nil {
		return
	}
	defer conn.Close()
	if conn.GetState() != connectivity.Shutdown || client.conn.Load() != nil {
		t.Fatal("reset left the concurrently created connection open")
	}
	// A reset must still allow the next connection to be created.
	client.dialOptions = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if _, err = client.connect(); err != nil {
		t.Fatal(err)
	}
}
