package route

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing/common/logger"
)

type halfCloseTestConn struct {
	net.Conn
	halfClosed chan struct{}
	once       sync.Once
}

func (c *halfCloseTestConn) CloseWrite() error { c.once.Do(func() { close(c.halfClosed) }); return nil }

func TestConnectionCopyPreservesTrackedHalfClose(t *testing.T) {
	manager := NewConnectionManager(logger.NOP())
	defer manager.Close()
	source, client := net.Pipe()
	defer source.Close()
	defer client.Close()
	destination, remote := net.Pipe()
	defer destination.Close()
	defer remote.Close()
	duplex := &halfCloseTestConn{Conn: destination, halfClosed: make(chan struct{})}
	tracked := manager.TrackConn(duplex)
	var done atomic.Bool
	finished := make(chan struct{})
	go func() {
		manager.connectionCopy(context.Background(), source, tracked, false, &done, nil)
		close(finished)
	}()
	go func() { _, _ = client.Write([]byte("upload")); client.Close() }()
	remote.SetReadDeadline(time.Now().Add(time.Second))
	var data [6]byte
	if _, err := io.ReadFull(remote, data[:]); err != nil || string(data[:]) != "upload" {
		t.Fatalf("copy: %q %v", data, err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("copy did not finish")
	}
	select {
	case <-duplex.halfClosed:
	default:
		t.Fatal("tracking wrapper hid CloseWrite")
	}
	// The receiving half is still usable after upload EOF.
	go func() { _, _ = remote.Write([]byte("reply!")) }()
	destination.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(destination, data[:]); err != nil || string(data[:]) != "reply!" {
		t.Fatalf("reverse direction closed: %q %v", data, err)
	}
}
