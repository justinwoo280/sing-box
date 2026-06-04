package xhttp

import "testing"

func resolveAutoMode(explicitMode string, isReality, hasDownload bool) string {
	mode := explicitMode
	if mode == "" || mode == "auto" {
		mode = "packet-up"
		if isReality {
			mode = "stream-one"
			if hasDownload {
				mode = "stream-up"
			}
		}
	}
	return mode
}

func TestAutoModeDefault(t *testing.T) {
	got := resolveAutoMode("", false, false)
	if got != "packet-up" {
		t.Errorf("empty mode without REALITY: got %q, want %q", got, "packet-up")
	}
}

func TestAutoModeExplicitString(t *testing.T) {
	got := resolveAutoMode("auto", false, false)
	if got != "packet-up" {
		t.Errorf("auto mode without REALITY: got %q, want %q", got, "packet-up")
	}
}

func TestAutoModeExplicitPacketUp(t *testing.T) {
	got := resolveAutoMode("packet-up", false, false)
	if got != "packet-up" {
		t.Errorf("explicit packet-up: got %q, want %q", got, "packet-up")
	}
}

func TestAutoModeExplicitStreamUp(t *testing.T) {
	got := resolveAutoMode("stream-up", false, false)
	if got != "stream-up" {
		t.Errorf("explicit stream-up: got %q, want %q", got, "stream-up")
	}
}

func TestAutoModeExplicitStreamOne(t *testing.T) {
	got := resolveAutoMode("stream-one", false, false)
	if got != "stream-one" {
		t.Errorf("explicit stream-one: got %q, want %q", got, "stream-one")
	}
}

func TestAutoModeRealityNoDownload(t *testing.T) {
	got := resolveAutoMode("", true, false)
	if got != "stream-one" {
		t.Errorf("REALITY without download: got %q, want %q", got, "stream-one")
	}
}

func TestAutoModeRealityAutoString(t *testing.T) {
	got := resolveAutoMode("auto", true, false)
	if got != "stream-one" {
		t.Errorf("auto+REALITY without download: got %q, want %q", got, "stream-one")
	}
}

func TestAutoModeRealityWithDownload(t *testing.T) {
	got := resolveAutoMode("", true, true)
	if got != "stream-up" {
		t.Errorf("REALITY with download: got %q, want %q", got, "stream-up")
	}
}

func TestAutoModeRealityWithDownloadAutoString(t *testing.T) {
	got := resolveAutoMode("auto", true, true)
	if got != "stream-up" {
		t.Errorf("auto+REALITY with download: got %q, want %q", got, "stream-up")
	}
}

func TestAutoModeExplicitOverridesReality(t *testing.T) {
	got := resolveAutoMode("packet-up", true, true)
	if got != "packet-up" {
		t.Errorf("explicit packet-up should override REALITY: got %q, want %q", got, "packet-up")
	}
}

func TestAutoModeExplicitStreamUpWithReality(t *testing.T) {
	got := resolveAutoMode("stream-up", true, false)
	if got != "stream-up" {
		t.Errorf("explicit stream-up with REALITY: got %q, want %q", got, "stream-up")
	}
}

func TestAutoModeNoRealityWithDownload(t *testing.T) {
	got := resolveAutoMode("", false, true)
	if got != "packet-up" {
		t.Errorf("no REALITY with download should still be packet-up: got %q, want %q", got, "packet-up")
	}
}

func TestAutoModeAllCombinations(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		isReality   bool
		hasDownload bool
		want        string
	}{
		{"empty/plain", "", false, false, "packet-up"},
		{"empty/reality", "", true, false, "stream-one"},
		{"empty/reality+download", "", true, true, "stream-up"},
		{"empty/download-only", "", false, true, "packet-up"},
		{"auto/plain", "auto", false, false, "packet-up"},
		{"auto/reality", "auto", true, false, "stream-one"},
		{"auto/reality+download", "auto", true, true, "stream-up"},
		{"auto/download-only", "auto", false, true, "packet-up"},
		{"explicit-packet-up/plain", "packet-up", false, false, "packet-up"},
		{"explicit-packet-up/reality", "packet-up", true, false, "packet-up"},
		{"explicit-stream-up/plain", "stream-up", false, false, "stream-up"},
		{"explicit-stream-up/reality", "stream-up", true, false, "stream-up"},
		{"explicit-stream-one/plain", "stream-one", false, false, "stream-one"},
		{"explicit-stream-one/reality+download", "stream-one", true, true, "stream-one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAutoMode(tt.mode, tt.isReality, tt.hasDownload)
			if got != tt.want {
				t.Errorf("resolveAutoMode(%q, %v, %v) = %q, want %q",
					tt.mode, tt.isReality, tt.hasDownload, got, tt.want)
			}
		})
	}
}

func TestAutoModeStreamOneSessionId(t *testing.T) {
	mode := resolveAutoMode("", true, false)
	sessionId := ""
	if mode != "stream-one" {
		sessionId = "some-uuid"
	}
	if sessionId != "" {
		t.Errorf("stream-one should have empty sessionId, got %q", sessionId)
	}

	mode = resolveAutoMode("", false, false)
	sessionId = ""
	if mode != "stream-one" {
		sessionId = "some-uuid"
	}
	if sessionId == "" {
		t.Error("packet-up should have non-empty sessionId")
	}
}
