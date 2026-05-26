// MIT License
//
// Copyright (c) 2026 Gedis Authors
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package gedis

import (
	"encoding/binary"
	"math"
)

const (
	arenaDefaultPage = 4096
	arenaHeaderSize  = 4
)

type Arena struct {
	buf  []byte
	off  int
	free []int
	page int
}

func NewArena(initialSize int) *Arena {
	if initialSize <= 0 {
		initialSize = arenaDefaultPage
	}
	return &Arena{
		buf:  make([]byte, initialSize),
		off:  0,
		free: make([]int, 0),
		page: arenaDefaultPage,
	}
}

func (a *Arena) Alloc(size int) int {
	if size <= 0 {
		return 0
	}

	for i, freeOff := range a.free {
		blockSize := int(a.ReadUint32(freeOff))
		if blockSize >= size {
			a.free = append(a.free[:i], a.free[i+1:]...)
			a.WriteUint32(freeOff, uint32(size))
			return freeOff + arenaHeaderSize
		}
	}

	needed := size + arenaHeaderSize
	if a.off+needed > len(a.buf) {
		a.grow(needed)
	}

	allocOff := a.off
	a.WriteUint32(allocOff, uint32(size))
	a.off += needed
	return allocOff + arenaHeaderSize
}

func (a *Arena) AllocString(s string) int {
	b := []byte(s)
	off := a.Alloc(len(b))
	if off != 0 {
		a.WriteBytes(off, b)
	}
	return off
}

func (a *Arena) AllocBytes(b []byte) int {
	off := a.Alloc(len(b))
	if off != 0 {
		a.WriteBytes(off, b)
	}
	return off
}

func (a *Arena) Free(dataOff int) {
	if dataOff == 0 {
		return
	}
	headerOff := dataOff - arenaHeaderSize
	a.free = append(a.free, headerOff)
}

func (a *Arena) WriteUint32(off int, v uint32) {
	binary.LittleEndian.PutUint32(a.buf[off:off+4], v)
}

func (a *Arena) ReadUint32(off int) uint32 {
	return binary.LittleEndian.Uint32(a.buf[off : off+4])
}

func (a *Arena) WriteUint16(off int, v uint16) {
	binary.LittleEndian.PutUint16(a.buf[off:off+2], v)
}

func (a *Arena) ReadUint16(off int) uint16 {
	return binary.LittleEndian.Uint16(a.buf[off : off+2])
}

func (a *Arena) WriteUint64(off int, v uint64) {
	binary.LittleEndian.PutUint64(a.buf[off:off+8], v)
}

func (a *Arena) ReadUint64(off int) uint64 {
	return binary.LittleEndian.Uint64(a.buf[off : off+8])
}

func (a *Arena) WriteFloat64(off int, v float64) {
	a.WriteUint64(off, math.Float64bits(v))
}

func (a *Arena) ReadFloat64(off int) float64 {
	return math.Float64frombits(a.ReadUint64(off))
}

func (a *Arena) WriteByte(off int, b byte) {
	a.buf[off] = b
}

func (a *Arena) ReadByte(off int) byte {
	return a.buf[off]
}

func (a *Arena) WriteBytes(off int, data []byte) {
	copy(a.buf[off:], data)
}

func (a *Arena) ReadBytes(off int, size int) []byte {
	if size <= 0 {
		return nil
	}
	result := make([]byte, size)
	copy(result, a.buf[off:off+size])
	return result
}

func (a *Arena) GetSlice(off int, size int) []byte {
	return a.buf[off : off+size]
}

func (a *Arena) ReadString(off int) string {
	size := int(a.ReadUint32(off - arenaHeaderSize))
	return string(a.buf[off : off+size])
}

func (a *Arena) SizeAt(off int) int {
	return int(a.ReadUint32(off - arenaHeaderSize))
}

func (a *Arena) Reset() {
	a.off = 0
	a.free = a.free[:0]
	a.buf = make([]byte, a.page)
}

func (a *Arena) grow(needed int) {
	newSize := len(a.buf) * 2
	for newSize < a.off+needed {
		newSize *= 2
	}
	newBuf := make([]byte, newSize)
	copy(newBuf, a.buf[:a.off])
	a.buf = newBuf
}
