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

import "encoding/binary"

// Ziplist 是一个双端紧凑列表结构，用于 List、Hash、小 ZSet。
// 格式:
//
//	+--------+--------+-------+--------+--------+
//	| zlbytes| zltail | zllen | entry1 | ...    | 0xFF |
//	+--------+--------+-------+--------+--------+
//	  4B       4B       2B       variable      1B
//
// 每个 entry: [prevLen(var)] [encoding(var)] [data(var)]
const (
	ziplistHeaderSize = 10 // zlbytes(4) + zltail(4) + zllen(2)
	ziplistEndByte    = 0xFF
	ziplistMaxPrevLen = 254 // 超过此值 prevLen 使用 5 字节编码
)

// ziplistNew 创建一个空 ziplist。
func ziplistNew(arena *Arena) int {
	size := ziplistHeaderSize + 1 // +1 for end byte
	off := arena.Alloc(size)
	arena.WriteUint32(off, uint32(size))                // zlbytes
	arena.WriteUint32(off+4, uint32(ziplistHeaderSize)) // zltail
	arena.WriteUint16(off+8, 0)                         // zllen
	arena.WriteByte(off+ziplistHeaderSize, ziplistEndByte)
	return off
}

// === Ziplist header 访问器 ===

func ziplistTotalBytes(arena *Arena, zlOff int) int       { return int(arena.ReadUint32(zlOff)) }
func ziplistSetTotalBytes(arena *Arena, zlOff int, v int) { arena.WriteUint32(zlOff, uint32(v)) }
func ziplistTailOffset(arena *Arena, zlOff int) int       { return int(arena.ReadUint32(zlOff + 4)) }
func ziplistSetTailOffset(arena *Arena, zlOff int, v int) { arena.WriteUint32(zlOff+4, uint32(v)) }
func ziplistNumEntries(arena *Arena, zlOff int) int       { return int(arena.ReadUint16(zlOff + 8)) }
func ziplistSetNumEntries(arena *Arena, zlOff int, v int) { arena.WriteUint16(zlOff+8, uint16(v)) }

// ziplistResize 调整 ziplist 大小。会将数据迁移到 Arena 新位置。
func ziplistResize(arena *Arena, zlOff int, newSize int) int {
	oldSize := ziplistTotalBytes(arena, zlOff)
	copySize := oldSize
	if newSize < oldSize {
		copySize = newSize
	}
	data := arena.ReadBytes(zlOff, copySize)
	arena.Free(zlOff)
	newOff := arena.Alloc(newSize)
	arena.WriteBytes(newOff, data)
	if newSize > oldSize {
		arena.WriteByte(newOff+newSize-1, ziplistEndByte)
	}
	ziplistSetTotalBytes(arena, newOff, newSize)
	return newOff
}

// === Entry 编码/解码 ===

// ziplistEntryPrevLen 读取 entry 的前驱长度及 prevLen 字段自身占用字节数。
// prevLen < 254: 1 字节; prevLen >= 254: 5 字节 (0xFE + 4B LE)。
func ziplistEntryPrevLen(arena *Arena, entryOff int) (prevLen int, prevLenSize int) {
	b := arena.ReadByte(entryOff)
	if b < ziplistMaxPrevLen {
		return int(b), 1
	}
	return int(binary.LittleEndian.Uint32(arena.GetSlice(entryOff+1, 4))), 5
}

// ziplistWritePrevLen 在 entry 起始位置写入前驱长度，返回写入的字节数。
func ziplistWritePrevLen(arena *Arena, entryOff int, prevLen int) int {
	if prevLen < ziplistMaxPrevLen {
		arena.WriteByte(entryOff, byte(prevLen))
		return 1
	}
	arena.WriteByte(entryOff, ziplistMaxPrevLen)
	binary.LittleEndian.PutUint32(arena.GetSlice(entryOff+1, 4), uint32(prevLen))
	return 5
}

// ziplistEntryEncoding 解析 entry 的编码字节。返回:
//
//	isString - true 为字符串，false 为整数
//	length   - 字符串长度（仅 isString=true 时有效）
//	encSize  - encoding 字段占用的字节数
//
// 编码规则:
//
//	00xxxxxx           → 0-63 字节字符串
//	01xxxxxx <len-8>   → 64-16383 字节字符串
//	10______ <len-32>  → >=16384 字节字符串 (大端序)
//	11xx____           → 整数编码
func ziplistEntryEncoding(arena *Arena, entryOff int) (isString bool, length int, encSize int) {
	b := arena.ReadByte(entryOff)
	if b <= 0x3F {
		return true, int(b), 1
	}
	if b <= 0x7F {
		high := int(b&0x3F) << 8
		low := int(arena.ReadByte(entryOff + 1))
		return true, high | low, 2
	}
	if b <= 0xBF {
		size := int(binary.BigEndian.Uint32(arena.GetSlice(entryOff+1, 4)))
		return true, size, 5
	}
	if b <= 0xDF {
		return false, 0, 1
	}
	if b <= 0xEF {
		return false, 0, 1
	}
	if b <= 0xFF {
		return false, 0, 1
	}
	return true, int(b), 1
}

