//go:build with_cronet && with_cronet_test && with_utls

package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	stdTLS "crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/cronet-go"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/crypto/hkdf"
)

// Run the actual sing-box REALITY server and XHTTP transport. The ordinary TLS
// target is local, so authentication and both key exchanges need no public site.
func TestBrowserRealityInterop(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up"} {
		for _, scenario := range []string{"hybrid", "x25519", "wrong-public-key", "wrong-short-id", "camouflage-public-key", "camouflage-short-id"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				camouflage := strings.HasPrefix(scenario, "camouflage-")
				fallbackSeen := make(chan *http.Request, 8)
				var fallbackRequests atomic.Int32
				target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fallbackRequests.Add(1)
					body, err := io.ReadAll(r.Body)
					if !camouflage || err != nil || len(body) != 0 || r.Method != "GET" || r.RequestURI != "/" || r.Host != "reality.test" || r.ProtoMajor != 2 {
						t.Errorf("unexpected forwarded HTTP request: %s %s host=%s proto=%s body=%q err=%v", r.Method, r.RequestURI, r.Host, r.Proto, body, err)
					}
					if r.Header.Get("Authorization") != "" || r.Header.Get("X-Private") != "" || r.Header.Get("Content-Type") != "" || r.Header.Get("User-Agent") != "CamouflageInterop/1" {
						t.Errorf("forwarded XHTTP headers: %v", r.Header)
					}
					cookie, err := r.Cookie("padding")
					if err != nil || len(r.Cookies()) != 1 || len(cookie.Value) < 30 || len(cookie.Value) > 61 || strings.Trim(cookie.Value, "0") != "" {
						t.Errorf("unexpected camouflage cookie: %q", r.Header.Get("Cookie"))
					}
					w.WriteHeader(http.StatusNoContent)
					fallbackSeen <- r
				}))
				target.EnableHTTP2 = true
				target.Config.ErrorLog = log.New(io.Discard, "", 0)
				group := stdTLS.X25519MLKEM768
				if scenario == "x25519" {
					group = stdTLS.X25519
				}
				cert, roots := browserRealityTargetCertificate(t)
				target.TLS = &stdTLS.Config{MinVersion: stdTLS.VersionTLS13, CurvePreferences: []stdTLS.CurveID{group}, Certificates: []stdTLS.Certificate{cert}}
				target.StartTLS()
				defer target.Close()
				key, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				targetAddr := M.ParseSocksaddr(target.Listener.Addr().String())
				serverTLS, err := tls.NewRealityServer(ctx, nil, option.InboundTLSOptions{
					Enabled: true, ServerName: "reality.test", ALPN: []string{"h2", "http/1.1"},
					Reality: &option.InboundRealityOptions{
						Enabled: true, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()),
						ShortID: []string{"0102030405060708"}, MaxTimeDifference: badoption.Duration(time.Minute),
						Handshake: option.InboundRealityHandshakeOptions{ServerOptions: option.ServerOptions{Server: targetAddr.AddrString(), ServerPort: targetAddr.Port}},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				observed := &browserRealityServer{ServerConfig: serverTLS, t: t}
				server, err := NewServer(ctx, logger.NOP(), option.V2RayXHTTPOptions{V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{
					Mode: mode, Path: "/reality", Host: "front.test",
				}}, observed, browserEchoHandler{})
				if err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				defer server.Close()
				go func() { _ = server.Serve(listener) }()
				defer observed.closeConnections()

				publicKey := key.PublicKey().Bytes()
				shortID := "0102030405060708"
				if scenario == "wrong-public-key" || scenario == "camouflage-public-key" {
					wrong, err := ecdh.X25519().GenerateKey(rand.Reader)
					if err != nil {
						t.Fatal(err)
					}
					publicKey = wrong.PublicKey().Bytes()
				} else if scenario == "wrong-short-id" || scenario == "camouflage-short-id" {
					shortID = "ff"
				}
				clientTLS, err := tls.NewClientWithOptions(tls.ClientOptions{
					Context: ctx, Logger: logger.NOP(), ServerAddress: "reality.test", Browser: true,
					Options: option.OutboundTLSOptions{Enabled: true, Reality: &option.OutboundRealityOptions{
						Enabled: true, PublicKey: base64.RawURLEncoding.EncodeToString(publicKey), ShortID: shortID,
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				var client adapter.V2RayClientTransport
				if camouflage {
					// Only the fixture trusts a local CA. Configure a native engine
					// directly so production Browser REALITY still disallows custom roots.
					engine := cronet.NewEngine()
					defer engine.Destroy()
					sid, _ := hex.DecodeString(shortID)
					var short [8]byte
					copy(short[:], sid)
					if err := engine.SetReality([32]byte(publicKey), short); err != nil {
						t.Fatal(err)
					}
					if !engine.SetTrustedRootCertificates(roots) {
						t.Fatal("set fixture root")
					}
					params := cronet.NewEngineParams()
					defer params.Destroy()
					params.SetEnableCheckResult(false)
					params.SetEnableHTTP2(true)
					params.SetEnableQuic(false)
					params.SetUserAgent("CamouflageInterop/1")
					if err := params.SetHostResolverRules("MAP reality.test 127.0.0.1"); err != nil {
						t.Fatal(err)
					}
					if got := engine.StartWithParams(params); got != cronet.ResultSuccess {
						t.Fatal(got)
					}
					defer engine.Shutdown()
					client, err = cronet.NewBrowserXHTTPClient(ctx, "https://reality.test:"+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), cronet.BrowserXHTTPOptions{
						Engine: engine, Mode: mode, Path: "/reality?private=query", Host: "front.test",
						Headers: http.Header{"Authorization": {"secret"}, "Cookie": {"private=secret"}, "X-Private": {"secret"}},
					})
				} else {
					client, err = NewClient(ctx, N.SystemDialer, M.ParseSocksaddr(listener.Addr().String()), option.V2RayXHTTPOptions{
						Browser: true, V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/reality", Host: "front.test"},
					}, clientTLS)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				if scenario == "wrong-public-key" || scenario == "wrong-short-id" || camouflage {
					conn, err := client.DialContext(ctx)
					if err == nil {
						_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
						_, err = conn.Read(make([]byte, 1))
						_ = conn.Close()
					}
					if err == nil {
						t.Fatal("invalid REALITY credentials succeeded")
					}
					if camouflage {
						var nativeError *cronet.ErrorGo
						if !errors.As(err, &nativeError) || nativeError.InternalErrorCode != cronet.NetErrorRealityAuthenticationFailed.Code() || nativeError.Retryable {
							t.Fatalf("expected REALITY authentication failure: %v", err)
						}
						select {
						case <-fallbackSeen:
						case <-ctx.Done():
							t.Fatal("no camouflage GET after REALITY forwarding:", ctx.Err())
						}
					} else if fallbackRequests.Load() != 0 {
						t.Fatal("untrusted target received an HTTP request")
					}
					if observed.accepted.Load() != 0 {
						t.Fatal("authentication failure reached XHTTP")
					}
					return
				}
				echo := func(payload []byte) {
					t.Helper()
					conn, err := client.DialContext(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
					written := make(chan error, 1)
					go func() { _, err := conn.Write(payload); written <- err }()
					got := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, got); err != nil {
						t.Fatal(err)
					}
					if err := <-written; err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, payload) {
						t.Fatal("REALITY XHTTP echo mismatch")
					}
				}
				echo(bytes.Repeat([]byte("Browser REALITY echo\n"), 100000))
				first := observed.accepted.Load()
				echo([]byte("reuse authenticated HTTP/2 connection"))
				if first == 0 || observed.accepted.Load() != first {
					t.Fatal("HTTP/2 connection was not reused")
				}
				observed.checkHellos(t, key, uint16(group))
				observed.closeConnections()
				echo([]byte("fresh REALITY authentication after reconnect"))
				if observed.accepted.Load() <= first {
					t.Fatal("reconnection did not perform a new handshake")
				}
				if fallbackRequests.Load() != 0 {
					t.Fatal("XHTTP request reached camouflage target")
				}
			})
		}
	}
}

