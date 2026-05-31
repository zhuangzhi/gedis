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

// SortOptions 排序选项
type SortOptions struct {
	Offset    int
	Count     int
	Alpha     bool
	Desc      bool
	StoreDest string
}

// SortResult 排序结果
type SortResult struct {
	Values []*PooledBuffer
	Dest   string
}

// Sort 对列表、集合或有序集合进行排序
func (db *RedisDB) Sort(key string, options SortOptions) (*SortResult, bool) {
	result := &SortResult{}

	values, ok := db.getAllValues(key)
	if !ok || len(values) == 0 {
		if options.StoreDest != "" && ok {
			db.Del(options.StoreDest)
		}
		return result, ok
	}

	sorted := db.sortValues(values, options)
	result.Values = sorted

	if options.StoreDest != "" {
		db.Del(options.StoreDest)
		db.RPush(options.StoreDest, sortedToBytes(sorted)...)
		result.Dest = options.StoreDest
	}

	return result, true
}

// getAllValues 获取 key 对应的所有值
func (db *RedisDB) getAllValues(key string) ([]string, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil, false
	}

	if db.isExpired([]byte(key), headOff) {
		return nil, false
	}

	objType := db.ObjectType(headOff)
	encoding := db.ObjectEncoding(headOff)

	switch objType {
	case ObjList:
		return db.getListValues(headOff)
	case ObjSet:
		if encoding == ObjEncodingIntset {
			return db.getIntsetValues(headOff)
		}
		return db.getSetValues(headOff)
	case ObjZSet:
		return db.getZSetValues(headOff)
	}

	return nil, false
}

// getListValues 获取列表的所有值
func (db *RedisDB) getListValues(headOff int) ([]string, bool) {
	objType := db.ObjectType(headOff)
	if objType != ObjList {
		return nil, false
	}

	values := make([]string, 0)
	for i := 0; ; i++ {
		val, ok := db.LIndexAt(headOff, i)
		if !ok {
			break
		}
		values = append(values, val.String())
		val.Close()
	}

	return values, true
}

// LIndexAt 获取列表指定索引的值（内部使用）
func (db *RedisDB) LIndexAt(headOff int, index int) (*PooledBuffer, bool) {
	encoding := db.ObjectEncoding(headOff)

	if encoding == ObjEncodingZiplist {
		return db.ziplistIndex(db.ObjectDataOffset(headOff), index)
	}
	return db.linkedListIndex(headOff, index)
}

// ziplistIndex 获取压缩列表指定索引的值
func (db *RedisDB) ziplistIndex(dataOff int, index int) (*PooledBuffer, bool) {
	if dataOff == 0 || index < 0 {
		return nil, false
	}

	entries := db.ziplistEntries(dataOff)
	if index >= len(entries) {
		return nil, false
	}

	entryOff := entries[index]
	size := db.arena.SizeAt(entryOff)
	data := db.arena.ReadBytes(entryOff, size)
	return BufFromBytes(data), true
}

// linkedListIndex 获取链表指定索引的值
func (db *RedisDB) linkedListIndex(headOff int, index int) (*PooledBuffer, bool) {
	if index < 0 {
		return nil, false
	}

	curOff := int(db.arena.ReadUint64(headOff + 24))
	count := 0

	for curOff != 0 {
		if count == index {
			itemOff := int(db.arena.ReadUint64(curOff))
			size := db.arena.SizeAt(itemOff)
			data := db.arena.ReadBytes(itemOff, size)
			return BufFromBytes(data), true
		}
		curOff = int(db.arena.ReadUint64(curOff + 16))
		count++
	}

	return nil, false
}

// ziplistEntries 获取压缩列表的所有条目偏移量
func (db *RedisDB) ziplistEntries(dataOff int) []int {
	entries := make([]int, 0)

	offset := dataOff + 4 + 4
	end := dataOff + int(db.arena.ReadUint32(dataOff))

	for offset < end {
		entryLen, _, _ := db.ziplistLength(dataOff, offset)
		if offset+entryLen > end {
			break
		}
		entries = append(entries, offset)
		offset += entryLen
	}

	return entries
}

