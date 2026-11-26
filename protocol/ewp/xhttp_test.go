package ewp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/stretchr/testify/require"
)

const (
	ewpXHTTPUser     = "11111111-2222-3333-4444-555555555555"
	ewpXHTTPServerID = "ewp-ordinary-xhttp-loopback"
	ewpXHTTPHost     = "ewp.test"
)

func TestEWPOrdinaryXHTTP(t *testing.T) {
	for _, test := range []struct {
		alpn, mode string
		framed     bool
	}{
		{"", "packet-up", false},
		{"http/1.1", "packet-up", false},
		{"h2", "packet-up", false},
		{"h2", "stream-up", false},
		{"h2", "stream-one", false},
		{"h2", "stream-up", true},
		{"h2", "stream-one", true},
		{"h3", "packet-up", false},
		{"h3", "stream-up", false},
		{"h3", "stream-one", false},
	} {
		protocol := test.alpn
		if protocol == "" {
			protocol = "cleartext"
		}
		name := protocol + "/" + test.mode
		if test.framed {
			name += "/grpc"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var serverTLS, clientTLS string
			if test.alpn != "" {
				cert, key := ewpXHTTPTestCertificate(t)
				serverTLS = fmt.Sprintf(`,"tls":{"enabled":true,"alpn":[%q],"certificate":%q,"key":%q}`, test.alpn, cert, key)
				clientTLS = fmt.Sprintf(`,"tls":{"enabled":true,"alpn":[%q],"server_name":%q,"certificate":%q}`, test.alpn, ewpXHTTPHost, cert)
			}
			transport := fmt.Sprintf(`{"type":"xhttp","host":%q,"path":"/ewp","mode":%q,"grpc_framing":%t}`, ewpXHTTPHost, test.mode, test.framed)
			address, publicKey, router := startEWPXHTTPTestInbound(t, ctx, transport, serverTLS, false)
			client := newEWPXHTTPTestOutbound(t, ctx, address, publicKey, transport, clientTLS, false)
			checkEWPXHTTPFlows(t, ctx, client, router)
		})
	}
}

type ewpXHTTPTestRouter struct {
	adapter.Router
	metadata chan adapter.InboundContext
}

func (r *ewpXHTTPTestRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, _ N.CloseHandlerFunc) {
	select {
	case r.metadata <- metadata:
		go func() {
			defer conn.Close()
			_, _ = io.Copy(conn, conn)
		}()
	case <-ctx.Done():
		conn.Close()
	}
}

func (r *ewpXHTTPTestRouter) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, _ N.CloseHandlerFunc) {
	select {
	case r.metadata <- metadata:
		go func() {
			defer conn.Close()
			_, _ = bufio.CopyPacket(conn, conn)
		}()
	case <-ctx.Done():
		conn.Close()
	}
}

func startEWPXHTTPTestInbound(t *testing.T, ctx context.Context, transport, tls string, early bool) (M.Socksaddr, string, *ewpXHTTPTestRouter) {
	t.Helper()
	privateKey, publicKey, err := sewp.GenerateSigningIdentity()
	require.NoError(t, err)
	var options option.EWPInboundOptions
	data := fmt.Sprintf(`{
		"listen":"127.0.0.1","listen_port":0,"server_id":%q,"signing_private_key":%q,"route_epoch":7,
		"users":[{"name":"alice","uuid":%q}],"early_server_finished":%t,"transport":%s%s
	}`, ewpXHTTPServerID, privateKey, ewpXHTTPUser, early, transport, tls)
	require.NoError(t, json.UnmarshalDisallowUnknownFields([]byte(data), &options))
	router := &ewpXHTTPTestRouter{metadata: make(chan adapter.InboundContext, 4)}
	inbound, err := NewInbound(ctx, router, logger.NOP(), "ewp-in", options)
	require.NoError(t, err)
	server := inbound.(*Inbound)
	scope := adapter.NewScope(ctx, logger.NOP())
	t.Cleanup(func() { _ = scope.Close() })
	require.NoError(t, server.Start(adapter.StartStateInitialize, scope))
	require.NoError(t, server.Start(adapter.StartStateStart, scope))
	if listener := server.listener.TCPListener(); listener != nil {
		return M.SocksaddrFromNet(listener.Addr()), publicKey, router
	}
	return M.SocksaddrFromNet(server.listener.UDPConn().LocalAddr()), publicKey, router
}

