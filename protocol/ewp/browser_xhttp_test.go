//go:build with_cronet && with_cronet_test && with_utls

package ewp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	stdTLS "crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type browserXHTTPEchoRouter struct {
	adapter.Router
	metadata chan adapter.InboundContext
}

func (r *browserXHTTPEchoRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.metadata <- metadata
	go func() {
		_, _ = io.Copy(conn, conn)
		_ = conn.Close()
	}()
}

func (r *browserXHTTPEchoRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, metadata adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.metadata <- metadata
	go func() {
		_, _ = bufio.CopyPacket(conn, conn)
		_ = conn.Close()
	}()
}

// Exercise actual EWP constructors, authenticated handshakes, ticket reuse and
// TCP and both UDP entry points over each Browser XHTTP wire format.
func TestEWPBrowserXHTTP(t *testing.T) {
	for _, stream := range []struct {
		mode   string
		framed bool
	}{
		{mode: "packet-up"},
		{mode: "stream-up"},
		{mode: "stream-one"},
		{mode: "stream-up", framed: true},
		{mode: "stream-one", framed: true},
	} {
		name := stream.mode
		if stream.framed {
			name += "/grpc"
		}
		for _, security := range []string{"tls", "reality"} {
			t.Run(name+"/"+security, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				certificate, certPEM, keyPEM := browserXHTTPCertificate(t)
				serverTLS := &option.InboundTLSOptions{Enabled: true, Certificate: []string{certPEM}, Key: []string{keyPEM}}
				clientTLS := &option.OutboundTLSOptions{Enabled: true, ServerName: "ewp.test", Certificate: []string{certPEM}}
				if security == "reality" {
					target := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
						t.Error("EWP traffic reached the camouflage target")
					}))
					target.EnableHTTP2 = true
					target.Config.ErrorLog = log.New(io.Discard, "", 0)
					target.TLS = &stdTLS.Config{MinVersion: stdTLS.VersionTLS13, Certificates: []stdTLS.Certificate{certificate}}
					target.StartTLS()
					defer target.Close()
					key, err := ecdh.X25519().GenerateKey(rand.Reader)
					if err != nil {
						t.Fatal(err)
					}
					address := M.ParseSocksaddr(target.Listener.Addr().String())
					serverTLS = &option.InboundTLSOptions{Enabled: true, ServerName: "ewp.test", Reality: &option.InboundRealityOptions{
						Enabled: true, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), ShortID: []string{"0102"},
						Handshake: option.InboundRealityHandshakeOptions{ServerOptions: option.ServerOptions{Server: address.AddrString(), ServerPort: address.Port}},
					}}
					clientTLS = &option.OutboundTLSOptions{Enabled: true, ServerName: "ewp.test", Reality: &option.OutboundRealityOptions{
						Enabled: true, PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "0102",
					}}
				}
				privateKey, publicKey, err := sewp.GenerateSigningIdentity()
				if err != nil {
					t.Fatal(err)
				}
				const uuid = "11111111-2222-3333-4444-555555555555"
				transport := option.V2RayTransportOptions{Type: C.V2RayTransportTypeXHTTP, XHTTPOptions: option.V2RayXHTTPOptions{
					V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: stream.mode, Path: "/ewp", Host: "front.test", GRPCFraming: stream.framed},
				}}
				router := &browserXHTTPEchoRouter{metadata: make(chan adapter.InboundContext, 3)}
				inbound, err := NewInbound(ctx, router, logger.NOP(), "ewp-in", option.EWPInboundOptions{
					ServerID: "browser-test", SigningPrivateKey: privateKey, RouteEpoch: 7,
					Users: []option.EWPUser{{Name: "alice", UUID: uuid}}, Transport: &transport,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: serverTLS},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer inbound.Close()
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				go func() { _ = inbound.(*Inbound).transport.Serve(listener) }()
				clientTransport := transport
				clientTransport.XHTTPOptions.Browser = true
				address := M.ParseSocksaddr(listener.Addr().String())
				outbound, err := NewOutbound(ctx, nil, logger.NOP(), "ewp-out", option.EWPOutboundOptions{
					UUID: uuid, ServerID: "browser-test", ServerPublicKey: publicKey, RouteEpoch: 7,
					ServerOptions: option.ServerOptions{Server: address.AddrString(), ServerPort: address.Port},
					Transport:     &clientTransport, OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: clientTLS},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer outbound.(*Outbound).Close()
				destination := M.ParseSocksaddr("destination.test:443")
				for index, network := range []string{N.NetworkTCP, N.NetworkUDP, N.NetworkTCP} {
					conn, err := outbound.DialContext(ctx, network, destination)
					if err != nil {
						t.Fatalf("flow %d (%s): %v", index, network, err)
					}
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					payload := []byte("EWP authenticated Browser flow")
					if network == N.NetworkTCP {
						payload = bytes.Repeat(payload, 10000)
					}
					written := make(chan error, 1)
					go func() {
						_, err := conn.Write(payload)
						if err != nil {
							_ = conn.Close()
						}
						written <- err
					}()
					got := make([]byte, len(payload))
					var readErr error
					if network == N.NetworkUDP {
						got = make([]byte, len(payload)+1)
						var n int
						n, readErr = conn.Read(got)
						got = got[:n]
					} else {
						_, readErr = io.ReadFull(conn, got)
					}
					_ = conn.Close()
					writeErr := <-written
					if readErr != nil {
						t.Fatalf("flow %d (%s): read %v, write %v", index, network, readErr, writeErr)
					}
					if writeErr != nil || !bytes.Equal(got, payload) {
						t.Fatalf("flow %d (%s): echo mismatch or write error: %v", index, network, writeErr)
					}
					select {
					case metadata := <-router.metadata:
						if metadata.User != "alice" || metadata.Destination != destination {
							t.Fatalf("incorrect EWP metadata: %+v", metadata)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				packets, err := outbound.ListenPacket(ctx, destination)
				if err != nil {
					t.Fatal("ListenPacket:", err)
				}
				defer packets.Close()
				_ = packets.SetDeadline(time.Now().Add(5 * time.Second))
				for index, peer := range []M.Socksaddr{
					destination,
					M.ParseSocksaddr("203.0.113.5:53"),
					M.ParseSocksaddr("[2001:db8::5]:5353"),
				} {
					payload := bytes.Repeat([]byte{byte(index + 1)}, 64+index)
					if n, err := packets.WriteTo(payload, peer); n != len(payload) || err != nil {
						t.Fatalf("ListenPacket write to %s: %d, %v", peer, n, err)
					}
					got := make([]byte, len(payload)+1)
					n, source, err := packets.ReadFrom(got)
					if err != nil || !bytes.Equal(got[:n], payload) || M.SocksaddrFromNet(source).Unwrap() != peer {
						t.Fatalf("ListenPacket echo from %s: %d bytes, %v", source, n, err)
					}
				}
				_ = packets.Close()
				select {
				case metadata := <-router.metadata:
					if metadata.User != "alice" || metadata.Destination != destination {
						t.Fatalf("incorrect ListenPacket metadata: %+v", metadata)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	}
}

func browserXHTTPCertificate(t *testing.T) (stdTLS.Certificate, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"ewp.test"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return stdTLS.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}))
}
