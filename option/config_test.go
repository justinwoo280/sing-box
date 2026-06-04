package option

import (
	"testing"

	Xbadoption "github.com/sagernet/sing-box/common/xray/json/badoption"
)

func TestGetNormalizedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty defaults to root", "", "/"},
		{"already has slash", "/xhttp", "/xhttp/"},
		{"missing slash prepended", "xhttp", "/xhttp/"},
		{"query stripped from path", "/xhttp?foo=bar", "/xhttp/"},
		{"no slash with query", "xhttp?foo=bar", "/xhttp/"},
		{"nested path", "/a/b/c", "/a/b/c/"},
		{"nested path with query", "/a/b/c?q=1", "/a/b/c/"},
		{"trailing slash preserved", "/xhttp/", "/xhttp/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &V2RayXHTTPBaseOptions{Path: tt.path}
			got := opts.GetNormalizedPath()
			if got != tt.want {
				t.Errorf("GetNormalizedPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetNormalizedQuery(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"no query", "/xhttp", ""},
		{"with query", "/xhttp?foo=bar", "foo=bar"},
		{"empty path with query", "?foo=bar", "foo=bar"},
		{"empty", "", ""},
		{"query with multiple params", "/xhttp?a=1&b=2", "a=1&b=2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &V2RayXHTTPBaseOptions{Path: tt.path}
			got := opts.GetNormalizedQuery()
			if got != tt.want {
				t.Errorf("GetNormalizedQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetNormalizedXPaddingBytes(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{}
		r := opts.GetNormalizedXPaddingBytes()
		if r.From != 100 || r.To != 1000 {
			t.Errorf("default: got {%d,%d}, want {100,1000}", r.From, r.To)
		}
	})
	t.Run("custom value", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{
			XPaddingBytes: Xbadoption.Range{From: 50, To: 200},
		}
		r := opts.GetNormalizedXPaddingBytes()
		if r.From != 50 || r.To != 200 {
			t.Errorf("custom: got {%d,%d}, want {50,200}", r.From, r.To)
		}
	})
}

func TestGetNormalizedScMaxEachPostBytes(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{}
		r := opts.GetNormalizedScMaxEachPostBytes()
		if r.From != 1000000 || r.To != 1000000 {
			t.Errorf("default: got {%d,%d}, want {1000000,1000000}", r.From, r.To)
		}
	})
	t.Run("custom value", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{
			ScMaxEachPostBytes: Xbadoption.Range{From: 500, To: 2000},
		}
		r := opts.GetNormalizedScMaxEachPostBytes()
		if r.From != 500 || r.To != 2000 {
			t.Errorf("custom: got {%d,%d}, want {500,2000}", r.From, r.To)
		}
	})
}

func TestGetNormalizedScMinPostsIntervalMs(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{}
		r := opts.GetNormalizedScMinPostsIntervalMs()
		if r.From != 30 || r.To != 30 {
			t.Errorf("default: got {%d,%d}, want {30,30}", r.From, r.To)
		}
	})
	t.Run("custom value", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{
			ScMinPostsIntervalMs: Xbadoption.Range{From: 10, To: 50},
		}
		r := opts.GetNormalizedScMinPostsIntervalMs()
		if r.From != 10 || r.To != 50 {
			t.Errorf("custom: got {%d,%d}, want {10,50}", r.From, r.To)
		}
	})
}

func TestGetNormalizedScMaxBufferedPosts(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{}
		got := opts.GetNormalizedScMaxBufferedPosts()
		if got != 30 {
			t.Errorf("default: got %d, want 30", got)
		}
	})
	t.Run("custom value", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{ScMaxBufferedPosts: 100}
		got := opts.GetNormalizedScMaxBufferedPosts()
		if got != 100 {
			t.Errorf("custom: got %d, want 100", got)
		}
	})
}

func TestGetNormalizedScStreamUpServerSecs(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{}
		r := opts.GetNormalizedScStreamUpServerSecs()
		if r.From != 20 || r.To != 80 {
			t.Errorf("default: got {%d,%d}, want {20,80}", r.From, r.To)
		}
	})
	t.Run("custom value", func(t *testing.T) {
		opts := &V2RayXHTTPBaseOptions{
			ScStreamUpServerSecs: Xbadoption.Range{From: 5, To: 15},
		}
		r := opts.GetNormalizedScStreamUpServerSecs()
		if r.From != 5 || r.To != 15 {
			t.Errorf("custom: got {%d,%d}, want {5,15}", r.From, r.To)
		}
	})
}

func TestRangeRand(t *testing.T) {
	r := Xbadoption.Range{From: 100, To: 100}
	got := r.Rand()
	if got != 100 {
		t.Errorf("same from/to: got %d, want 100", got)
	}

	r2 := Xbadoption.Range{From: 10, To: 20}
	for i := 0; i < 100; i++ {
		v := r2.Rand()
		if v < 10 || v > 20 {
			t.Fatalf("Rand() = %d, out of range [10,20]", v)
		}
	}
}

func TestV2RayXHTTPOptionsDownload(t *testing.T) {
	opts := V2RayXHTTPOptions{
		V2RayXHTTPBaseOptions: V2RayXHTTPBaseOptions{
			Mode: "stream-up",
			Path: "/xhttp",
		},
		Download: &V2RayXHTTPDownloadOptions{
			V2RayXHTTPBaseOptions: V2RayXHTTPBaseOptions{
				Path: "/download",
			},
		},
	}
	if opts.Download == nil {
		t.Fatal("Download should not be nil")
	}
	if opts.Download.Path != "/download" {
		t.Errorf("Download.Path = %q, want /download", opts.Download.Path)
	}
	if opts.Mode != "stream-up" {
		t.Errorf("Mode = %q, want stream-up", opts.Mode)
	}
}
