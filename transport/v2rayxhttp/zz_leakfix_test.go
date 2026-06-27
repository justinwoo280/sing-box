package xhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"testing"

	"github.com/sagernet/sing-box/option"
)

// errCloseCloser is an io.ReadCloser/io.WriteCloser whose Close returns a
// distinct error, used to verify splitConn.Close surfaces reader errors
// (the pre-fix code returned the writer's error for the reader branch).
type errCloseCloser struct {
	closeErr error
}

func (e *errCloseCloser) Read([]byte) (int, error)  { return 0, io.EOF }
func (e *errCloseCloser) Write([]byte) (int, error) { return 0, nil }
func (e *errCloseCloser) Close() error              { return e.closeErr }

// Regression: splitConn.Close() must not panic when reader/writer are nil
// (which happens on the error paths of Client.DialContext, where OpenStream
// fails before assigning conn.reader).
func TestSplitConn_Close_NilSafe(t *testing.T) {
	c := &splitConn{} // both reader and writer nil
	if err := c.Close(); err != nil {
		t.Fatalf("Close with nil reader/writer should be nil, got: %v", err)
	}
}

// Regression: the reader-close branch used to `return err` (the writer's
// error) instead of the reader's error. With only the reader failing, the
// returned error must be the reader's error.
func TestSplitConn_Close_ReaderErrorReturned(t *testing.T) {
	readerErr := errors.New("reader boom")
	c := &splitConn{
		writer: &errCloseCloser{closeErr: nil},
		reader: &errCloseCloser{closeErr: readerErr},
	}
	err := c.Close()
	if err == nil {
		t.Fatal("expected reader error, got nil")
	}
	if !errors.Is(err, readerErr) {
		t.Fatalf("expected reader error %v, got %v", readerErr, err)
	}
}

// Regression: both reader and writer failing must surface a non-nil error
// (previously the writer error masked the reader error, and the err2 bug
// could even drop it).
func TestSplitConn_Close_BothErrors(t *testing.T) {
	c := &splitConn{
		writer: &errCloseCloser{closeErr: errors.New("writer boom")},
		reader: &errCloseCloser{closeErr: errors.New("reader boom")},
	}
	if err := c.Close(); err == nil {
		t.Fatal("expected combined error, got nil")
	}
}

// onClose must be invoked exactly once during Close.
func TestSplitConn_Close_OnCloseInvokedOnce(t *testing.T) {
	calls := 0
	c := &splitConn{onClose: func() { calls++ }}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if calls != 1 {
		t.Fatalf("onClose called %d times, want 1", calls)
	}
}

// failingDC implements DialerClient; OpenStream always fails.
type failingDC struct{}

func (f *failingDC) IsClosed() bool { return false }
func (f *failingDC) OpenStream(ctx context.Context, urlStr string, body io.Reader, uploadOnly bool) (io.ReadCloser, net.Addr, net.Addr, error) {
	return nil, nil, nil, errors.New("dial failed")
}
func (f *failingDC) PostPacket(ctx context.Context, urlStr string, body io.Reader, contentLength int64) error {
	return errors.New("not used")
}

// Regression for the DialContext error-path leak: when OpenStream fails,
// the xmux OpenUsage counter must be released so the underlying connection
// can be retired/reused. Pre-fix, the error path returned without calling
// the release helper, leaving OpenUsage pinned at 1 forever (a connection
// that could never be recycled => effective leak).
func TestDialContext_ErrorPathReleasesUsage_StreamOne(t *testing.T) {
	xm := &XmuxClient{leftUsage: -1}
	xm.LeftRequests.Store(1 << 30)
	xm.OpenUsage.Store(0)

	c := &Client{
		options:     &option.V2RayXHTTPOptions{},
		httpVersion: "2",
		getHTTPClient: func() (DialerClient, *XmuxClient) {
			return &failingDC{}, xm
		},
		getHTTPClient2: func() (DialerClient, *XmuxClient) {
			return &failingDC{}, xm
		},
		getRequestURL:  func(string) url.URL { return url.URL{} },
		getRequestURL2: func(string) url.URL { return url.URL{} },
	}

	_, err := c.DialContext(context.Background())
	if err == nil {
		t.Fatal("expected DialContext to fail")
	}
	if got := xm.OpenUsage.Load(); got != 0 {
		t.Fatalf("OpenUsage leaked: got %d, want 0 (error path did not release usage)", got)
	}
}

// Same regression for stream-down / packet-up path: the first OpenStream
// (stream-down GET) fails and must release usage.
func TestDialContext_ErrorPathReleasesUsage_PacketUp(t *testing.T) {
	xm := &XmuxClient{leftUsage: -1}
	xm.LeftRequests.Store(1 << 30)
	xm.OpenUsage.Store(0)

	opts := &option.V2RayXHTTPOptions{}
	opts.Mode = "packet-up"
	c := &Client{
		options:     opts,
		httpVersion: "2",
		getHTTPClient: func() (DialerClient, *XmuxClient) {
			return &failingDC{}, xm
		},
		getHTTPClient2: func() (DialerClient, *XmuxClient) {
			return &failingDC{}, xm
		},
		getRequestURL:  func(string) url.URL { return url.URL{} },
		getRequestURL2: func(string) url.URL { return url.URL{} },
	}

	_, err := c.DialContext(context.Background())
	if err == nil {
		t.Fatal("expected DialContext to fail")
	}
	if got := xm.OpenUsage.Load(); got != 0 {
		t.Fatalf("OpenUsage leaked: got %d, want 0", got)
	}
}
