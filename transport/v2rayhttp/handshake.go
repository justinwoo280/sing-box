package v2rayhttp

import (
	"context"
	"net"
)

// HandshakeContext interrupts a stream handshake when its caller is canceled.
// The cancellation callback is stopped before a successful connection is returned.
func HandshakeContext(ctx context.Context, conn net.Conn, handshake func() error) error {
	if err := ctx.Err(); err != nil {
		conn.Close()
		return err
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		conn.Close()
		close(canceled)
	})
	err := handshake()
	if !stop() {
		<-canceled
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
	}
	return err
}
