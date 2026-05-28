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

// 位图（Bitmap）实现，支持位级别的读写、统计和位运算。
package gedis

import (
	"encoding/binary"
)

// SetBit 设置或清除指定偏移位置的位。返回该位的旧值。
func (db *RedisDB) SetBit(key string, offset int, val int) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	var data []byte
	if ok {
		enc := db.ObjectEncoding(headOff)
		dataOff := db.ObjectDataOffset(headOff)

		if enc == ObjEncodingInt {
			intVal := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, uint64(intVal))
			data = buf
		} else if dataOff != 0 {
			size := db.arena.SizeAt(dataOff)
			data = db.arena.ReadBytes(dataOff, size)
		}
	} else {
		data = make([]byte, 0)
	}

	byteIdx := offset / 8
	bitIdx := 7 - (offset % 8)

	for byteIdx >= len(data) {
		data = append(data, 0)
	}

	oldBit := (data[byteIdx] >> bitIdx) & 1

	if val != 0 {
		data[byteIdx] |= (1 << bitIdx)
	} else {
		data[byteIdx] &^= (1 << bitIdx)
	}

	if ok {
		oldDataOff := db.ObjectDataOffset(headOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
		newOff := db.arena.AllocBytes(data)
		db.ObjectSetDataOffset(headOff, newOff)
		db.ObjectSetEncoding(headOff, ObjEncodingRaw)
	} else {
		newOff := db.arena.AllocBytes(data)
		headOff = db.NewObject(ObjBitmap, ObjEncodingRaw, newOff)
		db.dict.Set(keyBytes, headOff)
	}

	return int(oldBit)
}

// GetBit 获取指定偏移位置的位值（0 或 1）。
func (db *RedisDB) GetBit(key string, offset int) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0
	}

	data := db.getRawData(headOff)
	if data == nil {
		return 0
	}

	byteIdx := offset / 8
	if byteIdx >= len(data) {
		return 0
	}

	bitIdx := 7 - (offset % 8)
	return int((data[byteIdx] >> bitIdx) & 1)
}

// BitCount 统计指定范围内设置为 1 的位数。
func (db *RedisDB) BitCount(key string, start, end int) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0
	}

	data := db.getRawData(headOff)
	if data == nil {
		return 0
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
		return 0
	}

	count := 0
	for i := start; i <= end; i++ {
		count += popcountByte(data[i])
	}
	return count
}

// BitOp 对多个位图执行位运算（AND、OR、XOR、NOT），结果存入目标键。
func (db *RedisDB) BitOp(op string, destKey string, srcKeys ...string) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	if len(srcKeys) == 0 {
		return 0
	}

	var datas [][]byte
	maxLen := 0

	for _, key := range srcKeys {
		headOff, ok := db.dict.Get([]byte(key))
		if ok {
			d := db.getRawData(headOff)
			datas = append(datas, d)
			if len(d) > maxLen {
				maxLen = len(d)
			}
		} else {
			datas = append(datas, nil)
		}
	}

	result := make([]byte, maxLen)

	if op == "NOT" {
		for i := 0; i < maxLen; i++ {
			if i < len(datas[0]) {
				result[i] = ^datas[0][i]
			} else {
				result[i] = 0xFF
			}
		}
	} else {
		for i := 0; i < maxLen; i++ {
			var b byte
			if len(datas) > 0 && i < len(datas[0]) {
				b = datas[0][i]
			}
			for j := 1; j < len(datas); j++ {
				var b2 byte
				if datas[j] != nil && i < len(datas[j]) {
					b2 = datas[j][i]
				}
				switch op {
				case "AND":
					b &= b2
				case "OR":
					b |= b2
				case "XOR":
					b ^= b2
				}
			}
			result[i] = b
		}
	}

	for maxLen > 0 && result[maxLen-1] == 0 {
		result = result[:maxLen-1]
		maxLen--
	}

	keyBytes := []byte(destKey)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	newOff := db.arena.AllocBytes(result)
	headOff := db.NewObject(ObjBitmap, ObjEncodingRaw, newOff)
	db.dict.Set(keyBytes, headOff)

	return len(result)
}