// ziplistWriteEntry 在 entryOff 写入一个字符串 entry，返回总字节数。
func ziplistWriteEntry(arena *Arena, entryOff int, prevLen int, data []byte) int {
	pos := entryOff
	pos += ziplistWritePrevLen(arena, pos, prevLen)

	if len(data) <= 0x3F {
		arena.WriteByte(pos, byte(len(data)))
		pos++
	} else if len(data) <= 0x3FFF {
		arena.WriteByte(pos, byte(0x40|(len(data)>>8)))
		arena.WriteByte(pos+1, byte(len(data)&0xFF))
		pos += 2
	} else {
		arena.WriteByte(pos, 0x80)
		binary.BigEndian.PutUint32(arena.GetSlice(pos+1, 4), uint32(len(data)))
		pos += 5
	}
	arena.WriteBytes(pos, data)
	pos += len(data)
	return pos - entryOff
}

// ziplistWriteEntryInt 写入一个整数 entry，返回总字节数。
func ziplistWriteEntryInt(arena *Arena, entryOff int, prevLen int, val int64) int {
	pos := entryOff
	pos += ziplistWritePrevLen(arena, pos, prevLen)

	if val >= 0 && val <= 12 {
		arena.WriteByte(pos, byte(0xF1+val-1))
		pos++
	} else if val >= mathMinInt8 && val <= mathMaxInt8 {
		arena.WriteByte(pos, 0xFE)
		arena.WriteByte(pos+1, byte(val))
		pos += 2
	} else if val >= mathMinInt16 && val <= mathMaxInt16 {
		arena.WriteByte(pos, 0xC0)
		arena.WriteUint16(pos+1, uint16(val))
		pos += 3
	} else if val >= mathMinInt32 && val <= mathMaxInt32 {
		arena.WriteByte(pos, 0xD0)
		arena.WriteUint32(pos+1, uint32(val))
		pos += 5
	} else {
		arena.WriteByte(pos, 0xE0)
		arena.WriteUint64(pos+1, uint64(val))
		pos += 9
	}
	return pos - entryOff
}

// ziplistReadEntryInt 从 entry 读取整数值。返回值和 encoding 的字节数。
func ziplistReadEntryInt(arena *Arena, entryOff int, prevLenSize int) (int64, int) {
	b := arena.ReadByte(entryOff + prevLenSize)
	if b >= 0xF1 && b <= 0xFD {
		return int64(b - 0xF1 + 1), 1
	}
	switch b {
	case 0xFE:
		return int64(int8(arena.ReadByte(entryOff + prevLenSize + 1))), 2
	case 0xC0:
		return int64(int16(arena.ReadUint16(entryOff + prevLenSize + 1))), 3
	case 0xD0:
		return int64(int32(arena.ReadUint32(entryOff + prevLenSize + 1))), 5
	case 0xE0:
		return int64(arena.ReadUint64(entryOff + prevLenSize + 1)), 9
	}
	return 0, 0
}

// ziplistReadEntryData 读取 entry 的 payload 数据（会分配新 []byte）。
func ziplistReadEntryData(arena *Arena, entryOff int) []byte {
	_, prevLenSize := ziplistEntryPrevLen(arena, entryOff)
	isStr, length, encSize := ziplistEntryEncoding(arena, entryOff+prevLenSize)
	if isStr {
		return arena.ReadBytes(entryOff+prevLenSize+encSize, length)
	}
	val, _ := ziplistReadEntryInt(arena, entryOff, prevLenSize)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(val))
	return buf
}

// ziplistEntryDataEquals 零分配比较 entry 数据与给定字节切片是否相等。
// 直接在 Arena 的 []byte 上逐字节比较，不分配副本。
func ziplistEntryDataEquals(arena *Arena, entryOff int, data []byte) bool {
	_, prevLenSize := ziplistEntryPrevLen(arena, entryOff)
	isStr, length, encSize := ziplistEntryEncoding(arena, entryOff+prevLenSize)
	if !isStr {
		return false
	}
	if length != len(data) {
		return false
	}
	dataOff := entryOff + prevLenSize + encSize
	slice := arena.GetSlice(dataOff, length)
	for i := range slice {
		if slice[i] != data[i] {
			return false
		}
	}
	return true
}

// ziplistEntrySize 计算写入给定前驱长度和数据需要的字节数。
func ziplistEntrySize(prevLen int, data []byte) int {
	plSize := 1
	if prevLen >= ziplistMaxPrevLen {
		plSize = 5
	}
	encSize := 1
	if len(data) > 0x3F {
		encSize = 2
	}
	if len(data) > 0x3FFF {
		encSize = 5
	}
	return plSize + encSize + len(data)
}

// ziplistEntryTotalSize 返回 entry 总字节数（prevLen + encoding + data）。
func ziplistEntryTotalSize(arena *Arena, entryOff int) int {
	_, plSize := ziplistEntryPrevLen(arena, entryOff)
	_, length, encSize := ziplistEntryEncoding(arena, entryOff+plSize)
	return plSize + encSize + length
}