// ziplistLength 解码压缩列表条目的长度
func (db *RedisDB) ziplistLength(dataOff, offset int) (int, int, bool) {
	firstByte := db.arena.ReadBytes(offset, 1)[0]

	if firstByte < 0x40 {
		return int(firstByte), int(firstByte), false
	} else if firstByte < 0xC0 {
		return int(firstByte & 0x3F), int(firstByte), false
	} else if firstByte < 0xF0 {
		return 2, int(firstByte), false
	} else {
		return 1, int(firstByte), false
	}
}

// getIntsetValues 获取整数集合的值
func (db *RedisDB) getIntsetValues(headOff int) ([]string, bool) {
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil, false
	}

	size := db.arena.SizeAt(dataOff)
	count := size / 8
	values := make([]string, 0, count)

	for i := 0; i < count; i++ {
		valOff := dataOff + i*8
		val := int64(db.arena.ReadUint64(valOff))
		values = append(values, strconv.FormatInt(val, 10))
	}

	return values, true
}

// getSetValues 获取集合的值
func (db *RedisDB) getSetValues(headOff int) ([]string, bool) {
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil, false
	}

	used := int(db.arena.ReadUint32(dataOff))
	tableOff := int(db.arena.ReadUint64(dataOff + 8))

	values := make([]string, 0, used)

	for i := 0; i < used*2 && len(values) < used; i += 2 {
		fieldOff := int(db.arena.ReadUint64(tableOff + i*8))
		if fieldOff == 0 {
			continue
		}
		size := db.arena.SizeAt(fieldOff)
		data := db.arena.ReadBytes(fieldOff, size)
		values = append(values, string(data))
	}

	return values, true
}

// getZSetValues 获取有序集合的值
func (db *RedisDB) getZSetValues(headOff int) ([]string, bool) {
	encoding := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if dataOff == 0 {
		return nil, false
	}

	if encoding == ObjEncodingZiplist {
		entries := db.ziplistEntries(dataOff)
		values := make([]string, 0, len(entries)/2)

		for i := 0; i+1 < len(entries); i += 2 {
			memberOff := entries[i+1] + 8
			size := db.arena.SizeAt(memberOff)
			data := db.arena.ReadBytes(memberOff, size)
			values = append(values, string(data))
		}

		return values, true
	}

	return nil, false
}

// sortValues 对值进行排序
func (db *RedisDB) sortValues(values []string, options SortOptions) []*PooledBuffer {
	if len(values) == 0 {
		return make([]*PooledBuffer, 0)
	}

	sorted := make([]string, len(values))
	copy(sorted, values)

	if options.Alpha {
		sort.Strings(sorted)
		if options.Desc {
			for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	} else {
		nums := make([]float64, len(sorted))
		for i, v := range sorted {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				nums[i] = f
			} else {
				nums[i] = 0
			}
		}

		indices := make([]int, len(nums))
		for i := range indices {
			indices[i] = i
		}

		sort.Slice(indices, func(i, j int) bool {
			if options.Desc {
				return nums[indices[i]] > nums[indices[j]]
			}
			return nums[indices[i]] < nums[indices[j]]
		})

		for i, idx := range indices {
			sorted[i] = values[idx]
		}
	}

	if options.Count > 0 {
		offset := options.Offset
		if offset < 0 {
			offset = 0
		}
		if offset >= len(sorted) {
			return make([]*PooledBuffer, 0)
		}
		end := offset + options.Count
		if end > len(sorted) {
			end = len(sorted)
		}
		sorted = sorted[offset:end]
	}

	result := make([]*PooledBuffer, len(sorted))
	for i, v := range sorted {
		result[i] = BufFromBytes([]byte(v))
	}

	return result
}

// sortedToBytes 将排序结果转换为字节切片
func sortedToBytes(values []*PooledBuffer) [][]byte {
	result := make([][]byte, len(values))
	for i, v := range values {
		result[i] = v.Bytes()
		v.Close()
	}
	return result
}
