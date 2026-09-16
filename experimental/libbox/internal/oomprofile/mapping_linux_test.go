//go:build linux

package oomprofile

import "testing"

func TestParseProcSelfMaps(t *testing.T) {
	data := []byte("00400000-00401000 r-xp 00001000 08:01 123 /tmp/example (deleted)\n" +
		"00401000-00402000 rw-p 00002000 08:01 123 /tmp/example\n" +
		"7fff0000-7fff1000 r-xp 00000000 00:00 0 [vdso]\n")

	var mappings []memMap
	parseProcSelfMaps(data, func(lo, hi, offset uint64, file, buildID string) {
		mappings = append(mappings, memMap{start: uintptr(lo), end: uintptr(hi), offset: offset, file: file, buildID: buildID})
	})

	if len(mappings) != 2 {
		t.Fatalf("got %d mappings, want 2", len(mappings))
	}
	if got := mappings[0]; got.start != 0x400000 || got.end != 0x401000 || got.offset != 0x1000 || got.file != "/tmp/example" {
		t.Fatalf("unexpected executable mapping: %+v", got)
	}
	if got := mappings[1]; got.file != "[vdso]" {
		t.Fatalf("unexpected vdso mapping: %+v", got)
	}
}
