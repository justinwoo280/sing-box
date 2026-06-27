package xhttp

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sagernet/sing-box/common/xray/signal/done"
	E "github.com/sagernet/sing/common/exceptions"
)

type splitConn struct {
	writer     io.WriteCloser
	reader     io.ReadCloser
	remoteAddr net.Addr
	localAddr  net.Addr
	onClose    func()
}

func (c *splitConn) Write(b []byte) (int, error) {
	return c.writer.Write(b)
}

func (c *splitConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *splitConn) Close() error {
	if c.onClose != nil {
		c.onClose()
	}

	var errs []error
	if c.writer != nil {
		if err := c.writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return E.Errors(errs...)
}

func (c *splitConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *splitConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *splitConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

func (c *splitConn) SetReadDeadline(t time.Time) error {
	if dr, ok := c.reader.(interface{ SetReadDeadline(time.Time) error }); ok {
		return dr.SetReadDeadline(t)
	}
	return nil
}

func (c *splitConn) SetWriteDeadline(t time.Time) error {
	if dw, ok := c.writer.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return dw.SetWriteDeadline(t)
	}
	return nil
}

func (c *splitConn) NeedAdditionalReadDeadline() bool {
	return true
}

type H1Conn struct {
	UnreadedResponsesCount int
	RespBufReader          *bufio.Reader
	net.Conn
}

func NewH1Conn(conn net.Conn) *H1Conn {
	return &H1Conn{
		RespBufReader: bufio.NewReader(conn),
		Conn:          conn,
	}
}

type httpServerConn struct {
	sync.Mutex
	*done.Instance
	io.Reader // no need to Close request.Body
	http.ResponseWriter
}

func (c *httpServerConn) Write(b []byte) (int, error) {
	c.Lock()
	defer c.Unlock()
	if c.Done() {
		return 0, io.ErrClosedPipe
	}
	n, err := c.ResponseWriter.Write(b)
	if err == nil {
		c.ResponseWriter.(http.Flusher).Flush()
	}
	return n, err
}

func (c *httpServerConn) Close() error {
	c.Lock()
	defer c.Unlock()
	return c.Instance.Close()
}
