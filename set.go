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

// SAdd 向集合中添加成员。对外友好 API，入参 []byte。
func (db *RedisDB) SAdd(key string, members ...[]byte) int {
	bufs := make([]*PooledBuffer, len(members))
	for i, m := range members {
		bufs[i] = BufFromBytes(m)
	}
	result := db.SAddBuffer(key, bufs...)
	for _, b := range bufs {
		b.Close()
	}
	return result
}

// SAddBuffer 向集合中添加成员，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) SAddBuffer(key string, members ...*PooledBuffer) int {
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
		innerDict.StoreMeta(dataOff)
		return added
	}

	return 0
}

// SRem 从集合中移除成员。对外友好 API，入参 []byte。
func (db *RedisDB) SRem(key string, members ...[]byte) int {
	bufs := make([]*PooledBuffer, len(members))
	for i, m := range members {
		bufs[i] = BufFromBytes(m)
	}
	result := db.SRemBuffer(key, bufs...)
	for _, b := range bufs {
		b.Close()
	}
	return result
}

// SRemBuffer 从集合中移除成员，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) SRemBuffer(key string, members ...*PooledBuffer) int {
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
		innerDict.StoreMeta(dataOff)
		return removed
	}

	return 0
}

// SIsMember 判断成员是否在集合中。对外友好 API，入参 []byte。
func (db *RedisDB) SIsMember(key string, member []byte) bool {
	pb := BufFromBytes(member)
	result := db.SIsMemberBuffer(key, pb)
	pb.Close()
	return result
}

// SIsMemberBuffer 判断成员是否在集合中，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) SIsMemberBuffer(key string, member *PooledBuffer) bool {
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

// SMembers 获取集合中的所有成员。返回 *ZSlices，遍历后须 zs.Close()。
func (db *RedisDB) SMembers(key string) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.smembersLocked(key)
}

