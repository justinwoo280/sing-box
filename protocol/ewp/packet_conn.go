package ewp

import (
	"net"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// Keep domain addresses intact and translate sing's Socksaddr before passing
// datagrams to EWP, whose standard WriteTo only accepts its own address types.
type ewpPacketConn struct{ net.PacketConn }

func newEWPPacketConn(conn net.PacketConn) N.NetPacketConn {
	return &ewpPacketConn{conn}
}

func (c *ewpPacketConn) WriteTo(p []byte, address net.Addr) (int, error) {
	if writer, ok := c.PacketConn.(interface {
		WriteToAddress([]byte, sewp.Address) (int, error)
	}); ok {
		return writer.WriteToAddress(p, socksaddrToEWP(M.SocksaddrFromNet(address).Unwrap()))
	}
	return c.PacketConn.WriteTo(p, address)
}

func (c *ewpPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	n, address, err := c.PacketConn.ReadFrom(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(n)
	return M.SocksaddrFromNet(address).Unwrap(), nil
}

func (c *ewpPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	defer buffer.Release()
	return common.Error(c.WriteTo(buffer.Bytes(), destination))
}
