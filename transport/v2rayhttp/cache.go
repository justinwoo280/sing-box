package v2rayhttp

import (
	"bufio"
	"net"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing/common/buf"
)

func CacheBufferedConn(conn net.Conn, reader *bufio.Reader) (net.Conn, error) {
	if reader == nil || reader.Buffered() == 0 {
		return conn, nil
	}
	cacheLen := reader.Buffered()
	buffer := buf.NewSize(cacheLen)
	if _, err := buffer.ReadFullFrom(reader, cacheLen); err != nil {
		buffer.Release()
		return nil, err
	}
	return NewCachedConn(conn, buffer), nil
}

// NewCachedConn takes ownership of buffer, including when the stream is closed
// before its cached bytes have been consumed.
func NewCachedConn(conn net.Conn, buffer *buf.Buffer) net.Conn {
	if buffer == nil || buffer.IsEmpty() {
		buffer.Release()
		return conn
	}
	return &cachedConn{Conn: conn, buffer: buffer}
}

type cachedConn struct {
	net.Conn
	readAccess sync.Mutex
	buffer     *buf.Buffer
	empty      atomic.Bool
	closed     atomic.Bool
}

func (c *cachedConn) Read(p []byte) (int, error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if c.buffer != nil {
		n, err := c.buffer.Read(p)
		if c.buffer.IsEmpty() {
			c.buffer.Release()
			c.buffer = nil
			c.empty.Store(true)
		}
		return n, err
	}
	return c.Conn.Read(p)
}

func (c *cachedConn) ReadCached() *buf.Buffer {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.closed.Load() {
		return nil
	}
	buffer := c.buffer
	c.buffer = nil
	c.empty.Store(true)
	return buffer
}

func (c *cachedConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	err := c.Conn.Close()
	c.readAccess.Lock()
	if c.buffer != nil {
		c.buffer.Release()
		c.buffer = nil
	}
	c.empty.Store(true)
	c.readAccess.Unlock()
	return err
}

func (c *cachedConn) ReaderReplaceable() bool { return c.empty.Load() }
func (*cachedConn) WriterReplaceable() bool   { return true }
func (c *cachedConn) Upstream() any           { return c.Conn }
