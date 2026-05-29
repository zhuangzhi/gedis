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
//
// String 命令实现: SET, GET, APPEND, GETRANGE, SETRANGE, STRLEN, INCRBY, INCRBYFLOAT.

package gedis

import "strconv"

// Set 设置 key 的值为 value。
// 对外友好 API，入参使用 []byte，方便 goja 调用。
func (db *RedisDB) Set(key string, value []byte) {
	pb := BufFromBytes(value)
	db.SetBuffer(key, pb)
	pb.Close()
}

// SetBuffer 设置 key 的值，入参使用 *PooledBuffer 避免堆分配。
// 调用方通过 Buf(s) 获取，传入后应立即 pb.Close() 归还池。
func (db *RedisDB) SetBuffer(key string, value *PooledBuffer) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	valOff := db.arena.AllocBytes(value.Bytes())
	headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
	db.dict.Set(keyBytes, headOff)
}

// Get 获取 key 的值。返回 *PooledBuffer，调用方用完后必须 pb.Close()。
func (db *RedisDB) Get(key string) (*PooledBuffer, bool) {
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
		s := strconv.FormatInt(val, 10)
		pb := NewBuf(len(s))
		pb.buf.WriteString(s)
		return pb, true
	}

	if dataOff == 0 {
		return nil, false
	}

	size := db.arena.SizeAt(dataOff)
	pb := NewBuf(size)
	pb.buf.Write(db.arena.ReadBytes(dataOff, size))
	return pb, true
}

// Append 在已有 key 值末尾追加 value。对外友好 API，入参 []byte。
func (db *RedisDB) Append(key string, value []byte) int {
	pb := BufFromBytes(value)
	result := db.AppendBuffer(key, pb)
	pb.Close()
	return result
}

// AppendBuffer 入参使用 *PooledBuffer 避免堆分配。
func (db *RedisDB) AppendBuffer(key string, value *PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	valBytes := value.Bytes()

	if !ok {
		valOff := db.arena.AllocBytes(valBytes)
		headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		return len(valBytes)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingInt {
		oldVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		oldStr := strconv.FormatInt(oldVal, 10)
		newData := append([]byte(oldStr), valBytes...)
		db.arena.Free(dataOff)
		newOff := db.arena.AllocBytes(newData)
		db.ObjectSetDataOffset(headOff, newOff)
		db.ObjectSetEncoding(headOff, ObjEncodingRaw)
		return len(newData)
	}

	if dataOff == 0 {
		newOff := db.arena.AllocBytes(valBytes)
		db.ObjectSetDataOffset(headOff, newOff)
		return len(valBytes)
	}

	oldSize := db.arena.SizeAt(dataOff)
	oldData := db.arena.ReadBytes(dataOff, oldSize)
	newData := append(oldData, valBytes...)

	db.arena.Free(dataOff)
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)

	return len(newData)
}

