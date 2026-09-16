//go:build with_utls

package tls

import (
	"net"
	"testing"
	"time"

	utls "github.com/metacubex/utls"
)

func TestRealityClientHelloKeepsX25519MLKEM768(t *testing.T) {
	conn := utls.UClient(nopRealityConn{}, &utls.Config{ServerName: "example.org"}, utls.HelloChrome_Auto)
	if err := prepareRealityClientHello(conn); err != nil {
		t.Fatal(err)
	}

	var supported, keyShare bool
	for _, extension := range conn.Extensions {
		switch extension := extension.(type) {
		case *utls.SupportedCurvesExtension:
			for _, curve := range extension.Curves {
				if curve == utls.X25519MLKEM768 {
					supported = true
				}
			}
		case *utls.KeyShareExtension:
			for _, share := range extension.KeyShares {
				if share.Group == utls.X25519MLKEM768 {
					keyShare = true
				}
			}
		}
	}
	if !supported {
		t.Fatal("supported_groups missing X25519MLKEM768")
	}
	if !keyShare {
		t.Fatal("key_share missing X25519MLKEM768")
	}
}

type nopRealityConn struct{}

func (nopRealityConn) Read([]byte) (int, error)         { return 0, nil }
func (nopRealityConn) Write([]byte) (int, error)        { return 0, nil }
func (nopRealityConn) Close() error                     { return nil }
func (nopRealityConn) LocalAddr() net.Addr              { return nil }
func (nopRealityConn) RemoteAddr() net.Addr             { return nil }
func (nopRealityConn) SetDeadline(time.Time) error      { return nil }
func (nopRealityConn) SetReadDeadline(time.Time) error  { return nil }
func (nopRealityConn) SetWriteDeadline(time.Time) error { return nil }
