package option

import (
	"encoding/json"
	"testing"
)

func TestV2RayXHTTPRangeJSONCompatibility(t *testing.T) {
	for _, test := range []struct {
		input string
		want  V2RayXHTTPRange
		json  string
	}{
		{`"100-1000"`, V2RayXHTTPRange{100, 1000}, `"100-1000"`},
		{`{"from":100,"to":1000}`, V2RayXHTTPRange{100, 1000}, `"100-1000"`},
		{`" 100 - 1000 "`, V2RayXHTTPRange{100, 1000}, `"100-1000"`},
		{`100`, V2RayXHTTPRange{100, 100}, `"100-100"`},
		{`"100"`, V2RayXHTTPRange{100, 100}, `"100-100"`},
		{`2147483647`, V2RayXHTTPRange{2147483647, 2147483647}, `"2147483647-2147483647"`},
		{`0`, V2RayXHTTPRange{}, `"0-0"`},
		{`null`, V2RayXHTTPRange{}, `"0-0"`},
	} {
		var r V2RayXHTTPRange
		if err := json.Unmarshal([]byte(test.input), &r); err != nil {
			t.Fatalf("unmarshal %s: %v", test.input, err)
		}
		if r != test.want {
			t.Fatalf("%s: range = %+v, want %+v", test.input, r, test.want)
		}
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != test.json {
			t.Fatalf("marshal = %s", encoded)
		}
	}
}

func TestV2RayXHTTPRangeRejectsInvalidJSON(t *testing.T) {
	for _, input := range []string{
		`-1`, `"-1"`, `"0--1"`, `{"from":-1,"to":3}`, `"2-1"`, `{"from":2,"to":1}`,
		`2147483648`, `"2147483648"`, `"1-2147483648"`, `{"from":1,"to":2147483648}`,
		`1.5`, `true`, `[]`, `"1-"`, `"1-2-3"`, `{"form":100,"to":1000}`,
	} {
		var r V2RayXHTTPRange
		if err := json.Unmarshal([]byte(input), &r); err == nil {
			t.Errorf("accepted invalid range %s", input)
		}
	}
}

func TestV2RayXHTTPDownloadJSONKeepsExplicitOverrides(t *testing.T) {
	var original V2RayTransportOptions
	err := json.Unmarshal([]byte(`{"type":"xhttp","download":{
		"headers":{}, "xmux":{}, "x_padding_obfs_mode":false, "x_padding_bytes":0,
		"session_key":"", "tls":{"enabled":false}
	}}`), &original)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded V2RayTransportOptions
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	dl := decoded.XHTTPOptions.Download
	if dl == nil || dl.Headers == nil || len(dl.Headers) != 0 || dl.Xmux == nil ||
		dl.XPaddingObfsMode == nil || *dl.XPaddingObfsMode ||
		dl.XPaddingBytes == nil || *dl.XPaddingBytes != (V2RayXHTTPRange{}) ||
		dl.SessionKey == nil || *dl.SessionKey != "" || dl.TLS == nil || dl.TLS.Enabled {
		t.Fatalf("lost explicit overrides: %s", encoded)
	}
	if dl.XPaddingPlacement != nil || dl.SessionPlacement != nil {
		t.Fatalf("invented values for omitted fields: %s", encoded)
	}
}

func TestV2RayXHTTPLegacyFormattedDownload(t *testing.T) {
	var transport V2RayTransportOptions
	err := json.Unmarshal([]byte(`{"type":"xhttp","x_padding_bytes":"32-64","download":{
		"server":"127.0.0.1","server_port":80,"mode":"","path":"/download","x_padding_bytes":"0-0",
		"sc_max_each_post_bytes":"0-0","sc_min_posts_interval_ms":"0-0","sc_stream_up_server_secs":"0-0","xmux":null
	}}`), &transport)
	if err != nil {
		t.Fatal(err)
	}
	download := transport.XHTTPOptions.Download
	if download == nil || download.Path != "/download" || download.XPaddingBytes != nil {
		t.Fatalf("legacy download should inherit padding: %+v", download)
	}
}
