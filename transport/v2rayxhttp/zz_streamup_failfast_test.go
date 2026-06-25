package xhttp

import (
	"context"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestStreamUp_H11_FailFast(t *testing.T) {
	c := &Client{
		options: &option.V2RayXHTTPOptions{
			V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: "stream-up"},
		},
		httpVersion: "1.1",
	}
	_, err := c.DialContext(context.Background())
	if err == nil {
		t.Fatal("DialContext returned nil error for stream-up over HTTP/1.1")
	}
	if !strings.Contains(err.Error(), "stream-up requires HTTP/2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamUp_AutoResolvesToStreamUp_H11_FailFast(t *testing.T) {
	c := &Client{
		options:     &option.V2RayXHTTPOptions{},
		isReality:   true,
		hasDownload: true,
		httpVersion: "1.1",
	}
	_, err := c.DialContext(context.Background())
	if err == nil {
		t.Fatal("DialContext returned nil error for auto stream-up over HTTP/1.1")
	}
	if !strings.Contains(err.Error(), "stream-up requires HTTP/2") {
		t.Fatalf("unexpected error: %v", err)
	}
}