func browserRealityTargetCertificate(t *testing.T) (stdTLS.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"reality.test"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return stdTLS.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

type browserRealityServer struct {
	tls.ServerConfig
	t        *testing.T
	accepted atomic.Int32
	mu       sync.Mutex
	conns    []*browserRealityWireConn
}

func (s *browserRealityServer) ServerHandshake(ctx context.Context, conn net.Conn) (tls.Conn, error) {
	wire := &browserRealityWireConn{Conn: conn}
	s.mu.Lock()
	s.conns = append(s.conns, wire)
	s.mu.Unlock()
	result, err := tls.ServerHandshake(ctx, wire, s.ServerConfig)
	if err != nil {
		return nil, err
	}
	state := result.ConnectionState()
	if state.Version != stdTLS.VersionTLS13 || state.DidResume || (state.NegotiatedProtocol != "" && state.NegotiatedProtocol != "h2") || state.ServerName != "reality.test" {
		s.t.Errorf("unexpected REALITY connection state: version=%x resume=%v ALPN=%s SNI=%s", state.Version, state.DidResume, state.NegotiatedProtocol, state.ServerName)
	}
	wire.accepted.Store(true)
	s.accepted.Add(1)
	return &browserRealityPlainConn{Conn: result, t: s.t}, nil
}

type browserRealityPlainConn struct {
	tls.Conn
	t       *testing.T
	preface []byte
}

func (c *browserRealityPlainConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	const preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	if len(c.preface) < len(preface) {
		c.preface = append(c.preface, p[:min(n, len(preface)-len(c.preface))]...)
		if len(c.preface) == len(preface) && string(c.preface) != preface {
			c.t.Errorf("REALITY application did not use HTTP/2: %q", c.preface)
		}
	}
	return n, err
}

func (s *browserRealityServer) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
}

type browserRealityWireConn struct {
	net.Conn
	accepted      atomic.Bool
	mu            sync.Mutex
	read, written []byte
}

