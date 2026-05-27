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

// Arena 是一个零 GC 压力的内存分配器。所有数据存储在单块 []byte 缓冲区中，
// 内部结构使用整数偏移量相互引用，Go GC 无需扫描 Arena 内部数据。
//
// 每次分配的块格式:
//
//	+------------+----------------------------------+
//	| size (4B)  |  data (variable)                 |
//	+------------+----------------------------------+
//	 ↑ header     ↑ dataOff (返回给调用方)
const (
	arenaDefaultPage = 4096
	arenaHeaderSize  = 4 // 每个分配块前 4 字节存数据大小
)

// Arena 内存分配器
type Arena struct {
	buf  []byte // 底层字节缓冲区
	off  int    // 当前分配位置
	free []int  // 空闲块列表，每个元素是已释放块的 header 偏移
	page int    // 默认页大小
}

// NewArena 创建一个新的 Arena 分配器。
// 若 initialSize <= 0，使用默认页大小 4096。
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

// Alloc 分配 size 字节的数据块，返回 data 区域的偏移量。
// 优先从空闲列表中复用已释放的块；若无合适块则在缓冲区末尾分配。
func (a *Arena) Alloc(size int) int {
	if size <= 0 {
		return 0
	}

	// 遍历空闲列表，寻找足够大的块
	for i, freeOff := range a.free {
		blockSize := int(a.ReadUint32(freeOff))
		if blockSize >= size {
			a.free = append(a.free[:i], a.free[i+1:]...)
			a.WriteUint32(freeOff, uint32(size))
			return freeOff + arenaHeaderSize
		}
	}

	// 无空闲块，在缓冲区末尾分配
	needed := size + arenaHeaderSize
	if a.off+needed > len(a.buf) {
		a.grow(needed)
	}

	allocOff := a.off
	a.WriteUint32(allocOff, uint32(size))
	a.off += needed
	return allocOff + arenaHeaderSize
}

// AllocString 分配并写入字符串数据。
func (a *Arena) AllocString(s string) int {
	b := []byte(s)
	off := a.Alloc(len(b))
	if off != 0 {
		a.WriteBytes(off, b)
	}
	return off
}

// AllocBytes 分配并写入字节切片数据。
func (a *Arena) AllocBytes(b []byte) int {
	off := a.Alloc(len(b))
	if off != 0 {
		a.WriteBytes(off, b)
	}
	return off
}

// Free 释放 dataOff 指向的内存块，将其加入空闲列表供后续复用。
func (a *Arena) Free(dataOff int) {
	if dataOff == 0 {
		return
	}
	headerOff := dataOff - arenaHeaderSize
	a.free = append(a.free, headerOff)
}

// WriteUint32 在小端序写入 uint32 值。
func (a *Arena) WriteUint32(off int, v uint32) {
	binary.LittleEndian.PutUint32(a.buf[off:off+4], v)
}

// ReadUint32 以小端序读取 uint32 值。
func (a *Arena) ReadUint32(off int) uint32 {
	return binary.LittleEndian.Uint32(a.buf[off : off+4])
}

// WriteUint16 以小端序写入 uint16 值。
func (a *Arena) WriteUint16(off int, v uint16) {
	binary.LittleEndian.PutUint16(a.buf[off:off+2], v)
}

// ReadUint16 以小端序读取 uint16 值。
func (a *Arena) ReadUint16(off int) uint16 {
	return binary.LittleEndian.Uint16(a.buf[off : off+2])
}

// WriteUint64 以小端序写入 uint64 值。
func (a *Arena) WriteUint64(off int, v uint64) {
	binary.LittleEndian.PutUint64(a.buf[off:off+8], v)
}

// ReadUint64 以小端序读取 uint64 值。
func (a *Arena) ReadUint64(off int) uint64 {
	return binary.LittleEndian.Uint64(a.buf[off : off+8])
}

// WriteFloat64 在 Arena 中写入 float64 值。
func (a *Arena) WriteFloat64(off int, v float64) {
	a.WriteUint64(off, math.Float64bits(v))
}

// ReadFloat64 从 Arena 中读取 float64 值。
func (a *Arena) ReadFloat64(off int) float64 {
	return math.Float64frombits(a.ReadUint64(off))
}

// WriteByte 在指定偏移写入单字节。
func (a *Arena) WriteByte(off int, b byte) {
	a.buf[off] = b
}

// ReadByte 从指定偏移读取单字节。
func (a *Arena) ReadByte(off int) byte {
	return a.buf[off]
}

// WriteBytes 在指定偏移写入字节切片。
func (a *Arena) WriteBytes(off int, data []byte) {
	copy(a.buf[off:], data)
}

// ReadBytes 读取数据副本。会分配新的 []byte 并复制数据。
// 若需零分配读取，请使用 GetSlice。
func (a *Arena) ReadBytes(off int, size int) []byte {
	if size <= 0 {
		return nil
	}
	result := make([]byte, size)
	copy(result, a.buf[off:off+size])
	return result
}

// GetSlice 返回 Arena 内部 []byte 的切片视图，零分配。
// 调用方不应持有此切片到 Arena 扩缩容之后。
func (a *Arena) GetSlice(off int, size int) []byte {
	return a.buf[off : off+size]
}

// ReadString 读取字符串数据（数据以长度前缀存储）。
func (a *Arena) ReadString(off int) string {
	size := int(a.ReadUint32(off - arenaHeaderSize))
	return string(a.buf[off : off+size])
}

// SizeAt 返回 dataOff 处数据块的大小（从 header 读取）。
func (a *Arena) SizeAt(off int) int {
	return int(a.ReadUint32(off - arenaHeaderSize))
}

// Reset 重置 Arena，清空所有数据和空闲列表。
func (a *Arena) Reset() {
	a.off = 0
	a.free = a.free[:0]
	a.buf = make([]byte, a.page)
}

// grow 扩展底层缓冲区。新大小为当前大小的 2 倍，
// 持续翻倍直到能容纳 needed 字节。
func (a *Arena) grow(needed int) {
	newSize := len(a.buf) * 2
	for newSize < a.off+needed {
		newSize *= 2
	}
	newBuf := make([]byte, newSize)
	copy(newBuf, a.buf[:a.off])
	a.buf = newBuf
}
