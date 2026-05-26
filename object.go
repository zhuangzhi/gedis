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

const (
	ObjHeaderSize = 16

	ObjEncodingRaw        uint8 = 0
	ObjEncodingInt        uint8 = 1
	ObjEncodingZiplist    uint8 = 2
	ObjEncodingIntset     uint8 = 3
	ObjEncodingSkiplist   uint8 = 4
	ObjEncodingLinkedList uint8 = 5
	ObjEncodingHashtable  uint8 = 6
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

func objHeaderType(off int) int       { return off }
func objHeaderEncoding(off int) int   { return off + 1 }
func objHeaderLRU(off int) int        { return off + 2 }
func objHeaderRefcount(off int) int   { return off + 4 }
func objHeaderDataOffset(off int) int { return off + 8 }

func (db *RedisDB) NewObject(typ uint8, encoding uint8, dataOff int) int {
	headOff := db.arena.Alloc(ObjHeaderSize)
	db.arena.WriteByte(objHeaderType(headOff), typ)
	db.arena.WriteByte(objHeaderEncoding(headOff), encoding)
	db.arena.WriteUint16(objHeaderLRU(headOff), 0)
	db.arena.WriteUint32(objHeaderRefcount(headOff), 1)
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
	return headOff
}

func (db *RedisDB) ObjectType(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderType(headOff))
}

func (db *RedisDB) ObjectEncoding(headOff int) uint8 {
	return db.arena.ReadByte(objHeaderEncoding(headOff))
}

func (db *RedisDB) ObjectDataOffset(headOff int) int {
	return int(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
}

func (db *RedisDB) ObjectRefcount(headOff int) uint32 {
	return db.arena.ReadUint32(objHeaderRefcount(headOff))
}

func (db *RedisDB) IncrRefcount(headOff int) {
	rc := db.ObjectRefcount(headOff)
	db.arena.WriteUint32(objHeaderRefcount(headOff), rc+1)
}

func (db *RedisDB) DecrRefcount(headOff int) uint32 {
	rc := db.ObjectRefcount(headOff)
	if rc > 0 {
		rc--
		db.arena.WriteUint32(objHeaderRefcount(headOff), rc)
	}
	return rc
}

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

func (db *RedisDB) ObjectSetDataOffset(headOff int, dataOff int) {
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(dataOff))
}

func (db *RedisDB) ObjectSetEncoding(headOff int, enc uint8) {
	db.arena.WriteByte(objHeaderEncoding(headOff), enc)
}
