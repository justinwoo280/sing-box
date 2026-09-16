// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build linux

package oomprofile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

var (
	errBadELF    = errors.New("malformed ELF binary")
	errNoBuildID = errors.New("no NT_GNU_BUILD_ID found in ELF binary")
)

// elfBuildID returns the GNU build ID without importing debug/elf and its
// transitive dependencies. It mirrors the small reader used by runtime/pprof.
func elfBuildID(file string) (string, error) {
	buf := make([]byte, 256)
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err = f.ReadAt(buf[:64], 0); err != nil {
		return "", err
	}
	if buf[0] != 0x7f || buf[1] != 'E' || buf[2] != 'L' || buf[3] != 'F' {
		return "", errBadELF
	}

	var order binary.ByteOrder
	switch buf[5] {
	case 1:
		order = binary.LittleEndian
	case 2:
		order = binary.BigEndian
	default:
		return "", errBadELF
	}

	var shnum int
	var shoff, shentsize int64
	switch buf[4] {
	case 1:
		shoff = int64(order.Uint32(buf[32:]))
		shentsize = int64(order.Uint16(buf[46:]))
		if shentsize != 40 {
			return "", errBadELF
		}
		shnum = int(order.Uint16(buf[48:]))
	case 2:
		shoff = int64(order.Uint64(buf[40:]))
		shentsize = int64(order.Uint16(buf[58:]))
		if shentsize != 64 {
			return "", errBadELF
		}
		shnum = int(order.Uint16(buf[60:]))
	default:
		return "", errBadELF
	}

	for i := 0; i < shnum; i++ {
		if _, err = f.ReadAt(buf[:shentsize], shoff+int64(i)*shentsize); err != nil {
			return "", err
		}
		if order.Uint32(buf[4:]) != 7 { // SHT_NOTE
			continue
		}
		var off, size int64
		if shentsize == 40 {
			off = int64(order.Uint32(buf[16:]))
			size = int64(order.Uint32(buf[20:]))
		} else {
			off = int64(order.Uint64(buf[24:]))
			size = int64(order.Uint64(buf[32:]))
		}
		size += off
		for off < size {
			if _, err = f.ReadAt(buf[:16], off); err != nil {
				return "", err
			}
			nameSize := int(order.Uint32(buf[0:]))
			descSize := int(order.Uint32(buf[4:]))
			noteType := int(order.Uint32(buf[8:]))
			descOff := off + int64(12+(nameSize+3)&^3)
			off = descOff + int64((descSize+3)&^3)
			if nameSize != 4 || noteType != 3 || string(buf[12:16]) != "GNU\x00" {
				continue
			}
			if descSize > len(buf) {
				return "", errBadELF
			}
			if _, err = f.ReadAt(buf[:descSize], descOff); err != nil {
				return "", err
			}
			return fmt.Sprintf("%x", buf[:descSize]), nil
		}
	}
	return "", errNoBuildID
}