func newEWPXHTTPTestOutbound(t *testing.T, ctx context.Context, address M.Socksaddr, publicKey, transport, tls string, early bool) *Outbound {
	t.Helper()
	var options option.EWPOutboundOptions
	data := fmt.Sprintf(`{
		"server":%q,"server_port":%d,"server_id":%q,"server_public_key":%q,"route_epoch":7,
		"uuid":%q,"early_server_finished":%t,"transport":%s%s
	}`, address.AddrString(), address.Port, ewpXHTTPServerID, publicKey, ewpXHTTPUser, early, transport, tls)
	require.NoError(t, json.UnmarshalDisallowUnknownFields([]byte(data), &options))
	outbound, err := NewOutbound(ctx, nil, logger.NOP(), "ewp-out", options)
	require.NoError(t, err)
	client := outbound.(*Outbound)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func checkEWPXHTTPFlows(t *testing.T, ctx context.Context, client *Outbound, router *ewpXHTTPTestRouter) {
	t.Helper()
	destination := M.ParseSocksaddr("echo.test:443")
	checkRoute := func() {
		t.Helper()
		select {
		case metadata := <-router.metadata:
			require.Equal(t, "alice", metadata.User)
			require.Equal(t, destination, metadata.Destination)
		case <-ctx.Done():
			t.Fatal("EWP did not route the authenticated flow:", ctx.Err())
		}
	}
	for _, network := range []string{N.NetworkTCP, N.NetworkUDP} {
		func() {
			conn, err := client.DialContext(ctx, network, destination)
			require.NoError(t, err, "EWP %s dial", network)
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
			payload := []byte("authenticated EWP over ordinary XHTTP")
			if network == N.NetworkTCP {
				payload = bytes.Repeat(payload, 2048)
			}
			written := make(chan error, 1)
			go func() {
				n, err := conn.Write(payload)
				if err == nil && n != len(payload) {
					err = io.ErrShortWrite
				}
				written <- err
				if err != nil {
					_ = conn.Close()
				}
			}()
			got := make([]byte, len(payload)+1)
			var n int
			if network == N.NetworkTCP {
				n, err = io.ReadFull(conn, got[:len(payload)])
			} else {
				n, err = conn.Read(got)
			}
			require.NoError(t, err, "EWP %s echo read", network)
			select {
			case err = <-written:
				require.NoError(t, err, "EWP %s echo write", network)
			case <-ctx.Done():
				t.Fatal("EWP echo write did not finish:", ctx.Err())
			}
			require.True(t, bytes.Equal(got[:n], payload), "EWP %s echo mismatch: %d bytes, want %d", network, n, len(payload))
			checkRoute()
		}()
	}

	// Use the same notification as a network change, then test the
	// address-bearing UDP entry point on a fresh transport pool.
	client.InterfaceUpdated(ctx)
	packets, err := client.ListenPacket(ctx, destination)
	require.NoError(t, err, "EWP ListenPacket after network reset")
	defer packets.Close()
	_ = packets.SetDeadline(time.Now().Add(4 * time.Second))
	for _, peer := range []M.Socksaddr{destination, M.ParseSocksaddr("[2001:db8::5]:53")} {
		payload := []byte("address-bearing UDP datagram")
		n, err := packets.WriteTo(payload, peer)
		require.NoError(t, err)
		require.Equal(t, len(payload), n)
		got := make([]byte, len(payload)+1)
		n, source, err := packets.ReadFrom(got)
		require.NoError(t, err)
		require.Equal(t, peer, M.SocksaddrFromNet(source).Unwrap())
		require.Equal(t, payload, got[:n])
	}
	checkRoute()
}

func ewpXHTTPTestCertificate(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{ewpXHTTPHost}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}
