package v2rayhttp

import (
	"io"
	"net"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/buf"
)

func TestCachedConnConcurrentReadClose(t *testing.T) {
	for range 64 {
		raw, peer := net.Pipe()
		peer.Close()
		buffer := buf.NewSize(1024)
		buffer.Extend(1024)
		conn := NewCachedConn(raw, buffer).(*cachedConn)
		start := make(chan struct{})
		var group sync.WaitGroup
		group.Go(func() { <-start; io.Copy(io.Discard, conn) })
		group.Go(func() { <-start; conn.ReaderReplaceable() })
		group.Go(func() { <-start; conn.Close() })
		close(start)
		group.Wait()
		if buffer.Cap() != 0 {
			t.Fatal("cached buffer was not released")
		}
	}
}

func TestCachedConnTransfersReadBuffer(t *testing.T) {
	raw, peer := net.Pipe()
	defer peer.Close()
	buffer := buf.New()
	buffer.WriteString("payload")
	conn := NewCachedConn(raw, buffer).(*cachedConn)
	owned := conn.ReadCached()
	defer owned.Release()
	conn.Close()
	if string(owned.Bytes()) != "payload" {
		t.Fatal("Close released a buffer already transferred to the reader")
	}
}
