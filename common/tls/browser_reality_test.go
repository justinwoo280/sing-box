package tls

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
)

func TestBrowserRealityConfiguration(t *testing.T) {
	key := [32]byte{9}
	newOptions := func() ClientOptions {
		return ClientOptions{Context: context.Background(), Logger: logger.NOP(), ServerAddress: "reality.test", Browser: true,
			Options: option.OutboundTLSOptions{Enabled: true, Reality: &option.OutboundRealityOptions{
				Enabled: true, PublicKey: base64.RawURLEncoding.EncodeToString(key[:]), ShortID: "0102",
			}}}
	}
	config, err := NewClientWithOptions(newOptions())
	if err != nil {
		t.Fatal(err)
	}
	c := config.(*browserRealityConfig)
	exported, err := c.BrowserTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if exported.Reality.PublicKey != key || exported.Reality.ShortID != [8]byte{1, 2} {
		t.Fatal("credentials changed")
	}
	if _, err := c.STDConfig(); err == nil {
		t.Fatal("REALITY allowed ordinary Go TLS")
	}
	if _, err := c.Client(nil); err == nil {
		t.Fatal("REALITY allowed ordinary Go connection")
	}
	exported.Reality.ShortID[0] = 99
	clone := c.Clone().(*browserRealityConfig)
	clone.SetServerName("another.test")
	if c.ServerName() != "reality.test" || clone.reality.ShortID[0] != 1 {
		t.Fatal("clone/export shares mutable config")
	}
	for _, test := range []struct {
		name, want string
		change     func(*option.OutboundTLSOptions)
	}{
		{"key", "public_key", func(o *option.OutboundTLSOptions) { o.Reality.PublicKey = "bad" }},
		{"short-id", "short_id", func(o *option.OutboundTLSOptions) { o.Reality.ShortID = "010203040506070809" }},
		{"ech", "ECH", func(o *option.OutboundTLSOptions) { o.ECH = &option.OutboundECHOptions{Enabled: true} }},
		{"insecure", "insecure", func(o *option.OutboundTLSOptions) { o.Insecure = true }},
		{"curves", "curve", func(o *option.OutboundTLSOptions) { o.CurvePreferences = []option.CurvePreference{29} }},
		{"fingerprint", "fingerprint", func(o *option.OutboundTLSOptions) {
			o.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "firefox"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := newOptions()
			test.change(&o.Options)
			if _, err := NewClientWithOptions(o); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
