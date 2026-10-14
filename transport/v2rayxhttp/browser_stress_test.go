//go:build with_cronet && with_cronet_test

package xhttp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	stdTLS "crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justinwoo280/sing-xhttp/xhttp"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func newBrowserStressClient(tb testing.TB, ctx context.Context, mode string, framed bool) (adapter.V2RayClientTransport, *atomic.Int32, *atomic.Int32, *httptest.Server) {
	tb.Helper()
	server, err := xhttp.NewServer(ctx, logger.NOP(), xhttp.Options{Mode: mode, Path: "/stress", GRPCFraming: framed}, nil, browserEchoHandler{})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = server.Close() })
	connections, requests := new(atomic.Int32), new(atomic.Int32)
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		defer requests.Add(-1)
		server.ServeHTTP(w, r)
	}))
	httpServer.EnableHTTP2 = true
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	httpServer.TLS = &stdTLS.Config{Certificates: []stdTLS.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	httpServer.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	httpServer.StartTLS()
	tb.Cleanup(httpServer.Close)
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}))
	tlsConfig, err := tls.NewClient(ctx, logger.NOP(), "127.0.0.1", option.OutboundTLSOptions{Enabled: true, Certificate: []string{certificate}})
	if err != nil {
		tb.Fatal(err)
	}
	client, err := NewClient(ctx, N.SystemDialer, M.ParseSocksaddr(httpServer.Listener.Addr().String()), option.V2RayXHTTPOptions{
		Browser: true, V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/stress", GRPCFraming: framed},
	}, tlsConfig)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = client.Close() })
	return client, connections, requests, httpServer
}

func TestBrowserSustainedReuse(t *testing.T) {
	for _, stream := range []struct {
		mode   string
		framed bool
	}{
		{"packet-up", false}, {"stream-up", false}, {"stream-one", false},
		{"stream-up", true}, {"stream-one", true},
	} {
		t.Run(fmt.Sprintf("%s/framed=%t", stream.mode, stream.framed), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			client, connections, requests, _ := newBrowserStressClient(t, ctx, stream.mode, stream.framed)
			const workers, rounds = 32, 16
			failures := make(chan error, workers)
			var wg sync.WaitGroup
			start := time.Now()
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					payload := bytes.Repeat([]byte{byte(worker + 1)}, 64<<10)
					for round := 0; round < rounds; round++ {
						if err := browserStressRoundTrip(ctx, client, payload); err != nil {
							failures <- fmt.Errorf("worker %d round %d: %w", worker, round, err)
							return
						}
					}
				}()
			}
			wg.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if t.Failed() {
				return
			}
			deadline := time.After(2 * time.Second)
			for requests.Load() != 0 {
				select {
				case <-deadline:
					t.Fatalf("%d HTTP requests survived session close", requests.Load())
				case <-time.After(5 * time.Millisecond):
				}
			}
			if connections.Load() > workers*2 {
				t.Errorf("HTTP/2 reuse degraded: %d TCP connections for %d sessions", connections.Load(), workers*rounds)
			}
			t.Logf("%d sessions, %d TCP connections, zero remaining requests, elapsed %s", workers*rounds, connections.Load(), time.Since(start))
		})
	}
}

func browserStressRoundTrip(ctx context.Context, client adapter.V2RayClientTransport, payload []byte) error {
	conn, err := client.DialContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	written := make(chan error, 1)
	go func() { _, err := conn.Write(payload); written <- err }()
	got := make([]byte, len(payload))
	_, err = io.ReadFull(conn, got)
	if err != nil {
		_ = conn.Close()
	}
	writeErr := <-written
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("echo mismatch")
	}
	return nil
}

