package v2raygrpclite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/transport/v2raygrpc"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type countedBody struct{ closed atomic.Int32 }

func (*countedBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *countedBody) Close() error           { b.closed.Add(1); return nil }

func TestLateGunConnCloseBeforeSetup(t *testing.T) {
	conn := newLateGunConn(io.Discard)
	conn.Close()
	body := new(countedBody)
	conn.setup(body, nil)
	if body.closed.Load() != 1 {
		t.Fatal("response arriving after Close was not released")
	}
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Read after Close: %v", err)
	}
}

func gunFrame(t *testing.T, data []byte) []byte {
	t.Helper()
	message, err := proto.Marshal(&v2raygrpc.Hunk{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 5, 5+len(message))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(message)))
	return append(frame, message...)
}

func TestGunConnEmptyMessage(t *testing.T) {
	input := append(gunFrame(t, nil), gunFrame(t, []byte("pong"))...)
	conn := newGunConn(bytes.NewReader(input), io.Discard, nil)
	data, err := io.ReadAll(conn)
	if err != nil || string(data) != "pong" {
		t.Fatalf("empty message followed by data: %q, %v", data, err)
	}
}

func TestGunConnMessageBoundaries(t *testing.T) {
	input := append(gunFrame(t, []byte("ping")), gunFrame(t, []byte("pong"))...)
	conn := newGunConn(bytes.NewReader(input), io.Discard, nil)
	var output bytes.Buffer
	if _, err := io.CopyBuffer(struct{ io.Writer }{&output}, conn, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if output.String() != "pingpong" {
		t.Fatalf("message boundaries: %q", output.Bytes())
	}
}

func TestGunConnUnknownAndRepeatedFields(t *testing.T) {
	message := protowire.AppendTag(nil, 2, protowire.VarintType)
	message = protowire.AppendVarint(message, 42)
	message = protowire.AppendTag(message, 1, protowire.BytesType)
	message = protowire.AppendBytes(message, []byte("ignored"))
	message = protowire.AppendTag(message, 1, protowire.BytesType)
	message = protowire.AppendBytes(message, []byte("payload"))
	frame := make([]byte, 5, 5+len(message))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(message)))
	conn := newGunConn(bytes.NewReader(append(frame, message...)), io.Discard, nil)
	data, err := io.ReadAll(conn)
	if err != nil || string(data) != "payload" {
		t.Fatalf("protobuf field handling: %q, %v", data, err)
	}
}

func TestGunConnInvalidMessage(t *testing.T) {
	for _, input := range [][]byte{
		{1, 0, 0, 0, 0},
		{0, 0, 0, 0, 2, 0x0a, 10},
		{0, 0, 0x40, 0, 1},
		{0, 0, 0, 0, 2, 0x0a},
	} {
		conn := newGunConn(bytes.NewReader(input), io.Discard, nil)
		if _, err := conn.Read(make([]byte, 1)); err == nil || err == io.EOF {
			t.Fatalf("accepted malformed frame %x: %v", input, err)
		}
	}
}

func TestLateGunConnCloseUnblocksRead(t *testing.T) {
	conn := newLateGunConn(io.Discard)
	done := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); done <- err }()
	conn.Close()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Read after Close: %v", err)
		}
	case <-time.After(time.Second):
		conn.setup(nil, net.ErrClosed)
		<-done
		t.Fatal("Close left Read waiting for response headers")
	}
}

func TestLateGunConnConcurrentSetupReadClose(t *testing.T) {
	for range 64 {
		conn := newLateGunConn(io.Discard)
		body := new(countedBody)
		start := make(chan struct{})
		var group sync.WaitGroup
		group.Go(func() { <-start; conn.setup(body, nil) })
		group.Go(func() { <-start; conn.Read(make([]byte, 1)) })
		group.Go(func() { <-start; conn.Close() })
		close(start)
		group.Wait()
		if body.closed.Load() != 1 {
			t.Fatal("concurrent setup/Close leaked response body")
		}
	}
}
