//go:build with_cronet && with_cronet_test

package xhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func TestBrowserResetActiveConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			_, _ = io.Copy(io.Discard, r.Body)
			return
		}
		_, _ = io.WriteString(w, "ready")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := newBrowserClient(ctx, N.SystemDialer, M.ParseSocksaddr(server.Listener.Addr().String()), option.V2RayXHTTPOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	resetter := client.(interface{ Reset() error })
	for i := 0; i < 3; i++ {
		conn, err := client.DialContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ready [5]byte
		if _, err := io.ReadFull(conn, ready[:]); err != nil || string(ready[:]) != "ready" {
			t.Fatalf("read after reset %d: %q, %v", i, ready, err)
		}
		readDone := make(chan error, 1)
		go func() { _, err := conn.Read(make([]byte, 1)); readDone <- err }()
		if err := resetter.Reset(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-readDone:
			if err == nil {
				t.Fatal("reset left active download open")
			}
		case <-time.After(time.Second):
			t.Fatal("reset did not unblock download")
		}
		_ = conn.Close()
	}
	// Reset and permanent shutdown must be safe together, and reset must never
	// bring back an outbound that has already been shut down.
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_ = resetter.Reset()
			_ = client.Close()
		}()
	}
	workers.Wait()
	_ = resetter.Reset()
	if conn, err := client.DialContext(ctx); !errors.Is(err, net.ErrClosed) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("dial after permanent close: %v", err)
	}
}