func (db *RedisDB) smembersLocked(key string) *ZSlices {
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
		result := NewZSlices()
		for i := 0; i < n; i++ {
			val := intsetGet(db.arena, isOff, i)
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, uint64(val))
			result.Add(buf)
		}
		result.Finish()
		return result
	}

	if enc == ObjEncodingHashtable {
		innerDict := LoadDictMeta(db.arena, dataOff)
		result := NewZSlices()
		for i := 0; i < innerDict.size; i++ {
			kOff, _ := innerDict.getSlot(i)
			if kOff != 0 {
				size := db.arena.SizeAt(kOff)
				key := db.arena.ReadBytes(kOff, size)
				result.Add(key)
			}
		}
		result.Finish()
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

// SInter 获取多个集合的交集。返回 *ZSlices，遍历后须 zs.Close()。
func (db *RedisDB) SInter(keys ...string) *ZSlices {
	if len(keys) == 0 {
		return nil
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	result := db.smembersLocked(keys[0])
	if result == nil {
		return nil
	}

	for _, key := range keys[1:] {
		other := db.smembersLocked(key)
		if other == nil {
			return nil
		}

		set := make(map[string]bool, other.Len())
		for i := 0; i < other.Len(); i++ {
			set[string(other.Get(i))] = true
		}
		other.Close()

		filtered := NewZSlices()
		for i := 0; i < result.Len(); i++ {
			s := string(result.Get(i))
			if set[s] {
				filtered.Add(result.Get(i))
			}
		}
		result.Close()
		filtered.Finish()
		result = filtered
	}

	return result
}

// SUnion 获取多个集合的并集。返回 *ZSlices，遍历后须 zs.Close()。
func (db *RedisDB) SUnion(keys ...string) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	seen := make(map[string]bool)
	result := NewZSlices()

	for _, key := range keys {
		zs := db.smembersLocked(key)
		if zs == nil {
			continue
		}
		for i := 0; i < zs.Len(); i++ {
			s := string(zs.Get(i))
			if !seen[s] {
				seen[s] = true
				result.Add(zs.Get(i))
			}
		}
		zs.Close()
	}
	result.Finish()
	return result
}

// SDiff 获取多个集合的差集。返回 *ZSlices，遍历后须 zs.Close()。
// 差集 = 第一个集合的元素，去除后续集合中存在的元素。
func (db *RedisDB) SDiff(keys ...string) *ZSlices {
	if len(keys) == 0 {
		return nil
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	result := db.smembersLocked(keys[0])
	if result == nil {
		return nil
	}

	if len(keys) == 1 {
		return result
	}

	for _, key := range keys[1:] {
		other := db.smembersLocked(key)
		if other == nil {
			result.Close()
			return nil
		}

		set := make(map[string]bool, other.Len())
		for i := 0; i < other.Len(); i++ {
			set[string(other.Get(i))] = true
		}
		other.Close()

		filtered := NewZSlices()
		for i := 0; i < result.Len(); i++ {
			s := string(result.Get(i))
			if !set[s] {
				filtered.Add(result.Get(i))
			}
		}
		result.Close()
		filtered.Finish()
		result = filtered
	}

	return result
}

// SDiffStore 获取多个集合的差集并将结果存入 destKey。返回元素数量。
// 如果 destKey 已存在，会覆盖。
func (db *RedisDB) SDiffStore(destKey string, keys ...string) int {
	if len(keys) == 0 {
		db.mu.Lock()
		defer db.mu.Unlock()
		headOff, ok := db.dict.Get([]byte(destKey))
		if ok {
			db.dict.Del([]byte(destKey))
			db.FreeObject(headOff)
		}
		return 0
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	result := db.smembersLocked(keys[0])
	if result == nil {
		headOff, ok := db.dict.Get([]byte(destKey))
		if ok {
			db.dict.Del([]byte(destKey))
			db.FreeObject(headOff)
		}
		return 0
	}

	if len(keys) == 1 {
		headOff, ok := db.dict.Get([]byte(destKey))
		if ok {
			db.dict.Del([]byte(destKey))
			db.FreeObject(headOff)
		}

		innerDict := NewDict(db.arena)
		for i := 0; i < result.Len(); i++ {
			mb := result.Get(i)
			innerDict.Set(mb, 0)
		}
		result.Close()

		sdOff := db.arena.Alloc(12)
		innerDict.StoreMeta(sdOff)
		headOff = db.NewObject(ObjSet, ObjEncodingHashtable, sdOff)
		db.dict.Set([]byte(destKey), headOff)
		return innerDict.used
	}

	for _, key := range keys[1:] {
		other := db.smembersLocked(key)
		if other == nil {
			result.Close()
			headOff, ok := db.dict.Get([]byte(destKey))
			if ok {
				db.dict.Del([]byte(destKey))
				db.FreeObject(headOff)
			}
			return 0
		}

		set := make(map[string]bool, other.Len())
		for i := 0; i < other.Len(); i++ {
			set[string(other.Get(i))] = true
		}
		other.Close()

		filtered := NewZSlices()
		for i := 0; i < result.Len(); i++ {
			s := string(result.Get(i))
			if !set[s] {
				filtered.Add(result.Get(i))
			}
		}
		result.Close()
		filtered.Finish()
		result = filtered
	}

	headOff, ok := db.dict.Get([]byte(destKey))
	if ok {
		db.dict.Del([]byte(destKey))
		db.FreeObject(headOff)
	}

	innerDict := NewDict(db.arena)
	for i := 0; i < result.Len(); i++ {
		mb := result.Get(i)
		innerDict.Set(mb, 0)
	}
	result.Close()

	sdOff := db.arena.Alloc(12)
	innerDict.StoreMeta(sdOff)
	headOff = db.NewObject(ObjSet, ObjEncodingHashtable, sdOff)
	db.dict.Set([]byte(destKey), headOff)
	return innerDict.used
}

func (db *RedisDB) SMove(srcKey, dstKey string, member []byte) bool {
	pb := BufFromBytes(member)
	result := db.SMoveBuffer(srcKey, dstKey, pb)
	pb.Close()
	return result
}

func (db *RedisDB) SMoveBuffer(srcKey, dstKey string, member *PooledBuffer) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	srcKeyBytes := []byte(srcKey)
	srcHeadOff, srcOk := db.dict.Get(srcKeyBytes)
	if !srcOk {
		return false
	}

	mb := member.Bytes()

	srcEnc := db.ObjectEncoding(srcHeadOff)
	srcDataOff := db.ObjectDataOffset(srcHeadOff)

	var found bool
	if srcEnc == ObjEncodingIntset {
		val := int64(binary.LittleEndian.Uint64(mb))
		found = intsetRemove(db.arena, &srcDataOff, val)
		if found {
			db.ObjectSetDataOffset(srcHeadOff, srcDataOff)
			if intsetLen(db.arena, srcDataOff) == 0 {
				db.dict.Del(srcKeyBytes)
				db.FreeObject(srcHeadOff)
			}
		}
	} else if srcEnc == ObjEncodingHashtable {
		found = false
		innerDict := LoadDictMeta(db.arena, srcDataOff)
		if innerDict.Del(mb) {
			found = true
			innerDict.StoreMeta(srcDataOff)
			if innerDict.used == 0 {
				db.dict.Del(srcKeyBytes)
				db.FreeObject(srcHeadOff)
			}
		}
	}

	if !found {
		return false
	}

	dstKeyBytes := []byte(dstKey)
	dstHeadOff, dstOk := db.dict.Get(dstKeyBytes)

	if dstOk {
		dstEnc := db.ObjectEncoding(dstHeadOff)
		dstDataOff := db.ObjectDataOffset(dstHeadOff)

		if dstEnc == ObjEncodingIntset {
			val := int64(binary.LittleEndian.Uint64(mb))
			if intsetFind(db.arena, dstDataOff, val) {
				return true
			}
			intsetAdd(db.arena, &dstDataOff, val)
			db.ObjectSetDataOffset(dstHeadOff, dstDataOff)
		} else if dstEnc == ObjEncodingHashtable {
			innerDict := LoadDictMeta(db.arena, dstDataOff)
			if _, exists := innerDict.Get(mb); !exists {
				innerDict.Set(mb, 0)
				innerDict.StoreMeta(dstDataOff)
			}
		}
	} else {
		innerDict := NewDict(db.arena)
		innerDict.Set(mb, 0)
		sdOff := db.arena.Alloc(12)
		innerDict.StoreMeta(sdOff)
		dstHeadOff = db.NewObject(ObjSet, ObjEncodingHashtable, sdOff)
		db.dict.Set(dstKeyBytes, dstHeadOff)
	}

	return true
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

	newEnc := enc
	if value < -32768 || value > 32767 {
		newEnc = 8
	} else if value < -128 || value > 127 {
		newEnc = 4
	}

	if newEnc > enc {
		newSize := 6 + (n+1)*newEnc
		newOff := arena.Alloc(newSize)
		arena.WriteUint16(intsetEncodingOff(arena, newOff), uint16(newEnc))
		arena.WriteUint32(intsetLengthOff(arena, newOff), uint32(n+1))
		newContentsOff := intsetContentsOff(arena, newOff)

		inserted := false
		di := 0
		for si := 0; si < n; si++ {
			oldVal := intsetGet(arena, *isOff, si)
			if !inserted && value < oldVal {
				writeInt(arena, newContentsOff+di*newEnc, newEnc, value)
				di++
				inserted = true
			}
			writeInt(arena, newContentsOff+di*newEnc, newEnc, oldVal)
			di++
		}
		if !inserted {
			writeInt(arena, newContentsOff+di*newEnc, newEnc, value)
		}

		arena.Free(*isOff)
		*isOff = newOff
		return true
	}

	insertPos := n
	for i := 0; i < n; i++ {
		if intsetGet(arena, *isOff, i) > value {
			insertPos = i
			break
		}
	}

	newSize := 6 + (n+1)*newEnc
	newOff := arena.Alloc(newSize)
	arena.WriteUint16(intsetEncodingOff(arena, newOff), uint16(newEnc))
	arena.WriteUint32(intsetLengthOff(arena, newOff), uint32(n+1))
	newContentsOff := intsetContentsOff(arena, newOff)

	for i := 0; i < insertPos; i++ {
		writeInt(arena, newContentsOff+i*newEnc, newEnc, intsetGet(arena, *isOff, i))
	}
	writeInt(arena, newContentsOff+insertPos*newEnc, newEnc, value)
	for i := insertPos; i < n; i++ {
		writeInt(arena, newContentsOff+(i+1)*newEnc, newEnc, intsetGet(arena, *isOff, i))
	}

	arena.Free(*isOff)
	*isOff = newOff

	return true
}

func intsetRemove(arena *Arena, isOff *int, value int64) bool {
	if !intsetFind(arena, *isOff, value) {
		return false
	}

	n := intsetLen(arena, *isOff)
	enc := intsetEncoding(arena, *isOff)

	newSize := 6 + (n-1)*enc
	newOff := arena.Alloc(newSize)
	arena.WriteUint16(intsetEncodingOff(arena, newOff), uint16(enc))
	arena.WriteUint32(intsetLengthOff(arena, newOff), uint32(n-1))
	newContentsOff := intsetContentsOff(arena, newOff)

	found := false
	di := 0
	for si := 0; si < n; si++ {
		oldVal := intsetGet(arena, *isOff, si)
		if !found && oldVal == value {
			found = true
			continue
		}
		writeInt(arena, newContentsOff+di*enc, enc, oldVal)
		di++
	}

	arena.Free(*isOff)
	*isOff = newOff

	return true
}

func writeInt(arena *Arena, off int, enc int, val int64) {
	switch enc {
	case 2:
		arena.WriteUint16(off, uint16(val))
	case 4:
		arena.WriteUint32(off, uint32(val))
	case 8:
		arena.WriteUint64(off, uint64(val))
	}
}
