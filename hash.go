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

func (db *RedisDB) HSet(key string, field string, value []byte) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		zlOff = ziplistInsert(db.arena, zlOff, value, false)
		headOff = db.NewObject(ObjHash, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return 1
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		for i := 0; i < n; i += 2 {
			f := ziplistGet(db.arena, zlOff, i)
			if string(f) == field {
				zlOff = ziplistDelete(db.arena, zlOff, i+1)
				zlOff = ziplistInsertAt(db.arena, zlOff, i+1, value)
				db.ObjectSetDataOffset(headOff, zlOff)
				return 0
			}
		}

		zlOff = ziplistInsert(db.arena, zlOff, []byte(field), false)
		zlOff = ziplistInsert(db.arena, zlOff, value, false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return 1
	}

	return 0
}

func (db *RedisDB) HGet(key string, field string) ([]byte, bool) {
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
				return ziplistGet(db.arena, zlOff, i+1), true
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

func (db *RedisDB) HGetAll(key string) map[string][]byte {
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
		result := make(map[string][]byte, n/2)
		for i := 0; i < n; i += 2 {
			f := ziplistGet(db.arena, zlOff, i)
			v := ziplistGet(db.arena, zlOff, i+1)
			result[string(f)] = v
		}
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

	oldZlOff := zlOff

	relInsertPos := ziplistHeaderSize
	for i := 0; i < index; i++ {
		relInsertPos += ziplistEntryTotalSize(arena, oldZlOff+relInsertPos)
	}

	var prevLen int
	if index > 0 {
		relPrevPos := ziplistHeaderSize
		for i := 0; i < index-1; i++ {
			relPrevPos += ziplistEntryTotalSize(arena, oldZlOff+relPrevPos)
		}
		prevLen = ziplistEntryTotalSize(arena, oldZlOff+relPrevPos)
	} else {
		prevLen = 0
	}

	entrySize := ziplistEntrySize(prevLen, data)

	oldSize := ziplistTotalBytes(arena, zlOff)
	newSize := oldSize + entrySize
	zlOff = ziplistResize(arena, zlOff, newSize)

	absInsertPos := zlOff + relInsertPos

	remainSize := oldSize - relInsertPos - 1
	if remainSize > 0 {
		src := arena.ReadBytes(absInsertPos, remainSize)
		arena.WriteBytes(absInsertPos+entrySize, src)
	}

	ziplistWriteEntry(arena, absInsertPos, prevLen, data)

	if index < n {
		nextEntryOff := absInsertPos + entrySize
		if nextEntryOff < zlOff+newSize-1 {
			ziplistWritePrevLen(arena, nextEntryOff, entrySize)
		}
	}

	ziplistSetNumEntries(arena, zlOff, n+1)
	if index >= n {
		ziplistSetTailOffset(arena, zlOff, relInsertPos)
	} else {
		ziplistSetTailOffset(arena, zlOff, ziplistTailOffset(arena, zlOff)+entrySize)
	}

	return zlOff
}

