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
	"strconv"
)

func (db *RedisDB) Set(key string, value []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	valOff := db.arena.AllocBytes(value)
	headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) Get(key string) ([]byte, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil, false
	}

	typ := db.ObjectType(headOff)
	if typ != ObjString && typ != ObjBitmap {
		return nil, false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingInt {
		val := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(val))
		return []byte(strconv.FormatInt(val, 10)), true
	}

	if dataOff == 0 {
		return nil, false
	}

	size := db.arena.SizeAt(dataOff)
	return db.arena.ReadBytes(dataOff, size), true
}

func (db *RedisDB) Append(key string, value []byte) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		valOff := db.arena.AllocBytes(value)
		headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		return len(value)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingInt {
		oldVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		oldStr := strconv.FormatInt(oldVal, 10)
		newData := append([]byte(oldStr), value...)
		db.arena.Free(dataOff)
		newOff := db.arena.AllocBytes(newData)
		db.ObjectSetDataOffset(headOff, newOff)
		db.ObjectSetEncoding(headOff, ObjEncodingRaw)
		return len(newData)
	}

	if dataOff == 0 {
		newOff := db.arena.AllocBytes(value)
		db.ObjectSetDataOffset(headOff, newOff)
		return len(value)
	}

	oldSize := db.arena.SizeAt(dataOff)
	oldData := db.arena.ReadBytes(dataOff, oldSize)
	newData := append(oldData, value...)

	db.arena.Free(dataOff)
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)

	return len(newData)
}

func (db *RedisDB) GetRange(key string, start, end int) []byte {
	db.mu.RLock()
	defer db.mu.RUnlock()

	data, ok := db.Get(key)
	if !ok {
		return nil
	}

	size := len(data)
	if start < 0 {
		start = size + start
	}
	if end < 0 {
		end = size + end
	}
	if start < 0 {
		start = 0
	}
	if end >= size {
		end = size - 1
	}
	if start > end {
		return nil
	}

	return data[start : end+1]
}

func (db *RedisDB) SetRange(key string, offset int, value []byte) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		valOff := db.arena.AllocBytes(value)
		headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		return len(value)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	var oldData []byte
	if enc == ObjEncodingInt {
		oldVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		oldData = []byte(strconv.FormatInt(oldVal, 10))
	} else if dataOff != 0 {
		oldSize := db.arena.SizeAt(dataOff)
		oldData = db.arena.ReadBytes(dataOff, oldSize)
	}

	newLen := offset + len(value)
	if newLen < len(oldData) {
		newLen = len(oldData)
	}

	newData := make([]byte, newLen)
	copy(newData, oldData)
	copy(newData[offset:], value)

	if dataOff != 0 {
		db.arena.Free(dataOff)
	}
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)
	db.ObjectSetEncoding(headOff, ObjEncodingRaw)

	return newLen
}

func (db *RedisDB) Strlen(key string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	if enc == ObjEncodingInt {
		val := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		return len(strconv.FormatInt(val, 10))
	}

	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return 0
	}
	return db.arena.SizeAt(dataOff)
}

func (db *RedisDB) IncrBy(key string, inc int64) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		headOff = db.NewObject(ObjString, ObjEncodingInt, 0)
		db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(inc))
		db.dict.Set(keyBytes, headOff)
		return inc, nil
	}

	enc := db.ObjectEncoding(headOff)

	if enc == ObjEncodingInt {
		oldVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		newVal := oldVal + inc
		db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(newVal))
		return newVal, nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		db.ObjectSetEncoding(headOff, ObjEncodingInt)
		db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(inc))
		return inc, nil
	}

	size := db.arena.SizeAt(dataOff)
	strVal := string(db.arena.ReadBytes(dataOff, size))
	oldVal, err := strconv.ParseInt(strVal, 10, 64)
	if err != nil {
		return 0, err
	}

	newVal := oldVal + inc
	db.arena.Free(dataOff)
	db.ObjectSetEncoding(headOff, ObjEncodingInt)
	db.arena.WriteUint64(objHeaderDataOffset(headOff), uint64(newVal))

	return newVal, nil
}

func (db *RedisDB) IncrByFloat(key string, inc float64) (float64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		newStr := strconv.FormatFloat(inc, 'f', -1, 64)
		valOff := db.arena.AllocBytes([]byte(newStr))
		headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		return inc, nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	var oldVal float64
	if enc == ObjEncodingInt {
		intVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		oldVal = float64(intVal)
	} else if dataOff != 0 {
		size := db.arena.SizeAt(dataOff)
		var err error
		oldVal, err = strconv.ParseFloat(string(db.arena.ReadBytes(dataOff, size)), 64)
		if err != nil {
			return 0, err
		}
	}

	newVal := oldVal + inc
	newStr := strconv.FormatFloat(newVal, 'f', -1, 64)

	if dataOff != 0 {
		db.arena.Free(dataOff)
	}
	newOff := db.arena.AllocBytes([]byte(newStr))
	db.ObjectSetDataOffset(headOff, newOff)
	db.ObjectSetEncoding(headOff, ObjEncodingRaw)

	return newVal, nil
}