// BitField 在位图上执行 GET、SET、INCRBY 子命令。
// 对应 Redis: BITFIELD key [GET type offset] [SET type offset value] [INCRBY type offset increment]
// 优化：入参使用 *PooledBuffer 替代 []byte，调用方通过 Buf(s) 获取后即可 Close。
func (db *RedisDB) BitField(key string, args ...*PooledBuffer) []int64 {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	var data []byte
	if ok {
		data = db.getRawData(headOff)
	} else {
		data = make([]byte, 0)
	}

	var results []int64

	i := 0
	for i < len(args) {
		if i+2 >= len(args) {
			break
		}

		op := args[i].String()
		typeStr := args[i+1].String()
		offsetOrOverflow, _ := parseIntBytes(args[i+2].Bytes())

		isSigned := typeStr[0] == 'i'
		var bits int64
		if isSigned || typeStr[0] == 'u' {
			bits, _ = parseIntBytes([]byte(typeStr[1:]))
		}

		switch op {
		case "GET":
			val := bitfieldGet(data, int(offsetOrOverflow), int(bits), isSigned)
			results = append(results, val)
			i += 3
		case "SET":
			if i+3 >= len(args) {
				break
			}
			setVal, _ := parseIntBytes(args[i+3].Bytes())
			oldVal := bitfieldGet(data, int(offsetOrOverflow), int(bits), isSigned)
			data = bitfieldSet(data, int(offsetOrOverflow), int(bits), setVal)
			results = append(results, oldVal)
			i += 4
		case "INCRBY":
			if i+3 >= len(args) {
				break
			}
			incVal, _ := parseIntBytes(args[i+3].Bytes())
			curVal := bitfieldGet(data, int(offsetOrOverflow), int(bits), isSigned)
			newVal := curVal + incVal
			data = bitfieldSet(data, int(offsetOrOverflow), int(bits), newVal)
			results = append(results, newVal)
			i += 4
		default:
			i++
		}
	}

	if ok {
		oldDataOff := db.ObjectDataOffset(headOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
		newOff := db.arena.AllocBytes(data)
		db.ObjectSetDataOffset(headOff, newOff)
		db.ObjectSetEncoding(headOff, ObjEncodingRaw)
	} else {
		newOff := db.arena.AllocBytes(data)
		headOff = db.NewObject(ObjBitmap, ObjEncodingRaw, newOff)
		db.dict.Set(keyBytes, headOff)
	}

	return results
}

func (db *RedisDB) getRawData(headOff int) []byte {
	if headOff == 0 {
		return nil
	}
	enc := db.ObjectEncoding(headOff)
	if enc == ObjEncodingInt {
		val := int64(db.arena.ReadUint64(objHeaderDataOffset(headOff)))
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(val))
		return buf
	}
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil
	}
	size := db.arena.SizeAt(dataOff)
	return db.arena.GetSlice(dataOff, size)
}

var popcount256 = [256]int{
	0, 1, 1, 2, 1, 2, 2, 3, 1, 2, 2, 3, 2, 3, 3, 4,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	4, 5, 5, 6, 5, 6, 6, 7, 5, 6, 6, 7, 6, 7, 7, 8,
}

func popcountByte(b byte) int {
	return popcount256[b]
}

func bitfieldGet(data []byte, offset int, bits int, signed bool) int64 {
	byteIdx := offset / 8
	bitIdx := offset % 8

	var val uint64
	neededBytes := (bitIdx + bits + 7) / 8

	for i := 0; i < neededBytes; i++ {
		idx := byteIdx + i
		b := byte(0)
		if idx < len(data) {
			b = data[idx]
		}
		val |= uint64(b) << ((neededBytes - 1 - i) * 8)
	}

	shift := (neededBytes * 8) - bitIdx - bits
	val >>= shift
	val &= (1 << bits) - 1

	if signed && (val&(1<<(bits-1))) != 0 {
		val |= ^((1 << bits) - 1)
	}

	return int64(val)
}

func bitfieldSet(data []byte, offset int, bits int, value int64) []byte {
	byteIdx := offset / 8
	bitIdx := offset % 8

	neededBytes := (bitIdx + bits + 7) / 8
	totalBytes := byteIdx + neededBytes

	for len(data) < totalBytes {
		data = append(data, 0)
	}

	var mask uint64 = (1 << bits) - 1
	val := uint64(value) & mask

	for i := neededBytes - 1; i >= 0; i-- {
		idx := byteIdx + neededBytes - 1 - i
		if idx >= len(data) {
			break
		}

		shiftStart := i * 8
		byteVal := byte((val >> shiftStart) & 0xFF)

		bitsInThisByte := 8
		if i == 0 {
			bitsInThisByte = 8 - bitIdx%8
		}
		if i == neededBytes-1 {
			remainingBits := (bitIdx + bits) % 8
			if remainingBits == 0 {
				remainingBits = 8
			}
			bitsInThisByte = remainingBits
		}

		clearMask := byte(((1 << bitsInThisByte) - 1) << (8 - bitsInThisByte - (bitIdx % 8)))
		if i == 0 {
			bitShift := bitIdx % 8
			data[idx] &^= clearMask
			data[idx] |= byteVal << (8 - bitShift - bitsInThisByte)
		} else if i == neededBytes-1 {
			data[idx] &^= clearMask
			data[idx] |= byteVal
		} else {
			data[idx] = byteVal
		}
	}

	return data
}

func parseIntBytes(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}

	neg := false
	start := 0
	if b[0] == '-' {
		neg = true
		start = 1
	}

	var val int64
	for i := start; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		val = val*10 + int64(b[i]-'0')
	}

	if neg {
		val = -val
	}
	return val, true
}
