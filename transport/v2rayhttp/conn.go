package v2rayhttp

import (
	std_bufio "bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/baderror"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type HTTPConn struct {
	net.Conn
	request           *http.Request
	requestWritten    atomic.Bool
	responseRead      bool
	responseCache     *buf.Buffer
	readerReplaceable atomic.Bool
	readAccess        sync.Mutex
	writeAccess       sync.Mutex
	closed            atomic.Bool
}

func NewHTTP1Conn(conn net.Conn, request *http.Request) *HTTPConn {
	if request.Header.Get("Host") == "" {
		request.Header.Set("Host", request.Host)
	}
	return &HTTPConn{
		Conn:    conn,
		request: request,
	}
}

func (c *HTTPConn) Read(b []byte) (n int, err error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if !c.responseRead {
		reader := std_bufio.NewReader(c.Conn)
		response, err := http.ReadResponse(reader, c.request)
		if err != nil {
			return 0, E.Cause(err, "read response")
		}
		if response.StatusCode != 200 {
			return 0, E.New("v2ray-http: unexpected status: ", response.Status)
		}
		if cacheLen := reader.Buffered(); cacheLen > 0 {
			c.responseCache = buf.NewSize(cacheLen)
			_, err = c.responseCache.ReadFullFrom(reader, cacheLen)
			if err != nil {
				c.responseCache.Release()
				c.responseCache = nil
				return 0, E.Cause(err, "read cache")
			}
		}
		c.responseRead = true
	}
	if c.responseCache != nil {
		n, err = c.responseCache.Read(b)
		if c.responseCache.IsEmpty() {
			c.responseCache.Release()
			c.responseCache = nil
			c.readerReplaceable.Store(true)
		}
		if n > 0 {
			return n, nil
		}
	}
	c.readerReplaceable.Store(true)
	return c.Conn.Read(b)
}

func (c *HTTPConn) Write(b []byte) (int, error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if !c.requestWritten.Load() {
		err := c.writeRequest(b)
		if err != nil {
			return 0, E.Cause(err, "write request")
		}
		c.requestWritten.Store(true)
		return len(b), nil
	}
	return c.Conn.Write(b)
}

func (c *HTTPConn) writeRequest(payload []byte) error {
	buffer := buf.New()
	defer buffer.Release()
	writer := bufio.NewBufferedWriter(c.Conn, buffer)
	const CRLF = "\r\n"
	_, err := writer.Write([]byte(F.ToString(c.request.Method, " ", c.request.URL.RequestURI(), " HTTP/1.1", CRLF)))
	if err != nil {
		return err
	}
	for key, value := range c.request.Header {
		_, err = writer.Write([]byte(F.ToString(key, ": ", strings.Join(value, ", "), CRLF)))
		if err != nil {
			return err
		}
	}
	_, err = writer.Write([]byte(CRLF))
	if err != nil {
		return err
	}
	_, err = writer.Write(payload)
	if err != nil {
		return err
	}
	err = writer.Fallthrough()
	if err != nil {
		return err
	}
	return nil
}

func (c *HTTPConn) ReaderReplaceable() bool {
	return c.readerReplaceable.Load()
}

func (c *HTTPConn) WriterReplaceable() bool {
	return c.requestWritten.Load()
}

func (c *HTTPConn) NeedHandshake() bool {
	return !c.requestWritten.Load()
}

func (c *HTTPConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	err := c.Conn.Close()
	c.readAccess.Lock()
	if c.responseCache != nil {
		c.responseCache.Release()
		c.responseCache = nil
	}
	c.readAccess.Unlock()
	return err
}

func (c *HTTPConn) Upstream() any {
	return c.Conn
}

type HTTP2Conn struct {
	access      sync.Mutex
	readAccess  sync.Mutex
	writeAccess sync.Mutex
	reader      io.Reader
	writer      io.Writer
	create      chan struct{}
	err         error
	ready       bool
	closed      bool
	cancel      context.CancelFunc
}

func NewHTTPConn(reader io.Reader, writer io.Writer) HTTP2Conn {
	return HTTP2Conn{
		reader: reader,
		writer: writer,
		ready:  true,
	}
}

func NewLateHTTPConn(writer io.Writer) *HTTP2Conn {
	return &HTTP2Conn{
		create: make(chan struct{}),
		writer: writer,
	}
}

func (c *HTTP2Conn) Setup(reader io.Reader, err error) {
	c.access.Lock()
	if c.ready {
		c.access.Unlock()
		common.Close(reader)
		return
	}
	c.reader = reader
	c.err = err
	c.ready = true
	close(c.create)
	c.access.Unlock()
	if err != nil {
		if c.cancel != nil {
			c.cancel()
		}
		common.Close(c.writer)
	}
}

func (c *HTTP2Conn) Read(b []byte) (n int, err error) {
	if c.create != nil {
		<-c.create
	}
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	c.access.Lock()
	reader, setupErr, closed := c.reader, c.err, c.closed
	c.access.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if setupErr != nil {
		return 0, setupErr
	}
	n, err = reader.Read(b)
	return n, baderror.WrapH2(err)
}

func (c *HTTP2Conn) Write(b []byte) (n int, err error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	c.access.Lock()
	setupErr, closed := c.err, c.closed
	c.access.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if setupErr != nil {
		return 0, setupErr
	}
	n, err = c.writer.Write(b)
	return n, baderror.WrapH2(err)
}

func (c *HTTP2Conn) Close() error {
	c.access.Lock()
	if c.closed {
		c.access.Unlock()
		return nil
	}
	c.closed = true
	reader := c.reader
	if !c.ready {
		c.ready = true
		close(c.create)
	}
	c.access.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	return common.Close(reader, c.writer)
}

func (c *HTTP2Conn) LocalAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *HTTP2Conn) RemoteAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *HTTP2Conn) SetDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *HTTP2Conn) SetReadDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *HTTP2Conn) SetWriteDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *HTTP2Conn) NeedAdditionalReadDeadline() bool {
	return true
}

