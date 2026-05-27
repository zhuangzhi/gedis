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

// 对象头管理。
// 每个存储值在 Arena 中都有一个 16 字节的对象头:
//
//	Offset | Size | Field
//	     0 |    1 | type       (ObjString, ObjList, ...)
//	     1 |    1 | encoding   (ObjEncodingRaw, ObjEncodingInt, ...)
//	     2 |    4 | lru        (未使用)
//	     6 |    2 | refcount   (引用计数)
//	     8 |    8 | dataOffset (数据区域偏移)
package gedis

const (
	ObjHeaderSize = 16

	// 编码类型
	ObjEncodingRaw        uint8 = 0 // 原始字节
	ObjEncodingInt        uint8 = 1 // 整数 (内联存储在对象头)
	ObjEncodingZiplist    uint8 = 2 // Ziplist 紧凑列表
	ObjEncodingIntset     uint8 = 3 // 整数集合
	ObjEncodingSkiplist   uint8 = 4 // 跳跃表
	ObjEncodingLinkedList uint8 = 5 // 链表 (保留)
	ObjEncodingHashtable  uint8 = 6 // 哈希表
)

const (
	ObjString uint8 = 0  // 字符串
	ObjList   uint8 = 1  // 列表
	ObjHash   uint8 = 2  // 哈希
	ObjSet    uint8 = 3  // 集合
	ObjZSet   uint8 = 4  // 有序集合
	ObjBitmap uint8 = 5  // 位图
	ObjHLL    uint8 = 6  // HyperLogLog
	ObjJSON   uint8 = 7  // JSON 文档
	ObjStream uint8 = 8  // 流
	ObjTS     uint8 = 9  // 时间序列
	ObjBloom  uint8 = 10 // 布隆过滤器
	ObjCuckoo uint8 = 11 // 布谷鸟过滤器
	ObjCMS    uint8 = 12 // Count-Min Sketch
	ObjTopK   uint8 = 13 // Top-K
	ObjSearch uint8 = 14 // 全文搜索
	ObjGraph  uint8 = 15 // 图
	ObjCell   uint8 = 16 // 速率限制
)

// 对象头各字段在 Arena 块内的偏移量
func objHeaderType(off int) int       { return off }
func objHeaderEncoding(off int) int   { return off + 1 }
func objHeaderLRU(off int) int        { return off + 2 }
func objHeaderRefcount(off int) int   { return off + 4 }
func objHeaderDataOffset(off int) int { return off + 8 }

// NewObject 创建一个新对象头。typ 为对象类型，encoding 为编码方式，dataOff 为数据偏移。
func (db *RedisDB) NewObject(typ uint8, encoding uint8, dataOff int) int {
	headOff := db.arena.Alloc(ObjHeaderSize)
	db.arena.WriteByte(objHeaderType(headOff), typ)
	db.arena.WriteByte(objHeaderEncoding(headOff), encoding)
	db.arena.WriteUint16(objHeaderLRU(headOff), 0)
	db.arena.WriteUint32(objHeaderRefcount(headOff), 1)
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
	return headOff
}

// ObjectType 读取对象类型。
func (db *RedisDB) ObjectType(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderType(headOff))
}

// ObjectEncoding 读取对象编码方式。
func (db *RedisDB) ObjectEncoding(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderEncoding(headOff))
}

// ObjectDataOffset 读取数据偏移量。
func (db *RedisDB) ObjectDataOffset(headOff int) int {
	return int(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
}

// ObjectRefcount 读取引用计数。
func (db *RedisDB) ObjectRefcount(headOff int) uint32 {
	return db.arena.ReadUint32(objHeaderRefcount(headOff))
}

// IncrRefcount 增加引用计数。
func (db *RedisDB) IncrRefcount(headOff int) {
	rc := db.ObjectRefcount(headOff)
	db.arena.WriteUint32(objHeaderRefcount(headOff), rc+1)
}

// DecrRefcount 减少引用计数并返回新值。
func (db *RedisDB) DecrRefcount(headOff int) uint32 {
	rc := db.ObjectRefcount(headOff)
	if rc > 0 {
		rc--
		db.arena.WriteUint32(objHeaderRefcount(headOff), rc)
	}
	return rc
}

// FreeObject 释放对象。减少引用计数，若变为 0 则释放对象头和数据区域。
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

// ObjectSetDataOffset 更新数据偏移。
func (db *RedisDB) ObjectSetDataOffset(headOff int, dataOff int) {
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
}

// ObjectSetEncoding 更新编码方式。
func (db *RedisDB) ObjectSetEncoding(headOff int, enc uint8) {
	db.arena.WriteByte(objHeaderEncoding(headOff), enc)
}