// ziplistFind 在 ziplist 中查找指定数据，返回索引，未找到返回 -1。
func ziplistFind(arena *Arena, zlOff int, data []byte) int {
	if zlOff == 0 {
		return -1
	}
	num := ziplistNumEntries(arena, zlOff)
	if num == 0 {
		return -1
	}
	pos := zlOff + ziplistHeaderSize
	for i := 0; i < num; i++ {
		entryData := ziplistReadEntryData(arena, pos)
		if bytesEqual(entryData, data) {
			return i
		}
		pos += ziplistEntryTotalSize(arena, pos)
	}
	return -1
}

// ziplistGet 按索引获取 entry 数据（会分配新 []byte）。
func ziplistGet(arena *Arena, zlOff int, index int) []byte {
	if zlOff == 0 || index < 0 {
		return nil
	}
	num := ziplistNumEntries(arena, zlOff)
	if index >= num {
		return nil
	}
	pos := zlOff + ziplistHeaderSize
	for i := 0; i < index; i++ {
		pos += ziplistEntryTotalSize(arena, pos)
	}
	return ziplistReadEntryData(arena, pos)
}

// ziplistInsert 在 ziplist 头部或尾部插入数据。atHead=true 时插入头部。
func ziplistInsert(arena *Arena, zlOff int, data []byte, atHead bool) int {
	num := ziplistNumEntries(arena, zlOff)
	oldSize := ziplistTotalBytes(arena, zlOff)
	oldZlOff := zlOff

	var relInsertPos int
	var prevLen int

	if atHead || num == 0 {
		relInsertPos = ziplistHeaderSize
		prevLen = 0
	} else {
		relPos := ziplistHeaderSize
		for i := 0; i < num; i++ {
			relPos += ziplistEntryTotalSize(arena, oldZlOff+relPos)
		}
		relInsertPos = relPos
		tailRelOff := ziplistTailOffset(arena, oldZlOff)
		prevLen = ziplistEntryTotalSize(arena, oldZlOff+tailRelOff)
	}

	entrySize := ziplistEntrySize(prevLen, data)
	newSize := oldSize + entrySize
	zlOff = ziplistResize(arena, zlOff, newSize)

	absInsertPos := zlOff + relInsertPos
	remainSize := oldSize - relInsertPos - 1
	if remainSize > 0 {
		arena.WriteBytes(absInsertPos+entrySize, arena.GetSlice(absInsertPos, remainSize))
	}

	ziplistWriteEntry(arena, absInsertPos, prevLen, data)

	if num > 0 {
		nextEntryOff := absInsertPos + entrySize
		if nextEntryOff < zlOff+newSize-1 {
			ziplistWritePrevLen(arena, nextEntryOff, entrySize)
		}
	}

	ziplistSetNumEntries(arena, zlOff, num+1)
	if atHead {
		ziplistSetTailOffset(arena, zlOff, ziplistTailOffset(arena, zlOff)+entrySize)
	} else {
		ziplistSetTailOffset(arena, zlOff, relInsertPos)
	}

	return zlOff
}

// ziplistDelete 删除指定索引的 entry。
func ziplistDelete(arena *Arena, zlOff int, index int) int {
	if zlOff == 0 {
		return 0
	}
	num := ziplistNumEntries(arena, zlOff)
	if index < 0 || index >= num {
		return zlOff
	}

	pos := zlOff + ziplistHeaderSize
	for i := 0; i < index; i++ {
		pos += ziplistEntryTotalSize(arena, pos)
	}

	entrySize := ziplistEntryTotalSize(arena, pos)
	oldSize := ziplistTotalBytes(arena, zlOff)

	newNextPrevLen := 0
	needUpdateNext := num > 1 && index < num-1
	if needUpdateNext {
		newNextPrevLen, _ = ziplistEntryPrevLen(arena, pos)
	}

	remainStart := pos + entrySize
	remainSize := oldSize - (remainStart - zlOff)
	if remainSize > 0 {
		arena.WriteBytes(pos, arena.GetSlice(remainStart, remainSize))
	}

	if needUpdateNext {
		ziplistWritePrevLen(arena, pos, newNextPrevLen)
	}

	newSize := oldSize - entrySize
	zlOff = ziplistResize(arena, zlOff, newSize)
	ziplistSetNumEntries(arena, zlOff, num-1)

	if num-1 == 0 {
		ziplistSetTailOffset(arena, zlOff, ziplistHeaderSize)
	}

	return zlOff
}

// ziplistLen 返回 ziplist 中 entry 数量。
func ziplistLen(arena *Arena, zlOff int) int {
	if zlOff == 0 {
		return 0
	}
	return ziplistNumEntries(arena, zlOff)
}

// bytesEqual 逐字节比较两个字节切片是否相等。
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const (
	mathMinInt8  = -128
	mathMaxInt8  = 127
	mathMinInt16 = -32768
	mathMaxInt16 = 32767
	mathMinInt32 = -2147483648
	mathMaxInt32 = 2147483647
)