// GetRange 返回 key 值的子字符串 [start, end]。返回 *PooledBuffer。
func (db *RedisDB) GetRange(key string, start, end int) (*PooledBuffer, bool) {
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

	var data []byte
	if enc == ObjEncodingInt {
		val := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		data = []byte(strconv.FormatInt(val, 10))
	} else if dataOff != 0 {
		size := db.arena.SizeAt(dataOff)
		data = db.arena.ReadBytes(dataOff, size)
	} else {
		return nil, false
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
	if start > end || start >= size {
		return nil, false
	}

	pb := NewBuf(end - start + 1)
	pb.buf.Write(data[start : end+1])
	return pb, true
}

// SetRange 覆盖 key 值从 offset 开始的部分。对外友好 API，入参 []byte。
func (db *RedisDB) SetRange(key string, offset int, value []byte) int {
	pb := BufFromBytes(value)
	result := db.SetRangeBuffer(key, offset, pb)
	pb.Close()
	return result
}

// SetRangeBuffer 入参使用 *PooledBuffer 避免堆分配。
func (db *RedisDB) SetRangeBuffer(key string, offset int, value *PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	valBytes := value.Bytes()

	if !ok {
		valOff := db.arena.AllocBytes(valBytes)
		headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		return len(valBytes)
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

	newLen := offset + len(valBytes)
	if newLen < len(oldData) {
		newLen = len(oldData)
	}

	newData := make([]byte, newLen)
	copy(newData, oldData)
	copy(newData[offset:], valBytes)

	if dataOff != 0 {
		db.arena.Free(dataOff)
	}
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)
	db.ObjectSetEncoding(headOff, ObjEncodingRaw)

	return newLen
}

// Strlen 返回 key 值的字节长度。
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

// IncrBy 将 key 的整数值增加 inc。若 key 不存在则设为 inc。
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

// IncrByFloat 将 key 的浮点值增加 inc。
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

func (db *RedisDB) MGet(keys ...string) []*PooledBuffer {
	db.mu.RLock()
	defer db.mu.RUnlock()

	results := make([]*PooledBuffer, len(keys))
	for i, key := range keys {
		keyBytes := []byte(key)
		headOff, ok := db.dict.Get(keyBytes)
		if !ok {
			results[i] = nil
			continue
		}

		enc := db.ObjectEncoding(headOff)
		if enc == ObjEncodingInt {
			intVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
			buf := Buf(strconv.FormatInt(intVal, 10))
			results[i] = buf
		} else {
			dataOff := db.ObjectDataOffset(headOff)
			if dataOff == 0 {
				results[i] = nil
				continue
			}
			size := db.arena.SizeAt(dataOff)
			data := db.arena.ReadBytes(dataOff, size)
			buf := Buf(string(data))
			results[i] = buf
		}
	}
	return results
}

func (db *RedisDB) MSet(keyValues map[string]*PooledBuffer) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for key, value := range keyValues {
		keyBytes := []byte(key)
		headOff, ok := db.dict.Get(keyBytes)

		if !ok {
			valOff := db.arena.AllocBytes(value.Bytes())
			headOff = db.NewObject(ObjString, ObjEncodingRaw, valOff)
			db.dict.Set(keyBytes, headOff)
		} else {
			enc := db.ObjectEncoding(headOff)
			if enc == ObjEncodingInt {
				db.ObjectSetEncoding(headOff, ObjEncodingRaw)
			}
			dataOff := db.ObjectDataOffset(headOff)
			if dataOff != 0 {
				db.arena.Free(dataOff)
			}
			newOff := db.arena.AllocBytes(value.Bytes())
			db.ObjectSetDataOffset(headOff, newOff)
		}
	}
}

func (db *RedisDB) MSetNX(keyValues map[string]*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	inserted := 0
	for key, value := range keyValues {
		keyBytes := []byte(key)
		_, ok := db.dict.Get(keyBytes)
		if ok {
			continue
		}

		valOff := db.arena.AllocBytes(value.Bytes())
		headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
		db.dict.Set(keyBytes, headOff)
		inserted++
	}
	return inserted
}

func (db *RedisDB) SetNX(key string, value *PooledBuffer) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	_, ok := db.dict.Get(keyBytes)
	if ok {
		return false
	}

	valOff := db.arena.AllocBytes(value.Bytes())
	headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
	db.dict.Set(keyBytes, headOff)
	return true
}

func (db *RedisDB) Decr(key string) (int64, error) {
	return db.IncrBy(key, -1)
}

func (db *RedisDB) DecrBy(key string, dec int64) (int64, error) {
	return db.IncrBy(key, -dec)
}

func (db *RedisDB) SetEx(key string, seconds int, value []byte) {
	pb := BufFromBytes(value)
	db.SetExBuffer(key, seconds, pb)
	pb.Close()
}

func (db *RedisDB) SetExBuffer(key string, seconds int, value *PooledBuffer) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	valOff := db.arena.AllocBytes(value.Bytes())
	headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) PsetEx(key string, milliseconds int64, value []byte) {
	pb := BufFromBytes(value)
	db.PsetExBuffer(key, milliseconds, pb)
	pb.Close()
}

func (db *RedisDB) PsetExBuffer(key string, milliseconds int64, value *PooledBuffer) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	valOff := db.arena.AllocBytes(value.Bytes())
	headOff := db.NewObject(ObjString, ObjEncodingRaw, valOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) GetDel(key string) (*PooledBuffer, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

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

	var pb *PooledBuffer
	if enc == ObjEncodingInt {
		val := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		s := strconv.FormatInt(val, 10)
		pb = NewBuf(len(s))
		pb.buf.WriteString(s)
	} else if dataOff != 0 {
		size := db.arena.SizeAt(dataOff)
		pb = NewBuf(size)
		pb.buf.Write(db.arena.ReadBytes(dataOff, size))
	} else {
		return nil, false
	}

	db.dict.Del([]byte(key))
	db.FreeObject(headOff)
	return pb, true
}
