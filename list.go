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
	listMaxZiplistEntries = 512
	listMaxZiplistValue   = 64
)

func listValToBuf(val []byte) *PooledBuffer {
	pb := NewBuf(len(val))
	pb.buf.Write(val)
	return pb
}

// LPush 向列表头部插入元素。对外友好 API，入参 []byte。
func (db *RedisDB) LPush(key string, values ...[]byte) int {
	bufs := make([]*PooledBuffer, len(values))
	for i, v := range values {
		bufs[i] = BufFromBytes(v)
	}
	result := db.LPushBuffer(key, bufs...)
	for _, b := range bufs {
		b.Close()
	}
	return result
}

// LPushBuffer 向列表头部插入元素，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) LPushBuffer(key string, values ...*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		for _, v := range values {
			zlOff = ziplistInsert(db.arena, zlOff, v.Bytes(), true)
		}
		headOff = db.NewObject(ObjList, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return ziplistLen(db.arena, zlOff)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		for _, v := range values {
			zlOff = ziplistInsert(db.arena, zlOff, v.Bytes(), true)
		}
		db.ObjectSetDataOffset(headOff, zlOff)
		return ziplistLen(db.arena, zlOff)
	}

	return 0
}

// RPush 向列表尾部插入元素。对外友好 API，入参 []byte。
func (db *RedisDB) RPush(key string, values ...[]byte) int {
	bufs := make([]*PooledBuffer, len(values))
	for i, v := range values {
		bufs[i] = BufFromBytes(v)
	}
	result := db.RPushBuffer(key, bufs...)
	for _, b := range bufs {
		b.Close()
	}
	return result
}

// RPushBuffer 向列表尾部插入元素，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) RPushBuffer(key string, values ...*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		for _, v := range values {
			zlOff = ziplistInsert(db.arena, zlOff, v.Bytes(), false)
		}
		headOff = db.NewObject(ObjList, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return ziplistLen(db.arena, zlOff)
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		for _, v := range values {
			zlOff = ziplistInsert(db.arena, zlOff, v.Bytes(), false)
		}
		db.ObjectSetDataOffset(headOff, zlOff)
		return ziplistLen(db.arena, zlOff)
	}

	return 0
}

// LPop 移除并返回列表头部元素。返回 *PooledBuffer。
func (db *RedisDB) LPop(key string) (*PooledBuffer, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

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
		if n == 0 {
			return nil, false
		}
		val := ziplistGet(db.arena, zlOff, 0)
		pb := listValToBuf(val)
		zlOff = ziplistDelete(db.arena, zlOff, 0)
		db.ObjectSetDataOffset(headOff, zlOff)
		if ziplistLen(db.arena, zlOff) == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
		return pb, true
	}

	return nil, false
}

// RPop 移除并返回列表尾部元素。返回 *PooledBuffer。
func (db *RedisDB) RPop(key string) (*PooledBuffer, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

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
		if n == 0 {
			return nil, false
		}
		val := ziplistGet(db.arena, zlOff, n-1)
		pb := listValToBuf(val)
		zlOff = ziplistDelete(db.arena, zlOff, n-1)
		db.ObjectSetDataOffset(headOff, zlOff)
		if ziplistLen(db.arena, zlOff) == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
		return pb, true
	}

	return nil, false
}

// LIndex 获取列表中指定索引的元素。返回 *PooledBuffer。
func (db *RedisDB) LIndex(key string, index int) (*PooledBuffer, bool) {
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
		if index < 0 {
			index = n + index
		}
		if index < 0 || index >= n {
			return nil, false
		}
		val := ziplistGet(db.arena, zlOff, index)
		return listValToBuf(val), true
	}

	return nil, false
}

// LRange 获取列表中指定范围的元素。返回 *ZSlices，遍历后须 zs.Close()。
func (db *RedisDB) LRange(key string, start, stop int) *ZSlices {
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
		if start < 0 {
			start = n + start
		}
		if stop < 0 {
			stop = n + stop
		}
		if start < 0 {
			start = 0
		}
		if stop >= n {
			stop = n - 1
		}
		if start > stop {
			return nil
		}

		result := NewZSlices()
		for i := start; i <= stop; i++ {
			val := ziplistGet(db.arena, zlOff, i)
			result.Add(val)
		}
		result.Finish()
		return result
	}

	return nil
}

func (db *RedisDB) LLen(key string) int {
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
		return ziplistLen(db.arena, dataOff)
	}

	return 0
}