func (c *browserRealityWireConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.mu.Lock()
	c.read = append(c.read, p[:min(n, max(0, 8192-len(c.read)))]...)
	c.mu.Unlock()
	return n, err
}

func (c *browserRealityWireConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.mu.Lock()
	c.written = append(c.written, p[:min(n, max(0, 8192-len(c.written)))]...)
	c.mu.Unlock()
	return n, err
}

func realityHelloExtensions(t *testing.T, raw []byte, client bool) (hello, session []byte, extensions map[uint16][]byte) {
	t.Helper()
	var message []byte
	for len(raw) >= 5 {
		n := int(binary.BigEndian.Uint16(raw[3:5]))
		if len(raw) < 5+n {
			break
		}
		if raw[0] == 22 {
			message = append(message, raw[5:5+n]...)
		}
		raw = raw[5+n:]
		if len(message) >= 4 {
			length := int(message[1])<<16 | int(message[2])<<8 | int(message[3])
			if len(message) >= length+4 {
				message = message[:length+4]
				break
			}
		}
	}
	if len(message) < 39 {
		t.Fatal("missing TLS hello")
	}
	reader := bytes.NewReader(message[38:])
	readVector := func(wide bool) []byte {
		var n uint16
		if wide {
			if err := binary.Read(reader, binary.BigEndian, &n); err != nil {
				t.Fatal(err)
			}
		} else {
			b, err := reader.ReadByte()
			if err != nil {
				t.Fatal(err)
			}
			n = uint16(b)
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(reader, b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	session = readVector(false)
	if client {
		_ = readVector(true)
		_ = readVector(false)
	} else {
		if _, err := reader.Seek(3, io.SeekCurrent); err != nil {
			t.Fatal(err)
		}
	}
	ext := readVector(true)
	extensions = make(map[uint16][]byte)
	for len(ext) >= 4 {
		id, n := binary.BigEndian.Uint16(ext), int(binary.BigEndian.Uint16(ext[2:]))
		if n > len(ext)-4 {
			t.Fatal("invalid extension length")
		}
		extensions[id] = bytes.Clone(ext[4 : 4+n])
		ext = ext[4+n:]
	}
	return message, session, extensions
}

func (s *browserRealityServer) checkHellos(t *testing.T, key *ecdh.PrivateKey, group uint16) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if !c.accepted.Load() {
			continue
		}
		c.mu.Lock()
		read, written := bytes.Clone(c.read), bytes.Clone(c.written)
		c.mu.Unlock()
		hello, sid, ext := realityHelloExtensions(t, read, true)
		if len(sid) != 32 {
			t.Fatal("REALITY session ID length")
		}
		sigs := ext[13]
		if len(sigs) < 2 {
			t.Fatal("missing signature algorithms")
		}
		for i := 2; i+2 <= len(sigs); i += 2 {
			if binary.BigEndian.Uint16(sigs[i:]) == 0x0807 {
				t.Fatal("ClientHello advertised Ed25519")
			}
		}
		if _, ok := ext[0xfe0d]; !ok {
			t.Fatal("ECH GREASE missing")
		}
		shares := ext[51]
		if len(shares) < 2 {
			t.Fatal("missing key shares")
		}
		shares = shares[2:]
		var x25519 []byte
		hybrid := false
		for len(shares) >= 4 {
			id, n := binary.BigEndian.Uint16(shares), int(binary.BigEndian.Uint16(shares[2:]))
			if n > len(shares)-4 {
				t.Fatal("invalid key share")
			}
			if id == 29 {
				x25519 = shares[4 : 4+n]
			}
			if id == 4588 {
				hybrid = n == 1216
			}
			shares = shares[4+n:]
		}
		if len(x25519) != 32 || !hybrid {
			t.Fatal("Chrome hybrid/X25519 shares not preserved")
		}
		pub, err := ecdh.X25519().NewPublicKey(x25519)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := key.ECDH(pub)
		if err != nil {
			t.Fatal(err)
		}
		auth := make([]byte, 32)
		if _, err := io.ReadFull(hkdf.New(sha256.New, secret, hello[6:26], []byte("REALITY")), auth); err != nil {
			t.Fatal(err)
		}
		block, err := aes.NewCipher(auth)
		if err != nil {
			t.Fatal(err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
		clear(hello[39:71])
		plain, err := aead.Open(nil, hello[26:38], sid, hello)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain[:4], []byte{1, 8, 1, 0}) || !bytes.Equal(plain[8:], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Fatalf("incorrect ClientVer/short ID: %x", plain)
		}
		if time.Since(time.Unix(int64(binary.BigEndian.Uint32(plain[4:8])), 0)).Abs() > time.Minute {
			t.Fatal("incorrect REALITY timestamp")
		}
		_, echoed, serverExt := realityHelloExtensions(t, written, false)
		if !bytes.Equal(sid, echoed) || len(serverExt[51]) < 2 || binary.BigEndian.Uint16(serverExt[51]) != group {
			t.Fatal("incorrect server key exchange/session echo")
		}
	}
}