func TestBrowserConnectionLossRecovery(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up", "stream-one"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client, connections, _, server := newBrowserStressClient(t, ctx, mode, false)
			payload := []byte("active before connection loss")
			const rounds, active = 12, 4
			for round := 0; round < rounds; round++ {
				var conns []net.Conn
				for i := 0; i < active; i++ {
					conn, err := client.DialContext(ctx)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = conn.Close() })
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					if _, err := conn.Write(payload); err != nil {
						t.Fatal(err)
					}
					got := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
						t.Fatalf("before failure %d: %v", round, err)
					}
					conns = append(conns, conn)
				}
				server.CloseClientConnections()
				for _, conn := range conns {
					_, err := conn.Read(make([]byte, 1))
					var timeout net.Error
					if err == nil || errors.As(err, &timeout) && timeout.Timeout() {
						t.Fatalf("connection loss %d was not detected: %v", round, err)
					}
					_ = conn.Close()
				}
				// Keep the same Browser client/engine: recovery must not rely on
				// rebuilding the outbound or resetting its entire pool manually.
				if err := browserStressRoundTrip(ctx, client, payload); err != nil {
					t.Fatalf("recovery %d: %v", round, err)
				}
			}
			t.Logf("recovered from %d connection losses affecting %d active sessions; %d TCP connections", rounds, rounds*active, connections.Load())
		})
	}
}

func BenchmarkBrowserThroughput(b *testing.B) {
	for _, mode := range []string{"packet-up", "stream-up", "stream-one"} {
		b.Run(mode, func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client, connections, _, _ := newBrowserStressClient(b, ctx, mode, false)
			conn, err := client.DialContext(ctx)
			if err != nil {
				b.Fatal(err)
			}
			defer conn.Close()
			payload := bytes.Repeat([]byte("browser throughput\n"), 60000)
			got := make([]byte, len(payload))
			exchange := func() {
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				written := make(chan error, 1)
				go func() { _, err := conn.Write(payload); written <- err }()
				if _, err := io.ReadFull(conn, got); err != nil {
					_ = conn.Close()
					<-written
					b.Fatal(err)
				}
				if err := <-written; err != nil || !bytes.Equal(got, payload) {
					b.Fatalf("transfer: %v", err)
				}
			}
			exchange() // Exclude engine startup and the first TLS handshake.
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				exchange()
			}
			b.StopTimer()
			b.ReportMetric(float64(connections.Load()), "TCP-conns")
		})
	}
}

// Opt-in wall-clock test: cross idle/ping/stream-up heartbeat intervals while
// retaining the same session and native pool. The regular stress test covers
// rapid connection churn separately.
func TestBrowserLongLivedReuse(t *testing.T) {
	seconds, err := strconv.Atoi(os.Getenv("BROWSER_SOAK_SECONDS"))
	if err != nil || seconds < 60 {
		t.Skip("set BROWSER_SOAK_SECONDS >= 60 for the long-lived connection test")
	}
	for _, stream := range []struct {
		mode   string
		framed bool
	}{
		{"packet-up", false}, {"stream-up", false}, {"stream-one", false},
		{"stream-up", true}, {"stream-one", true},
	} {
		t.Run(fmt.Sprintf("%s/framed=%t", stream.mode, stream.framed), func(t *testing.T) {
			t.Parallel()
			duration := time.Duration(seconds) * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), duration+20*time.Second)
			defer cancel()
			client, connections, _, _ := newBrowserStressClient(t, ctx, stream.mode, stream.framed)
			conn, err := client.DialContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			payload := bytes.Repeat([]byte("long-lived browser"), 64)
			started := time.Now()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			var transfers int
			for {
				elapsed := time.Since(started)
				// Keep a quiet 40-second interval, then resume the same stream.
				if elapsed < 10*time.Second || elapsed > 50*time.Second {
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					if _, err := conn.Write(payload); err != nil {
						t.Fatal(err)
					}
					got := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
						t.Fatalf("transfer after %s: %v", elapsed, err)
					}
					_ = conn.SetDeadline(time.Time{})
					transfers++
				}
				if elapsed >= duration {
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			t.Logf("same session survived %s including 40s idle: %d transfers, %d TCP connections", duration, transfers, connections.Load())
		})
	}
}
