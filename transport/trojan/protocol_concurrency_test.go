package trojan

import (
	"io"
	"net"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type discardConn struct{ net.Conn }

func (discardConn) Write(p []byte) (int, error) { return len(p), nil }

func TestClientConnConcurrentHandshake(t *testing.T) {
	conn := NewClientConn(discardConn{}, Key("test"), M.ParseSocksaddr("example.com:443"))
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			conn.NeedHandshakeForWrite()
			conn.FrontHeadroom()
			conn.ReaderReplaceable()
			conn.WriterReplaceable()
			if _, err := conn.Write([]byte("payload")); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
}

func TestClientPacketConnConcurrentHandshake(t *testing.T) {
	conn := NewClientPacketConn(discardConn{}, Key("test"))
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			conn.NeedHandshake()
			packet := buf.NewSize(512)
			packet.Resize(256, 0)
			packet.WriteString("payload")
			if err := conn.WritePacket(packet, M.ParseSocksaddr("example.com:53")); err != nil {
				t.Error(err)
			}
			conn.FrontHeadroom()
		})
	}
	group.Wait()
}

func TestClientHandshakeBufferReleased(t *testing.T) {
	for _, packet := range []bool{false, true} {
		buffer := buf.NewSize(512)
		buffer.Resize(256, 0)
		buffer.WriteString("payload")
		if packet {
			conn := NewClientPacketConn(discardConn{}, Key("test"))
			if err := conn.WritePacket(buffer, M.ParseSocksaddr("example.com:53")); err != nil {
				t.Fatal(err)
			}
		} else {
			conn := NewClientConn(discardConn{}, Key("test"), M.ParseSocksaddr("example.com:443"))
			if err := conn.WriteBuffer(buffer); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := buffer.ReadByte(); err != io.EOF {
			buffer.Release()
			t.Error("first write retained its owned buffer")
		}
	}
}
