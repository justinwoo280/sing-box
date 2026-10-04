package obfs

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

type countingConn struct {
	net.Conn
	handshakes atomic.Int32
}

func (c *countingConn) Write(p []byte) (int, error) {
	if len(p) > 0 && (p[0] == 'G' || p[0] == 22) {
		c.handshakes.Add(1)
	}
	return len(p), nil
}

func TestConcurrentFirstWrite(t *testing.T) {
	for _, protocol := range []string{"http", "tls"} {
		t.Run(protocol, func(t *testing.T) {
			raw := new(countingConn)
			var conn net.Conn
			if protocol == "http" {
				conn = NewHTTPObfs(raw, "example.com", "80")
			} else {
				conn = NewTLSObfs(raw, "example.com")
			}
			var group sync.WaitGroup
			for range 32 {
				group.Go(func() {
					if _, err := conn.Write([]byte("payload")); err != nil {
						t.Error(err)
					}
				})
			}
			group.Wait()
			if n := raw.handshakes.Load(); n != 1 {
				t.Fatalf("sent %d handshakes on one connection", n)
			}
		})
	}
}
