package v2raygrpclite

import (
	std_bufio "bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/transport/v2rayhttp"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/baderror"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/varbin"

	"google.golang.org/protobuf/encoding/protowire"
)

// kanged from: https://github.com/Qv2ray/gun-lite

var _ net.Conn = (*GunConn)(nil)

const maxMessageSize = 4 << 20

type GunConn struct {
	conn        *v2rayhttp.HTTP2Conn
	reader      *std_bufio.Reader
	flusher     http.Flusher
	cancel      context.CancelFunc
	readAccess  sync.Mutex
	writeAccess sync.Mutex
	frame       []byte
	cache       []byte
	readErr     error
	closed      atomic.Bool
}

func newGunConn(reader io.Reader, writer io.Writer, flusher http.Flusher) *GunConn {
	conn := v2rayhttp.NewHTTPConn(reader, writer)
	return &GunConn{
		conn:    &conn,
		reader:  std_bufio.NewReader(&conn),
		flusher: flusher,
	}
}

func newLateGunConn(writer io.Writer) *GunConn {
	conn := v2rayhttp.NewLateHTTPConn(writer)
	return &GunConn{
		conn:   conn,
		reader: std_bufio.NewReader(conn),
	}
}

func (c *GunConn) setup(reader io.Reader, err error) {
	c.conn.Setup(reader, err)
	if err != nil && c.cancel != nil {
		c.cancel()
	}
}

func (c *GunConn) Read(b []byte) (n int, err error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if c.readErr != nil {
		return 0, c.readErr
	}
	n, err = c.read(b)
	err = baderror.WrapH2(err)
	c.readErr = err
	return n, err
}

func (c *GunConn) read(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}
	for len(c.cache) == 0 {
		var header [5]byte
		if _, err = io.ReadFull(c.reader, header[:]); err != nil {
			return 0, err
		}
		if header[0] != 0 {
			return 0, E.New("v2ray-grpc: compressed messages are unsupported")
		}
		messageSize := binary.BigEndian.Uint32(header[1:])
		if messageSize > maxMessageSize {
			return 0, E.New("v2ray-grpc: message exceeds maximum size")
		}
		if cap(c.frame) < int(messageSize) {
			c.frame = make([]byte, messageSize)
		} else {
			c.frame = c.frame[:messageSize]
		}
		if _, err = io.ReadFull(c.reader, c.frame); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return 0, E.New("v2ray-grpc: truncated message")
			}
			return 0, err
		}
		// Empty protobuf messages are legal. Parse only within this gRPC message
		// so an absent or unknown field cannot consume the next frame's header.
		message := c.frame
		var data []byte
		for len(message) > 0 {
			number, wireType, tagLen := protowire.ConsumeTag(message)
			if tagLen < 0 {
				return 0, E.New("v2ray-grpc: invalid message: ", protowire.ParseError(tagLen))
			}
			message = message[tagLen:]
			var fieldLen int
			if number == 1 && wireType == protowire.BytesType {
				data, fieldLen = protowire.ConsumeBytes(message)
			} else {
				fieldLen = protowire.ConsumeFieldValue(number, wireType, message)
			}
			if fieldLen < 0 {
				return 0, E.New("v2ray-grpc: invalid message: ", protowire.ParseError(fieldLen))
			}
			message = message[fieldLen:]
		}
		c.cache = data
	}
	n = copy(b, c.cache)
	c.cache = c.cache[n:]
	return n, nil
}

func (c *GunConn) Write(b []byte) (n int, err error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	varLen := varbin.UvarintLen(uint64(len(b)))
	buffer := buf.NewSize(6 + varLen + len(b))
	defer buffer.Release()
	header := buffer.Extend(6 + varLen)
	header[0] = 0x00
	binary.BigEndian.PutUint32(header[1:5], uint32(1+varLen+len(b)))
	header[5] = 0x0A
	binary.PutUvarint(header[6:], uint64(len(b)))
	common.Must1(buffer.Write(b))
	_, err = c.conn.Write(buffer.Bytes())
	if err != nil {
		return 0, baderror.WrapH2(err)
	}
	if c.flusher != nil {
		c.flusher.Flush()
	}
	return len(b), nil
}

func (c *GunConn) WriteBuffer(buffer *buf.Buffer) error {
	defer buffer.Release()
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	dataLen := buffer.Len()
	varLen := varbin.UvarintLen(uint64(dataLen))
	header := buffer.ExtendHeader(6 + varLen)
	header[0] = 0x00
	binary.BigEndian.PutUint32(header[1:5], uint32(1+varLen+dataLen))
	header[5] = 0x0A
	binary.PutUvarint(header[6:], uint64(dataLen))
	err := common.Error(c.conn.Write(buffer.Bytes()))
	if err != nil {
		return baderror.WrapH2(err)
	}
	if c.flusher != nil {
		c.flusher.Flush()
	}
	return nil
}

func (c *GunConn) FrontHeadroom() int {
	return 6 + binary.MaxVarintLen64
}

func (c *GunConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	err := c.conn.Close()
	c.readAccess.Lock()
	c.frame = nil
	c.cache = nil
	c.readAccess.Unlock()
	return err
}

func (c *GunConn) LocalAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *GunConn) RemoteAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *GunConn) SetDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *GunConn) SetReadDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *GunConn) SetWriteDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *GunConn) NeedAdditionalReadDeadline() bool {
	return true
}
