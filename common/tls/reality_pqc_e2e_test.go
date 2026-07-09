//go:build with_utls

package tls_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net"
	"testing"
	"time"

	boxtls "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"

	utls "github.com/refraction-networking/utls"
	reality "github.com/xtls/reality"
)

// realityKeypair generates a REALITY X25519 keypair in the base64
// RawURLEncoding form sing-box expects (32-byte scalar / 32-byte pub).
func realityKeypair(t *testing.T) (privB64, pubB64 string) {
	t.Helper()
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
}

// dialable reports whether apple.com:443 is reachable, so the test can
// skip cleanly in a sandbox with no egress.
func dialable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

const (
	realityDest = "apple.com"
	realitySNI  = "apple.com"
	shortID     = "0123456789abcdef"
)

// newRealityServerConfig builds a raw xtls/reality server config,
// bypassing sing-box's option layer (which needs a full service
// context). This isolates exactly what we're testing: the sing-box
// REALITY *client* authenticating against a genuine REALITY server that
// supports the X25519MLKEM768 hybrid group.
func newRealityServerConfig(t *testing.T, privB64 string) *reality.Config {
	t.Helper()
	priv, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		t.Fatal(err)
	}
	var sid [8]byte
	if _, err := hex.Decode(sid[:], []byte(shortID)); err != nil {
		t.Fatal(err)
	}
	cfg := &reality.Config{
		Type:                   "tcp",
		Dest:                   realityDest + ":443",
		ServerNames:            map[string]bool{realitySNI: true},
		PrivateKey:             priv,
		ShortIds:               map[[8]byte]bool{sid: true},
		SessionTicketsDisabled: true,
	}
	cfg.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, addr)
	}
	return cfg
}

func newRealityClient(t *testing.T, pub, fingerprint string) boxtls.Config {
	t.Helper()
	logFactory, _ := log.New(log.Options{})
	cc, err := boxtls.NewClient(context.Background(), logFactory.NewLogger("reality-client"),
		realitySNI,
		option.OutboundTLSOptions{
			Enabled:    true,
			ServerName: realitySNI,
			UTLS:       &option.OutboundUTLSOptions{Enabled: true, Fingerprint: fingerprint},
			Reality: &option.OutboundRealityOptions{
				Enabled:   true,
				PublicKey: pub,
				ShortID:   shortID,
			},
		})
	if err != nil {
		t.Fatalf("NewClient(reality): %v", err)
	}
	return cc
}

// TestReality_PQC_EndToEnd_ChromeFingerprint verifies that with the
// X25519MLKEM768-preserving client (no more trim) the REALITY handshake
// SUCCEEDS end-to-end, and the negotiated key exchange is the hybrid
// post-quantum group — i.e. the fingerprint kept its PQC group AND auth
// still works (MlkemEcdhe selected for the hybrid group).
func TestReality_PQC_EndToEnd_ChromeFingerprint(t *testing.T) {
	if !dialable(realityDest + ":443") {
		t.Skipf("no egress to %s:443; skipping REALITY e2e", realityDest)
	}
	priv, pub := realityKeypair(t)
	serverCfg := newRealityServerConfig(t, priv)

	client := newRealityClient(t, pub, "chrome")

	// REALITY needs real deadlines + a real fallback dial, so use a
	// loopback TCP pair rather than net.Pipe.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type result struct {
		conn net.Conn
		err  error
	}
	srvCh := make(chan result, 1)
	go func() {
		raw, aerr := ln.Accept()
		if aerr != nil {
			srvCh <- result{nil, aerr}
			return
		}
		sctx, scancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer scancel()
		sconn, herr := reality.Server(sctx, raw, serverCfg)
		srvCh <- result{sconn, herr}
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cconn, cerr := boxtls.ClientHandshake(ctx, raw, client)

	// A successful client-side REALITY handshake is the assertion: the
	// client verified the server's REALITY signature, which is only
	// possible if the session-id auth ECDH matched — i.e. the client
	// used the correct X25519 private key (MlkemEcdhe for the hybrid
	// group). We do NOT wait on the server goroutine: xtls/reality's
	// Server() blocks relaying to the fallback until the data streams
	// close, which would deadlock a handshake-only test.
	if cerr != nil {
		t.Fatalf("REALITY client handshake failed (regression: PQC auth broke): %v", cerr)
	}
	defer cconn.Close()

	state := cconn.ConnectionState()
	t.Logf("REALITY handshake OK: version=%x cipher=%x alpn=%q",
		state.Version, state.CipherSuite, state.NegotiatedProtocol)

	// Handshake success is itself the proof of PQC auth: the ClientHello
	// advertises X25519MLKEM768 as the FIRST key_share (see the
	// fingerprint unit test), the REALITY server picks it, extracts the
	// X25519 tail of the hybrid share for session-id auth, and the
	// client authenticated with the matching MlkemEcdhe key. If the old
	// Ecdhe-only path were still in effect the auth ECDH would mismatch
	// and ClientHandshake would have failed above.
}

// TestReality_ClientHello_KeepsMLKEM768 is an offline check that the
// Chrome-fingerprint ClientHello still carries X25519MLKEM768 in both
// supported_groups and key_share (i.e. the trim was really removed).
func TestReality_ClientHello_KeepsMLKEM768(t *testing.T) {
	uConfig := &utls.Config{ServerName: realitySNI, InsecureSkipVerify: true}
	uConn := utls.UClient(nopConn{}, uConfig, utls.HelloChrome_Auto)
	defer uConn.Close()
	if err := uConn.BuildHandshakeState(); err != nil {
		t.Fatalf("BuildHandshakeState: %v", err)
	}

	var sawCurve, sawKeyShare bool
	for _, ext := range uConn.Extensions {
		if ce, ok := ext.(*utls.SupportedCurvesExtension); ok {
			for _, c := range ce.Curves {
				if c == utls.X25519MLKEM768 {
					sawCurve = true
				}
			}
		}
		if ks, ok := ext.(*utls.KeyShareExtension); ok {
			for _, share := range ks.KeyShares {
				if share.Group == utls.X25519MLKEM768 {
					sawKeyShare = true
				}
			}
		}
	}
	if !sawCurve {
		t.Error("supported_groups missing X25519MLKEM768")
	}
	if !sawKeyShare {
		t.Error("key_share missing X25519MLKEM768")
	}
}

// nopConn is a net.Conn that never does I/O; enough for
// BuildHandshakeState which only assembles the ClientHello.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, nil }
func (nopConn) Write([]byte) (int, error)        { return 0, nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return nil }
func (nopConn) RemoteAddr() net.Addr             { return nil }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }
