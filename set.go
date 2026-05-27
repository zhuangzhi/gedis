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
)

func setValToBuf(val []byte) *PooledBuffer {
	pb := NewBuf(len(val))
	pb.buf.Write(val)
	return pb
}

func (db *RedisDB) SAdd(key string, members ...*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		if allInts(members) && len(members) <= 512 {
			isOff := intsetNew(db.arena)
			added := 0
			for _, m := range members {
				val := int64(binary.LittleEndian.Uint64(m.Bytes()))
				if intsetAdd(db.arena, &isOff, val) {
					added++
				}
			}
			headOff = db.NewObject(ObjSet, ObjEncodingIntset, isOff)
			db.dict.Set(keyBytes, headOff)
			return added
		}

		innerDict := NewDict(db.arena)
		added := 0
		for _, m := range members {
			mb := m.Bytes()
			if _, ok := innerDict.Get(mb); !ok {
				innerDict.Set(mb, 0)
				added++
			}
		}
		sdOff := db.arena.Alloc(12)
		innerDict.StoreMeta(sdOff)
		headOff = db.NewObject(ObjSet, ObjEncodingHashtable, sdOff)
		db.dict.Set(keyBytes, headOff)
		return added
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingIntset {
		isOff := dataOff
		added := 0
		for _, m := range members {
			val := int64(binary.LittleEndian.Uint64(m.Bytes()))
			if intsetAdd(db.arena, &isOff, val) {
				added++
			}
		}
		db.ObjectSetDataOffset(headOff, isOff)
		return added
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		added := 0
		for _, m := range members {
			mb := m.Bytes()
			if _, ok := innerDict.Get(mb); !ok {
				innerDict.Set(mb, 0)
				added++
			}
		}
		return added
	}

	return 0
}

func (db *RedisDB) SRem(key string, members ...*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingIntset {
		isOff := dataOff
		removed := 0
		for _, m := range members {
			val := int64(binary.LittleEndian.Uint64(m.Bytes()))
			if intsetRemove(db.arena, &isOff, val) {
				removed++
			}
		}
		db.ObjectSetDataOffset(headOff, isOff)
		if intsetLen(db.arena, isOff) == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
		return removed
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		removed := 0
		for _, m := range members {
			if innerDict.Del(m.Bytes()) {
				removed++
			}
		}
		return removed
	}

	return 0
}

func (db *RedisDB) SIsMember(key string, member *PooledBuffer) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)
	mb := member.Bytes()

	if enc == ObjEncodingIntset {
		val := int64(binary.LittleEndian.Uint64(mb))
		return intsetFind(db.arena, dataOff, val)
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		_, ok := innerDict.Get(mb)
		return ok
	}

	return false
}

func (db *RedisDB) SMembers(key string) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingIntset {
		isOff := dataOff
		n := intsetLen(db.arena, isOff)
		result := make([]*PooledBuffer, 0, n)
		for i := 0; i < n; i++ {
			val := intsetGet(db.arena, isOff, i)
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, uint64(val))
			result = append(result, setValToBuf(buf))
		}
		return result
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		result := make([]*PooledBuffer, 0)
		for i := 0; i < innerDict.size; i++ {
			kOff, _ := innerDict.getSlot(i)
			if kOff != 0 {
				size := db.arena.SizeAt(kOff)
				key := db.arena.ReadBytes(kOff, size)
				result = append(result, setValToBuf(key))
			}
		}
		return result
	}

	return nil
}

func (db *RedisDB) SCard(key string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingIntset {
		return intsetLen(db.arena, dataOff)
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		return innerDict.used
	}

	return 0
}

func (db *RedisDB) SInter(keys ...string) []*PooledBuffer {
	if len(keys) == 0 {
		return nil
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	var result []*PooledBuffer
	first := true

	for _, key := range keys {
		members := db.SMembers(key)
		if first {
			result = members
			first = false
			continue
		}

		set := make(map[string]bool, len(members))
		for _, m := range members {
			set[m.String()] = true
		}

		filtered := make([]*PooledBuffer, 0)
		for _, m := range result {
			if set[m.String()] {
				filtered = append(filtered, m)
			}
		}
		result = filtered
	}

	return result
}

func (db *RedisDB) SUnion(keys ...string) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	seen := make(map[string]bool)
	var result []*PooledBuffer

	for _, key := range keys {
		for _, m := range db.SMembers(key) {
			s := m.String()
			if !seen[s] {
				seen[s] = true
				result = append(result, m)
			}
		}
	}

	return result
}

func allInts(members []*PooledBuffer) bool {
	for _, m := range members {
		if m.Len() != 8 {
			return false
		}
	}
	return true
}

// === Intset Implementation ===

func intsetEncodingOff(arena *Arena, isOff int) int { return isOff }
func intsetLengthOff(arena *Arena, isOff int) int   { return isOff + 2 }
func intsetContentsOff(arena *Arena, isOff int) int { return isOff + 6 }

func intsetNew(arena *Arena) int {
	size := 6
	off := arena.Alloc(size)
	arena.WriteUint16(intsetEncodingOff(arena, off), 8)
	arena.WriteUint32(intsetLengthOff(arena, off), 0)
	return off
}

func intsetLen(arena *Arena, isOff int) int {
	return int(arena.ReadUint32(intsetLengthOff(arena, isOff)))
}

func intsetEncoding(arena *Arena, isOff int) int {
	return int(arena.ReadUint16(intsetEncodingOff(arena, isOff)))
}

func intsetGet(arena *Arena, isOff int, index int) int64 {
	enc := intsetEncoding(arena, isOff)
	contentsOff := intsetContentsOff(arena, isOff)
	switch enc {
	case 2:
		return int64(int16(arena.ReadUint16(contentsOff + index*2)))
	case 4:
		return int64(int32(arena.ReadUint32(contentsOff + index*4)))
	case 8:
		return int64(arena.ReadUint64(contentsOff + index*8))
	}
	return 0
}

func intsetFind(arena *Arena, isOff int, value int64) bool {
	n := intsetLen(arena, isOff)
	for i := 0; i < n; i++ {
		if intsetGet(arena, isOff, i) == value {
			return true
		}
	}
	return false
}

func intsetAdd(arena *Arena, isOff *int, value int64) bool {
	if intsetFind(arena, *isOff, value) {
		return false
	}

	n := intsetLen(arena, *isOff)
	enc := intsetEncoding(arena, *isOff)

	neededEnc := enc
	switch {
	case value >= -32768 && value <= 32767:
		if enc < 2 {
			neededEnc = 2
		}
	case value >= -2147483648 && value <= 2147483647:
		if enc < 4 {
			neededEnc = 4
		}
	default:
		neededEnc = 8
	}

	if neededEnc > enc {
		newIsOff := intsetUpgradeAndAdd(arena, *isOff, value)
		*isOff = newIsOff
		return true
	}

	oldSize := 6 + n*enc
	newSize := 6 + (n+1)*neededEnc

	data := arena.ReadBytes(*isOff, oldSize)
	arena.Free(*isOff)
	newOff := arena.Alloc(newSize)
	arena.WriteBytes(newOff, data)

	var insertPos int
	for insertPos = 0; insertPos < n; insertPos++ {
		if intsetGet(arena, newOff, insertPos) > value {
			break
		}
	}

	newContentsOff := intsetContentsOff(arena, newOff)
	arena.WriteUint32(intsetLengthOff(arena, newOff), uint32(n+1))

	for i := n; i > insertPos; i-- {
		oldVal := intsetGet(arena, newOff, i-1)
		switch enc {
		case 2:
			arena.WriteUint16(newContentsOff+i*2, uint16(oldVal))
		case 4:
			arena.WriteUint32(newContentsOff+i*4, uint32(oldVal))
		case 8:
			arena.WriteUint64(newContentsOff+i*8, uint64(oldVal))
		}
	}

	switch enc {
	case 2:
		arena.WriteUint16(newContentsOff+insertPos*2, uint16(value))
	case 4:
		arena.WriteUint32(newContentsOff+insertPos*4, uint32(value))
	case 8:
		arena.WriteUint64(newContentsOff+insertPos*8, uint64(value))
	}

	*isOff = newOff
	return true
}

func intsetRemove(arena *Arena, isOff *int, value int64) bool {
	n := intsetLen(arena, *isOff)
	enc := intsetEncoding(arena, *isOff)

	pos := -1
	for i := 0; i < n; i++ {
		if intsetGet(arena, *isOff, i) == value {
			pos = i
			break
		}
	}
	if pos == -1 {
		return false
	}

	contentsOff := intsetContentsOff(arena, *isOff)
	for i := pos; i < n-1; i++ {
		nextVal := intsetGet(arena, *isOff, i+1)
		switch enc {
		case 2:
			arena.WriteUint16(contentsOff+i*2, uint16(nextVal))
		case 4:
			arena.WriteUint32(contentsOff+i*4, uint32(nextVal))
		case 8:
			arena.WriteUint64(contentsOff+i*8, uint64(nextVal))
		}
	}

	arena.WriteUint32(intsetLengthOff(arena, *isOff), uint32(n-1))
	return true
}

func intsetUpgradeAndAdd(arena *Arena, isOff int, value int64) int {
	n := intsetLen(arena, isOff)
	oldEnc := intsetEncoding(arena, isOff)
	newEnc := 2
	if value < -32768 || value > 32767 {
		newEnc = 4
	}
	if value < -2147483648 || value > 2147483647 {
		newEnc = 8
	}
	if newEnc < oldEnc {
		newEnc = oldEnc
	}

	newSize := 6 + (n+1)*newEnc
	newOff := arena.Alloc(newSize)
	arena.WriteUint16(intsetEncodingOff(arena, newOff), uint16(newEnc))
	arena.WriteUint32(intsetLengthOff(arena, newOff), uint32(n+1))

	newContentsOff := intsetContentsOff(arena, newOff)
	oldContentsOff := intsetContentsOff(arena, isOff)

	prepend := value < 0
	insertPos := 0
	if prepend {
		switch newEnc {
		case 2:
			arena.WriteUint16(newContentsOff, uint16(value))
		case 4:
			arena.WriteUint32(newContentsOff, uint32(value))
		case 8:
			arena.WriteUint64(newContentsOff, uint64(value))
		}
		insertPos = 1
	}

	for i := 0; i < n; i++ {
		var oldVal int64
		switch oldEnc {
		case 2:
			oldVal = int64(int16(arena.ReadUint16(oldContentsOff + i*2)))
		case 4:
			oldVal = int64(int32(arena.ReadUint32(oldContentsOff + i*4)))
		case 8:
			oldVal = int64(arena.ReadUint64(oldContentsOff + i*8))
		}

		if !prepend && oldVal > value {
			switch newEnc {
			case 2:
				arena.WriteUint16(newContentsOff+insertPos*2, uint16(value))
			case 4:
				arena.WriteUint32(newContentsOff+insertPos*4, uint32(value))
			case 8:
				arena.WriteUint64(newContentsOff+insertPos*8, uint64(value))
			}
			insertPos++
			prepend = true
		}

		switch newEnc {
		case 2:
			arena.WriteUint16(newContentsOff+insertPos*2, uint16(oldVal))
		case 4:
			arena.WriteUint32(newContentsOff+insertPos*4, uint32(oldVal))
		case 8:
			arena.WriteUint64(newContentsOff+insertPos*8, uint64(oldVal))
		}
		insertPos++
	}

	if !prepend {
		switch newEnc {
		case 2:
			arena.WriteUint16(newContentsOff+insertPos*2, uint16(value))
		case 4:
			arena.WriteUint32(newContentsOff+insertPos*4, uint32(value))
		case 8:
			arena.WriteUint64(newContentsOff+insertPos*8, uint64(value))
		}
	}

	arena.Free(isOff)
	return newOff
}
