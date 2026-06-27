package xhttp

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

func TestUploadQueueOrderedPush(t *testing.T) {
	q := NewUploadQueue(30)

	payloads := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte("foo"),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for i, expected := range payloads {
			n, err := q.Read(buf)
			if err != nil {
				t.Errorf("Read %d failed: %v", i, err)
				return
			}
			if !bytes.Equal(buf[:n], expected) {
				t.Errorf("Read %d: got %q, want %q", i, buf[:n], expected)
				return
			}
		}
	}()

	for i, p := range payloads {
		err := q.Push(Packet{Payload: p, Seq: uint64(i)})
		if err != nil {
			t.Fatalf("Push %d failed: %v", i, err)
		}
	}

	wg.Wait()
	q.Close()
}

func TestUploadQueueOutOfOrder(t *testing.T) {
	q := NewUploadQueue(30)

	expected := []string{"first", "second", "third"}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for i, want := range expected {
			n, err := q.Read(buf)
			if err != nil {
				t.Errorf("Read %d failed: %v", i, err)
				return
			}
			if string(buf[:n]) != want {
				t.Errorf("Read %d: got %q, want %q", i, buf[:n], want)
				return
			}
		}
	}()

	q.Push(Packet{Payload: []byte("first"), Seq: 0})
	q.Push(Packet{Payload: []byte("third"), Seq: 2})
	q.Push(Packet{Payload: []byte("second"), Seq: 1})

	wg.Wait()
	q.Close()
}

func TestUploadQueueReadEmptyClosed(t *testing.T) {
	q := NewUploadQueue(30)
	q.Close()

	buf := make([]byte, 1024)
	_, err := q.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF on empty closed queue, got: %v", err)
	}
}

func TestUploadQueuePushAfterClose(t *testing.T) {
	q := NewUploadQueue(30)
	q.Close()

	err := q.Push(Packet{Payload: []byte("nope"), Seq: 0})
	if err == nil {
		t.Fatal("expected error pushing to closed queue")
	}
}

type mockReadCloser struct {
	data   []byte
	offset int
	closed bool
}

func (m *mockReadCloser) Read(b []byte) (int, error) {
	if m.offset >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(b, m.data[m.offset:])
	m.offset += n
	return n, nil
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return nil
}

func TestUploadQueuePushReader(t *testing.T) {
	q := NewUploadQueue(30)

	reader := &mockReadCloser{data: []byte("stream data here")}
	err := q.Push(Packet{Reader: reader})
	if err != nil {
		t.Fatal(err)
	}

	err = q.Push(Packet{Reader: &mockReadCloser{data: []byte("dup")}})
	if err == nil {
		t.Fatal("expected error pushing second reader")
	}

	q.Close()

	buf := make([]byte, 1024)
	var result []byte
	for {
		n, err := q.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if string(result) != "stream data here" {
		t.Fatalf("got %q, want %q", result, "stream data here")
	}
}

func TestUploadQueuePartialRead(t *testing.T) {
	q := NewUploadQueue(30)
	payload := []byte("this is a longer payload that exceeds the small buffer")

	var wg sync.WaitGroup
	wg.Add(1)

	var result []byte
	var readErr error
	go func() {
		defer wg.Done()
		smallBuf := make([]byte, 10)
		for len(result) < len(payload) {
			n, err := q.Read(smallBuf)
			if n > 0 {
				result = append(result, smallBuf[:n]...)
			}
			if err != nil {
				readErr = err
				return
			}
		}
	}()

	q.Push(Packet{Payload: payload, Seq: 0})
	wg.Wait()
	q.Close()

	if readErr != nil {
		t.Fatalf("unexpected error: %v", readErr)
	}
	if !bytes.Equal(result, payload) {
		t.Fatalf("got %q, want %q", result, payload)
	}
}

func TestUploadQueueOverflow(t *testing.T) {
	q := NewUploadQueue(3)

	pushDone := make(chan struct{})
	go func() {
		defer close(pushDone)
		q.Push(Packet{Payload: []byte("seq0"), Seq: 0})
		for i := 2; i < 20; i++ {
			q.Push(Packet{Payload: []byte("gap"), Seq: uint64(i)})
		}
		q.Close()
	}()

	buf := make([]byte, 1024)
	readErr := make(chan error, 1)
	go func() {
		for {
			_, err := q.Read(buf)
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	select {
	case err := <-readErr:
		if err != io.EOF && err.Error() != "packet queue is too large" {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		q.Close()
		<-readErr
	}
	q.Close()
	<-pushDone
}

func TestUploadQueueConcurrentPushRead(t *testing.T) {
	q := NewUploadQueue(30)
	count := 20

	var wg sync.WaitGroup
	wg.Add(1)

	readCount := 0
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := q.Read(buf)
			if n > 0 {
				readCount++
			}
			if readCount >= count {
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for i := 0; i < count; i++ {
		payload := []byte("packet-" + string(rune('A'+i)))
		q.Push(Packet{Payload: payload, Seq: uint64(i)})
	}

	wg.Wait()
	q.Close()

	if readCount != count {
		t.Fatalf("expected %d reads, got %d", count, readCount)
	}
}

func TestUploadQueueDuplicateReaderPush(t *testing.T) {
	q := NewUploadQueue(30)

	r1 := &mockReadCloser{data: []byte("first")}
	err := q.Push(Packet{Reader: r1})
	if err != nil {
		t.Fatal(err)
	}

	r2 := &mockReadCloser{data: []byte("second")}
	err = q.Push(Packet{Reader: r2})
	if err == nil {
		t.Fatal("expected error pushing second reader")
	}
	q.Close()
}

func TestUploadQueueReadZeroPayload(t *testing.T) {
	q := NewUploadQueue(30)

	var wg sync.WaitGroup
	wg.Add(1)

	var readErr error
	var firstN int
	var secondData string
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)

		n, err := q.Read(buf)
		if err != nil {
			readErr = err
			return
		}
		firstN = n

		n, err = q.Read(buf)
		if err != nil {
			readErr = err
			return
		}
		secondData = string(buf[:n])
	}()

	q.Push(Packet{Payload: []byte(""), Seq: 0})
	q.Push(Packet{Payload: []byte("after"), Seq: 1})

	wg.Wait()
	q.Close()

	if readErr != nil {
		t.Fatalf("Read failed: %v", readErr)
	}
	if firstN != 0 {
		t.Fatalf("expected 0 bytes for empty payload, got %d", firstN)
	}
	if secondData != "after" {
		t.Fatalf("got %q, want %q", secondData, "after")
	}
}

func TestUploadQueueCloseWhileReading(t *testing.T) {
	q := NewUploadQueue(30)

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		_, err := q.Read(buf)
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	q.Close()

	select {
	case err := <-errCh:
		if err != io.EOF {
			t.Fatalf("expected EOF after close, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after Close")
	}
}
