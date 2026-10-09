package ewp

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

func TestEWPUDPAdapter(t *testing.T) {
	for _, address := range []string{"203.0.113.1:443", "[2001:db8::1]:443", "destination.test:443"} {
		t.Run(address, func(t *testing.T) {
			privateKey, publicKey, err := sewp.GenerateSigningIdentity()
			if err != nil {
				t.Fatal(err)
			}
			const uuid = "11111111-2222-3333-4444-555555555555"
			router := &ewpTestRouter{done: make(chan struct{})}
			inbound := &Inbound{router: router, logger: logger.NOP(), users: []option.EWPUser{{Name: "alice", UUID: uuid}}}
			inbound.service, err = sewp.NewServiceV23(&inboundHandler{owner: inbound}, privateKey, "udp-test", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer inbound.service.Close()
			if err := inbound.service.AddUser(uuid); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			defer serverConn.Close()
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				inbound.NewConnection(ctx, serverConn, adapter.InboundContext{}, nil)
			}()
			client, err := sewp.NewClientV23(uuid, "udp-test", publicKey, 0)
			if err != nil {
				t.Fatal(err)
			}
			destination := M.ParseSocksaddr(address)
			packets, err := client.DialPacketConn(ctx, clientConn, socksaddrToEWP(destination))
			if err != nil {
				t.Fatal(err)
			}
			conn := bufio.NewBindPacketConn(newEWPPacketConn(packets), destination)
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			go func() {
				select {
				case <-router.done:
				case <-ctx.Done():
					return
				}
				router.mu.Lock()
				packetConn := router.packetConn
				router.mu.Unlock()
				_, _ = bufio.CopyPacket(packetConn, packetConn)
				_ = packetConn.Close()
			}()
			for index := 0; index < 3; index++ {
				payload := bytes.Repeat([]byte{byte(index + 1)}, 64+index)
				if _, err := conn.Write(payload); err != nil {
					t.Fatalf("datagram %d: %v", index, err)
				}
				got := make([]byte, len(payload)+1)
				n, err := conn.Read(got)
				if err != nil || !bytes.Equal(payload, got[:n]) {
					t.Fatalf("datagram %d: echo %x, error %v", index, got, err)
				}
			}
			router.mu.Lock()
			metadata := router.md
			router.mu.Unlock()
			if metadata.User != "alice" || metadata.Destination != destination {
				t.Fatalf("incorrect UDP metadata: %+v", metadata)
			}
			_ = conn.Close()
			select {
			case <-serverDone:
			case <-ctx.Done():
				t.Fatal("UDP session did not terminate")
			}
		})
	}
}
