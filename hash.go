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

import "strconv"

// HSet 设置哈希表中的字段值。对外友好 API，value 入参 []byte。
func (db *RedisDB) HSet(key string, field string, value []byte) int {
	pb := BufFromBytes(value)
	result := db.HSetBuffer(key, field, pb)
	pb.Close()
	return result
}

// HSetBuffer 设置哈希表中的字段值，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) HSetBuffer(key string, field string, value *PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	fieldBytes := []byte(field)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, fieldBytes, false)
		zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return 1
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, fieldBytes) {
				zlOff = ziplistDelete(db.arena, zlOff, i+1)
				zlOff = ziplistInsertAt(db.arena, zlOff, i+1, value.Bytes())
				db.ObjectSetDataOffset(headOff, zlOff)
				return 0
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}

		zlOff = ziplistInsert(db.arena, zlOff, fieldBytes, false)
		zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return 1
	}

	return 0
}

// HGet 获取哈希表中指定字段的值。返回 *PooledBuffer。
func (db *RedisDB) HGet(key string, field string) (*PooledBuffer, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
				val := ziplistGet(db.arena, zlOff, i+1)
				pb := NewBuf(len(val))
				pb.buf.Write(val)
				return pb, true
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}
		return nil, false
	}

	return nil, false
}

func (db *RedisDB) HDel(key string, fields ...string) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		deleted := 0
		for _, field := range fields {
			n := ziplistLen(db.arena, zlOff)
			pos := zlOff + ziplistHeaderSize
			for i := 0; i < n; i += 2 {
				if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
					zlOff = ziplistDelete(db.arena, zlOff, i+1)
					zlOff = ziplistDelete(db.arena, zlOff, i)
					deleted++
					break
				}
				pos += ziplistEntryTotalSize(db.arena, pos)
				pos += ziplistEntryTotalSize(db.arena, pos)
			}
		}
		db.ObjectSetDataOffset(headOff, zlOff)
		if ziplistLen(db.arena, zlOff) == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
		return deleted
	}

	return 0
}

// HGetAll 获取哈希表中的所有字段和值。返回 *ZSlices，
// 格式为 [field1, value1, field2, value2, ...]，遍历后须 zs.Close()。
func (db *RedisDB) HGetAll(key string) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		result := NewZSlices()
		for i := 0; i < n; i++ {
			v := ziplistGet(db.arena, zlOff, i)
			result.Add(v)
		}
		result.Finish()
		return result
	}

	return nil
}

func (db *RedisDB) HIncrBy(key string, field string, inc int64) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		valStr := strconv.FormatInt(inc, 10)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(valStr), false)
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return inc, nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
				v := ziplistGet(db.arena, zlOff, i+1)
				oldVal, err := strconv.ParseInt(string(v), 10, 64)
				if err != nil {
					return 0, err
				}
				newVal := oldVal + inc
				newStr := strconv.FormatInt(newVal, 10)
				zlOff = ziplistDelete(db.arena, zlOff, i+1)
				zlOff = ziplistInsertAt(db.arena, zlOff, i+1, []byte(newStr))
				db.ObjectSetDataOffset(headOff, zlOff)
				return newVal, nil
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}

		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		valStr := strconv.FormatInt(inc, 10)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(valStr), false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return inc, nil
	}

	return 0, nil
}

func (db *RedisDB) HStrLen(key string, field string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
				v := ziplistGet(db.arena, zlOff, i+1)
				return len(v)
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}
	}
	return 0
}

func (db *RedisDB) HRandField(key string, count int) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		if n == 0 {
			return nil
		}

		result := make([]*PooledBuffer, 0, count)
		for i := 0; i < count && i < n/2; i++ {
			idx := (i * 7) % (n / 2)
			pos := zlOff + ziplistHeaderSize
			for j := 0; j < idx*2; j++ {
				pos += ziplistEntryTotalSize(db.arena, pos)
			}
			v := ziplistGet(db.arena, zlOff, idx*2)
			pb := NewBuf(len(v))
			pb.buf.Write(v)
			result = append(result, pb)
		}
		return result
	}
	return nil
}

func (db *RedisDB) HExists(key string, field string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, ok := db.HGet(key, field)
	return ok
}

func (db *RedisDB) HLen(key string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		return ziplistLen(db.arena, dataOff) / 2
	}

	return 0
}