type ServerHTTPConn struct {
	HTTP2Conn
	Flusher http.Flusher
}

func (c *ServerHTTPConn) Write(b []byte) (n int, err error) {
	n, err = c.writer.Write(b)
	if err == nil {
		c.Flusher.Flush()
	}
	return
}

type HTTP2ConnWrapper struct {
	N.ExtendedConn
	access sync.Mutex
	closed bool
}

func NewHTTP2Wrapper(conn net.Conn) *HTTP2ConnWrapper {
	return &HTTP2ConnWrapper{
		ExtendedConn: bufio.NewExtendedConn(conn),
	}
}

func (w *HTTP2ConnWrapper) Write(p []byte) (n int, err error) {
	w.access.Lock()
	defer w.access.Unlock()
	if w.closed {
		return 0, net.ErrClosed
	}
	return w.ExtendedConn.Write(p)
}

func (w *HTTP2ConnWrapper) WriteBuffer(buffer *buf.Buffer) error {
	w.access.Lock()
	defer w.access.Unlock()
	if w.closed {
		buffer.Release()
		return net.ErrClosed
	}
	return w.ExtendedConn.WriteBuffer(buffer)
}

func (w *HTTP2ConnWrapper) CloseWrapper() {
	w.access.Lock()
	defer w.access.Unlock()
	w.closed = true
}

func (w *HTTP2ConnWrapper) Close() error {
	// Closing the underlying stream must be able to interrupt a blocked writer
	// before waiting for it to leave the response writer critical section.
	err := w.ExtendedConn.Close()
	w.CloseWrapper()
	return err
}

func (w *HTTP2ConnWrapper) Upstream() any {
	return w.ExtendedConn
}

func DupContext(ctx context.Context) context.Context {
	id, loaded := log.IDFromContext(ctx)
	if !loaded {
		return context.Background()
	}
	return log.ContextWithID(context.Background(), id)
}
