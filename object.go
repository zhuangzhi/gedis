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

// 对象头布局 (16 字节):
//
//	Offset  Size  Field
//	  0      1    type
//	  1      1    encoding
//	  2      4    lru
//	  6      2    refcount
//	  8      8    data_offset
const (
	ObjHeaderSize = 16

	ObjEncodingRaw        uint8 = 0 // 原始字节数据
	ObjEncodingInt        uint8 = 1 // 内联整数
	ObjEncodingZiplist    uint8 = 2 // 压缩列表
	ObjEncodingIntset     uint8 = 3 // 整数集合
	ObjEncodingSkiplist   uint8 = 4 // 跳跃表
	ObjEncodingLinkedList uint8 = 5 // 链表
	ObjEncodingHashtable  uint8 = 6 // 哈希表
)

const (
	ObjString uint8 = 0
	ObjList   uint8 = 1
	ObjHash   uint8 = 2
	ObjSet    uint8 = 3
	ObjZSet   uint8 = 4
	ObjBitmap uint8 = 5
	ObjHLL    uint8 = 6
	ObjJSON   uint8 = 7
	ObjStream uint8 = 8
	ObjTS     uint8 = 9
	ObjBloom  uint8 = 10
	ObjCuckoo uint8 = 11
	ObjCMS    uint8 = 12
	ObjTopK   uint8 = 13
	ObjSearch uint8 = 14
	ObjGraph  uint8 = 15
	ObjCell   uint8 = 16
)

// 对象头各字段偏移访问器
func objHeaderType(off int) int       { return off }
func objHeaderEncoding(off int) int   { return off + 1 }
func objHeaderLRU(off int) int        { return off + 2 }
func objHeaderRefcount(off int) int   { return off + 4 }
func objHeaderDataOffset(off int) int { return off + 8 }

// NewObject 在 Arena 中创建对象头，refcount 初始为 1。
func (db *RedisDB) NewObject(typ uint8, encoding uint8, dataOff int) int {
	headOff := db.arena.Alloc(ObjHeaderSize)
	db.arena.WriteByte(objHeaderType(headOff), typ)
	db.arena.WriteByte(objHeaderEncoding(headOff), encoding)
	db.arena.WriteUint16(objHeaderLRU(headOff), 0)
	db.arena.WriteUint32(objHeaderRefcount(headOff), 1)
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
	return headOff
}

// ObjectType 返回对象的类型标识。
func (db *RedisDB) ObjectType(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderType(headOff))
}

// ObjectEncoding 返回对象的编码方式。
func (db *RedisDB) ObjectEncoding(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderEncoding(headOff))
}

// ObjectDataOffset 返回对象的 payload 数据在 Arena 中的偏移。
func (db *RedisDB) ObjectDataOffset(headOff int) int {
	return int(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
}

// ObjectRefcount 返回对象的引用计数。
func (db *RedisDB) ObjectRefcount(headOff int) uint32 {
	return db.arena.ReadUint32(objHeaderRefcount(headOff))
}

// IncrRefcount 增加对象引用计数。
func (db *RedisDB) IncrRefcount(headOff int) {
	rc := db.ObjectRefcount(headOff)
	db.arena.WriteUint32(objHeaderRefcount(headOff), rc+1)
}

// DecrRefcount 递减对象引用计数，返回递减后的值。
func (db *RedisDB) DecrRefcount(headOff int) uint32 {
	rc := db.ObjectRefcount(headOff)
	if rc > 0 {
		rc--
		db.arena.WriteUint32(objHeaderRefcount(headOff), rc)
	}
	return rc
}

// FreeObject 释放对象。先递减引用计数，只有归零时才释放 Arena 中的数据。
func (db *RedisDB) FreeObject(headOff int) {
	if headOff == 0 {
		return
	}
	if db.DecrRefcount(headOff) > 0 {
		return
	}
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff != 0 {
		db.arena.Free(dataOff)
	}
	db.arena.Free(headOff)
}

// ObjectSetDataOffset 更新对象的 data 偏移。
func (db *RedisDB) ObjectSetDataOffset(headOff int, dataOff int) {
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
}

// ObjectSetEncoding 更新对象的编码类型。
func (db *RedisDB) ObjectSetEncoding(headOff int, enc uint8) {
	db.arena.WriteByte(objHeaderEncoding(headOff), enc)
}
