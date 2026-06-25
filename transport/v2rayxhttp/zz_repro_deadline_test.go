package xhttp

import (
	"io"
	"testing"
	"time"
)

// Repro for claim A1: splitConn.SetReadDeadline / SetDeadline are no-ops
// (the source has `// TODO cannot do anything useful` returning nil).
// A blocked Read must therefore NOT be interrupted by a deadline, which
// is exactly why an EWP handshake layered on top of an xhttp splitConn
// hangs forever instead of timing out.
func TestRepro_SplitConnDeadlineIsNoOp(t *testing.T) {
	r, w := io.Pipe()
	c := &splitConn{
		reader: r,
		writer: w,
	}

	// Set a deadline in the past. A real deadline implementation would
	// make the subsequent Read return immediately with a timeout error.
	past := time.Now().Add(-time.Second)
	if err := c.SetReadDeadline(past); err != nil {
		t.Fatalf("SetReadDeadline returned error: %v (expected nil no-op)", err)
	}
	if err := c.SetDeadline(past); err != nil {
		t.Fatalf("SetDeadline returned error: %v (expected nil no-op)", err)
	}

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 4)
		_, err := c.Read(buf)
		readDone <- err
	}()

	select {
	case err := <-readDone:
		t.Fatalf("Read returned before any data despite past deadline: err=%v — deadline is NOT a no-op?", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: the deadline did nothing, Read is still blocked.
	}

	// Unblock the goroutine so the test can finish cleanly.
	_ = w.Close()
	<-readDone
}