func ziplistInsertAt(arena *Arena, zlOff int, index int, data []byte) int {
	if index < 0 {
		return zlOff
	}
	n := ziplistNumEntries(arena, zlOff)
	if index >= n {
		return ziplistInsert(arena, zlOff, data, false)
	}

	oldSize := ziplistTotalBytes(arena, zlOff)
	oldTail := ziplistTailOffset(arena, zlOff)

	insertOff := zlOff + ziplistHeaderSize
	for i := 0; i < index; i++ {
		insertOff += ziplistEntryTotalSize(arena, insertOff)
	}

	prevLen := 0
	if index > 0 {
		prevEntryOff := zlOff + ziplistHeaderSize
		for i := 0; i < index-1; i++ {
			prevEntryOff += ziplistEntryTotalSize(arena, prevEntryOff)
		}
		prevLen = ziplistEntryTotalSize(arena, prevEntryOff)
	}

	entrySize := ziplistEntrySize(prevLen, data)
	newSize := oldSize + entrySize

	headerSize := ziplistHeaderSize
	prefixSize := insertOff - (zlOff + headerSize)
	suffixSize := oldSize - headerSize - prefixSize - 1

	newOff := arena.Alloc(newSize)

	arena.WriteBytes(newOff, arena.GetSlice(zlOff, headerSize))

	pos := newOff + headerSize
	if prefixSize > 0 {
		arena.WriteBytes(pos, arena.GetSlice(zlOff+headerSize, prefixSize))
		pos += prefixSize
	}

	pos += ziplistWriteEntry(arena, pos, prevLen, data)

	if suffixSize > 0 {
		firstOff := insertOff
		_, oldPrevSize := ziplistEntryPrevLen(arena, firstOff)
		firstTotal := ziplistEntryTotalSize(arena, firstOff)
		firstDataOff := firstOff + oldPrevSize
		firstDataSize := firstTotal - oldPrevSize

		pos += ziplistWritePrevLen(arena, pos, entrySize)
		arena.WriteBytes(pos, arena.GetSlice(firstDataOff, firstDataSize))
		pos += firstDataSize

		remainingOff := firstOff + firstTotal
		remainingSize := suffixSize - firstTotal
		if remainingSize > 0 {
			arena.WriteBytes(pos, arena.GetSlice(remainingOff, remainingSize))
			pos += remainingSize
		}
	}

	arena.WriteByte(pos, ziplistEndByte)

	ziplistSetTotalBytes(arena, newOff, newSize)
	ziplistSetNumEntries(arena, newOff, n+1)
	if index == n {
		ziplistSetTailOffset(arena, newOff, ziplistHeaderSize+prefixSize)
	} else {
		ziplistSetTailOffset(arena, newOff, oldTail+entrySize)
	}

	arena.Free(zlOff)
	return newOff
}

func (db *RedisDB) HMSet(key string, keyValues map[string]*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	var zlOff int
	if !ok {
		zlOff = ziplistNew(db.arena)
		for field, value := range keyValues {
			zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
			zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		}
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return len(keyValues)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff = dataOff
		for field, value := range keyValues {
			idx := ziplistFind(db.arena, zlOff, []byte(field))
			if idx != -1 {
				zlOff = ziplistDelete(db.arena, zlOff, idx)
				zlOff = ziplistDelete(db.arena, zlOff, idx)
			}
			zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
			zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		}
		db.ObjectSetDataOffset(headOff, zlOff)
	}
	return len(keyValues)
}

func (db *RedisDB) HMGet(key string, fields ...string) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		results := make([]*PooledBuffer, len(fields))
		for i := range results {
			results[i] = nil
		}
		return results
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	results := make([]*PooledBuffer, len(fields))
	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize

		for i, field := range fields {
			found := false
			scanPos := pos
			for j := 0; j < n; j += 2 {
				if ziplistEntryDataEquals(db.arena, scanPos, []byte(field)) {
					valBytes := ziplistGet(db.arena, zlOff, j+1)
					if valBytes != nil {
						results[i] = Buf(string(valBytes))
					}
					found = true
					break
				}
				scanPos += ziplistEntryTotalSize(db.arena, scanPos)
				scanPos += ziplistEntryTotalSize(db.arena, scanPos)
			}
			if !found {
				results[i] = nil
			}
		}
	}
	return results
}

func (db *RedisDB) HKeys(key string) [][]byte {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		keys := make([][]byte, 0, n/2)
		for i := 0; i < n; i += 2 {
			keyBytes := ziplistGet(db.arena, zlOff, i)
			if keyBytes != nil {
				keys = append(keys, keyBytes)
			}
		}
		return keys
	}
	return nil
}

func (db *RedisDB) HVals(key string) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		vals := make([]*PooledBuffer, 0, n/2)
		for i := 1; i < n; i += 2 {
			valBytes := ziplistGet(db.arena, zlOff, i)
			if valBytes != nil {
				vals = append(vals, Buf(string(valBytes)))
			}
		}
		return vals
	}
	return nil
}

func (db *RedisDB) HSetNX(key string, field string, value *PooledBuffer) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return true
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
				return false
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}

		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		zlOff = ziplistInsert(db.arena, zlOff, value.Bytes(), false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return true
	}
	return false
}

func (db *RedisDB) HIncrByFloat(key string, field string, inc float64) (float64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		valStr := strconv.FormatFloat(inc, 'f', -1, 64)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(valStr), false)
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return inc, nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, []byte(field)) {
				v := ziplistGet(db.arena, zlOff, i+1)
				oldVal, err := strconv.ParseFloat(string(v), 64)
				if err != nil {
					return 0, err
				}
				newVal := oldVal + inc
				newStr := strconv.FormatFloat(newVal, 'f', -1, 64)
				zlOff = ziplistDelete(db.arena, zlOff, i+1)
				zlOff = ziplistInsertAt(db.arena, zlOff, i+1, []byte(newStr))
				db.ObjectSetDataOffset(headOff, zlOff)
				return newVal, nil
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}

		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		valStr := strconv.FormatFloat(inc, 'f', -1, 64)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(valStr), false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return inc, nil
	}
	return 0, nil
}
