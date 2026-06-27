package xhttp

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestUploadQueue_DeadlineExpires(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()

	if err := q.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	buf := make([]byte, 16)
	start := time.Now()
	_, err := q.Read(buf)
	elapsed := time.Since(start)

	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected os.ErrDeadlineExceeded, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("deadline did not fire promptly: %v", elapsed)
	}
}

func TestUploadQueue_DeadlinePastImmediate(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()

	if err := q.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	buf := make([]byte, 16)
	_, err := q.Read(buf)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected os.ErrDeadlineExceeded for past deadline, got %v", err)
	}
}

func TestUploadQueue_DeadlineCleared(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()

	if err := q.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := q.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := q.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Read returned with cleared deadline: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if err := q.Push(Packet{Payload: []byte("hi"), Seq: 0}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Read after data: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not return after data pushed")
	}
}

func TestUploadQueue_DeadlineThenDataStillReadable(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()

	if err := q.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	_, err := q.Read(buf)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}

	if err := q.Push(Packet{Payload: []byte("payload"), Seq: 0}); err != nil {
		t.Fatal(err)
	}
	n, err := q.Read(buf)
	if err != nil {
		t.Fatalf("Read after timeout: %v", err)
	}
	if string(buf[:n]) != "payload" {
		t.Fatalf("got %q, want payload", buf[:n])
	}
}

func TestSplitConn_SetReadDeadlinePropagatesToUploadQueue(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()
	c := &splitConn{reader: q, writer: nopWriteCloser{}}

	if err := c.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	buf := make([]byte, 16)
	_, err := c.Read(buf)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected deadline to propagate through splitConn, got %v", err)
	}
}

func TestSplitConn_SetDeadlinePropagatesToUploadQueue(t *testing.T) {
	q := NewUploadQueue(30)
	defer q.Close()
	c := &splitConn{reader: q, writer: nopWriteCloser{}}

	if err := c.SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	buf := make([]byte, 16)
	_, err := c.Read(buf)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected SetDeadline to propagate to read side, got %v", err)
	}
}

func TestSplitConn_DeadlineNoOpOnUnsupportedReader(t *testing.T) {
	r, w := io.Pipe()
	defer func() { _ = w.Close() }()
	c := &splitConn{reader: r, writer: nopWriteCloser{}}

	if err := c.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("SetReadDeadline on unsupported reader should be nil, got %v", err)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4)
		_, err := c.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("unsupported reader should remain blocked, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	_ = w.Close()
	<-done
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(b []byte) (int, error) { return len(b), nil }
func (nopWriteCloser) Close() error                { return nil }
