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
	"sort"
	"strconv"
)

const (
	ObjArray uint8 = 17
)

// ArraySet 向数组设置值
func (db *RedisDB) ArraySet(key string, index int64, value []byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	idxStr := strconv.FormatInt(index, 10)
	valOff := db.arena.AllocBytes(value)
	arrDict.Set([]byte(idxStr), valOff)
	return true
}

// ArrayGet 获取数组指定索引的值
func (db *RedisDB) ArrayGet(key string, index int64) (*PooledBuffer, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	if db.isExpired(keyBytes, headOff) {
		return nil, false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return nil, false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return nil, false
	}

	idxStr := strconv.FormatInt(index, 10)
	valOff, ok := arrDict.Get([]byte(idxStr))
	if !ok {
		return nil, false
	}

	size := db.arena.SizeAt(valOff)
	data := db.arena.ReadBytes(valOff, size)
	return BufFromBytes(data), true
}

// ArrayLen 获取数组长度
func (db *RedisDB) ArrayLen(key string) int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return -1
	}

	if db.isExpired(keyBytes, headOff) {
		return -1
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return -1
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return 0
	}

	return int64(arrDict.Len())
}

// ArrayAppend 向数组末尾添加值
func (db *RedisDB) ArrayAppend(key string, value []byte) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return -1
	}

	if db.isExpired(keyBytes, headOff) {
		return -1
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return -1
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return -1
	}

	idx := arrDict.Len()
	idxStr := strconv.Itoa(idx)
	valOff := db.arena.AllocBytes(value)
	arrDict.Set([]byte(idxStr), valOff)
	arrDict.StoreMeta(dataOff)

	return int64(idx)
}

// ArrayPop 移除并返回数组最后一个元素
func (db *RedisDB) ArrayPop(key string) (*PooledBuffer, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	if db.isExpired(keyBytes, headOff) {
		return nil, false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return nil, false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return nil, false
	}

	len := arrDict.Len()
	if len == 0 {
		return nil, false
	}

	idx := len - 1
	idxStr := strconv.Itoa(idx)
	valOff, ok := arrDict.Get([]byte(idxStr))
	if !ok {
		return nil, false
	}

	size := db.arena.SizeAt(valOff)
	data := db.arena.ReadBytes(valOff, size)
	arrDict.Del([]byte(idxStr))
	arrDict.StoreMeta(dataOff)

	return BufFromBytes(data), true
}

// ArrayShift 移除并返回数组第一个元素
func (db *RedisDB) ArrayShift(key string) (*PooledBuffer, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	if db.isExpired(keyBytes, headOff) {
		return nil, false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return nil, false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return nil, false
	}

	len := arrDict.Len()
	if len == 0 {
		return nil, false
	}

	idxStr := "0"
	valOff, ok := arrDict.Get([]byte(idxStr))
	if !ok {
		return nil, false
	}

	size := db.arena.SizeAt(valOff)
	data := db.arena.ReadBytes(valOff, size)

	for i := 0; i < len-1; i++ {
		srcIdx := strconv.Itoa(i + 1)
		dstIdx := strconv.Itoa(i)
		srcOff, ok := arrDict.Get([]byte(srcIdx))
		if ok {
			arrDict.Set([]byte(dstIdx), srcOff)
		}
	}
	arrDict.Del([]byte(strconv.Itoa(len - 1)))
	arrDict.StoreMeta(dataOff)

	return BufFromBytes(data), true
}

// ArrayInsert 在指定位置插入值
func (db *RedisDB) ArrayInsert(key string, index int64, value []byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()
	idx := int(index)
	if idx < 0 {
		idx += len
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len {
		idx = len
	}

	for i := len; i > idx; i-- {
		srcIdx := strconv.Itoa(i - 1)
		dstIdx := strconv.Itoa(i)
		srcOff, ok := arrDict.Get([]byte(srcIdx))
		if ok {
			arrDict.Set([]byte(dstIdx), srcOff)
		}
	}

	idxStr := strconv.Itoa(idx)
	valOff := db.arena.AllocBytes(value)
	arrDict.Set([]byte(idxStr), valOff)
	arrDict.StoreMeta(dataOff)

	return true
}

// ArrayDel 删除数组指定索引的值
func (db *RedisDB) ArrayDel(key string, index int64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()
	idx := int(index)
	if idx < 0 || idx >= len {
		return false
	}

	arrDict.Del([]byte(strconv.Itoa(idx)))

	for i := idx; i < len-1; i++ {
		srcIdx := strconv.Itoa(i + 1)
		dstIdx := strconv.Itoa(i)
		srcOff, ok := arrDict.Get([]byte(srcIdx))
		if ok {
			arrDict.Set([]byte(dstIdx), srcOff)
		}
	}
	arrDict.Del([]byte(strconv.Itoa(len - 1)))
	arrDict.StoreMeta(dataOff)

	return true
}

// ArrayCreate 创建一个新数组
func (db *RedisDB) ArrayCreate(key string, values ...[]byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)

	arrDict := NewDict(db.arena)
	for i, v := range values {
		idxStr := strconv.Itoa(i)
		valOff := db.arena.AllocBytes(v)
		arrDict.Set([]byte(idxStr), valOff)
	}

	metaOff := db.arena.Alloc(12)
	arrDict.StoreMeta(metaOff)

	headOff := db.NewObject(ObjArray, ObjEncodingHashtable, metaOff)
	db.dict.Set(keyBytes, headOff)
	db.incrementKeyVersion(key)

	return true
}

