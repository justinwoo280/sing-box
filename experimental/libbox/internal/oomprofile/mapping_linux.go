//go:build linux

package oomprofile

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

func (b *profileBuilder) readMapping() {
	data, _ := os.ReadFile("/proc/self/maps")
	parseProcSelfMaps(data, func(lo, hi, offset uint64, file, buildID string) {
		b.addMappingEntry(lo, hi, offset, file, buildID, false)
	})
	if len(b.mem) == 0 {
		b.addMappingEntry(0, 0, 0, "", "", true)
	}
}

// parseProcSelfMaps parses Linux's /proc/<pid>/maps format. This is kept
// locally instead of linking to runtime/pprof's private implementation: the
// latter is an unstable implementation detail and stopped being linkable
// with newer Go toolchains.
func parseProcSelfMaps(data []byte, addMapping func(lo, hi, offset uint64, file, buildID string)) {
	var line []byte
	next := func() []byte {
		var field []byte
		field, line, _ = bytes.Cut(line, []byte(" "))
		line = bytes.TrimLeft(line, " ")
		return field
	}

	for len(data) > 0 {
		line, data, _ = bytes.Cut(data, []byte("\n"))
		addr := next()
		loStr, hiStr, ok := strings.Cut(string(addr), "-")
		if !ok {
			continue
		}
		lo, err := strconv.ParseUint(loStr, 16, 64)
		if err != nil {
			continue
		}
		hi, err := strconv.ParseUint(hiStr, 16, 64)
		if err != nil {
			continue
		}
		perm := next()
		if len(perm) < 3 || perm[2] != 'x' {
			continue
		}
		offset, err := strconv.ParseUint(string(next()), 16, 64)
		if err != nil {
			continue
		}
		next()          // dev
		inode := next() // inode
		if line == nil {
			continue
		}
		file := string(line)
		const deleted = " (deleted)"
		if strings.HasSuffix(file, deleted) {
			file = strings.TrimSuffix(file, deleted)
		}
		if len(inode) == 1 && inode[0] == '0' && file == "" {
			continue
		}
		buildID, _ := elfBuildID(file)
		addMapping(lo, hi, offset, file, buildID)
	}
}