// ArrayRange 获取数组指定范围内的元素
func (db *RedisDB) ArrayRange(key string, start, end int64) ([]*PooledBuffer, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	if db.isExpired(keyBytes, headOff) {
		return nil, false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return nil, false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return make([]*PooledBuffer, 0), true
	}

	len := arrDict.Len()

	if start < 0 {
		start += int64(len)
	}
	if end < 0 {
		end += int64(len)
	}

	if start > end || start >= int64(len) {
		return make([]*PooledBuffer, 0), true
	}

	if end >= int64(len) {
		end = int64(len) - 1
	}

	count := int(end - start + 1)
	result := make([]*PooledBuffer, count)

	for i := int(start); i <= int(end); i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			data := db.arena.ReadBytes(valOff, size)
			result[i-int(start)] = BufFromBytes(data)
		}
	}

	return result, true
}

// ArraySort 对数组进行排序
func (db *RedisDB) ArraySort(key string, desc bool) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()
	if len <= 1 {
		return true
	}

	values := make([]string, len)
	for i := 0; i < len; i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			values[i] = string(db.arena.ReadBytes(valOff, size))
		}
	}

	if desc {
		sort.Sort(sort.Reverse(sort.StringSlice(values)))
	} else {
		sort.Strings(values)
	}

	for i, v := range values {
		idxStr := strconv.Itoa(i)
		valOff := db.arena.AllocBytes([]byte(v))
		arrDict.Set([]byte(idxStr), valOff)
	}

	return true
}

// ArrayReverse 反转数组
func (db *RedisDB) ArrayReverse(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()
	if len <= 1 {
		return true
	}

	values := make([][]byte, len)
	for i := 0; i < len; i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			values[i] = db.arena.ReadBytes(valOff, size)
		}
	}

	for i, j := 0, len-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}

	for i, v := range values {
		idxStr := strconv.Itoa(i)
		valOff := db.arena.AllocBytes(v)
		arrDict.Set([]byte(idxStr), valOff)
	}

	return true
}

// ArrayContains 检查数组是否包含指定值
func (db *RedisDB) ArrayContains(key string, value []byte) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()
	target := string(value)

	for i := 0; i < len; i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			v := string(db.arena.ReadBytes(valOff, size))
			if v == target {
				return true
			}
		}
	}

	return false
}

// ArrayIndexOf 查找值在数组中的索引
func (db *RedisDB) ArrayIndexOf(key string, value []byte) int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return -1
	}

	if db.isExpired(keyBytes, headOff) {
		return -1
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return -1
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return -1
	}

	len := arrDict.Len()
	target := string(value)

	for i := 0; i < len; i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			v := string(db.arena.ReadBytes(valOff, size))
			if v == target {
				return int64(i)
			}
		}
	}

	return -1
}

// ArrayClear 清空数组
func (db *RedisDB) ArrayClear(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	newDict := NewDict(db.arena)
	metaOff := db.arena.Alloc(12)
	newDict.StoreMeta(metaOff)

	db.arena.Free(db.ObjectDataOffset(headOff))
	db.ObjectSetDataOffset(headOff, metaOff)

	return true
}

// ArrayTrim 修剪数组
func (db *RedisDB) ArrayTrim(key string, start, end int64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	objType := db.ObjectType(headOff)
	if objType != ObjArray {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	arrDict := db.dictGetDict(dataOff)
	if arrDict == nil {
		return false
	}

	len := arrDict.Len()

	if start < 0 {
		start += int64(len)
	}
	if end < 0 {
		end += int64(len)
	}

	if start > end || start >= int64(len) {
		newDict := NewDict(db.arena)
		metaOff := db.arena.Alloc(12)
		newDict.StoreMeta(metaOff)
		db.arena.Free(dataOff)
		db.ObjectSetDataOffset(headOff, metaOff)
		return true
	}

	if end >= int64(len) {
		end = int64(len) - 1
	}

	newLen := int(end - start + 1)
	values := make([][]byte, newLen)

	for i := int(start); i <= int(end); i++ {
		idxStr := strconv.Itoa(i)
		valOff, ok := arrDict.Get([]byte(idxStr))
		if ok {
			size := db.arena.SizeAt(valOff)
			values[i-int(start)] = db.arena.ReadBytes(valOff, size)
		}
	}

	newDict := NewDict(db.arena)
	for i, v := range values {
		idxStr := strconv.Itoa(i)
		valOff := db.arena.AllocBytes(v)
		newDict.Set([]byte(idxStr), valOff)
	}

	newMetaOff := db.arena.Alloc(12)
	newDict.StoreMeta(newMetaOff)

	db.arena.Free(dataOff)
	db.ObjectSetDataOffset(headOff, newMetaOff)

	return true
}

// ArrayPush 向数组末尾添加值（同 ArrayAppend）
func (db *RedisDB) ArrayPush(key string, value []byte) int64 {
	return db.ArrayAppend(key, value)
}

// ArrayUnshift 向数组开头添加值
func (db *RedisDB) ArrayUnshift(key string, value []byte) int64 {
	ok := db.ArrayInsert(key, 0, value)
	if !ok {
		return -1
	}
	return db.ArrayLen(key)
}

// dictGetDict 从偏移获取 Dict 对象
func (db *RedisDB) dictGetDict(off int) *Dict {
	if off == 0 {
		return nil
	}
	return LoadDictMeta(db.arena, off)
}
