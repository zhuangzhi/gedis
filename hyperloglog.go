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

// HyperLogLog 实现，使用 16384 个 6 位寄存器进行基数估计。
// 基于 MurmurHash64 哈希函数。
package gedis

import (
	"math"
)

const (
	hllRegisters = 16384        // 寄存器数量
	hllPMask     = hllRegisters - 1 // 索引掩码
	hllBits      = 6            // 每寄存器位数
	hllBytes     = (hllRegisters * hllBits) / 8 // 寄存器总字节数
)

// PFAdd 向 HyperLogLog 中添加元素。返回实际更新的寄存器数量。
func (db *RedisDB) PFAdd(key string, elements ...*PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	var regOff int
	if ok {
		regOff = db.ObjectDataOffset(headOff)
		if regOff == 0 || db.arena.SizeAt(regOff) < hllBytes {
			if regOff != 0 {
				db.arena.Free(regOff)
			}
			regOff = db.arena.Alloc(hllBytes)
			db.ObjectSetDataOffset(headOff, regOff)
		}
	} else {
		regOff = db.arena.Alloc(hllBytes)
		headOff = db.NewObject(ObjHLL, ObjEncodingRaw, regOff)
		db.dict.Set(keyBytes, headOff)
	}

	registers := db.arena.GetSlice(regOff, hllBytes)

	updated := 0
	for _, elem := range elements {
		hash := murmurHash64(elem.Bytes())
		idx := int(hash & hllPMask)
		runLen := countTrailingZeros(hash>>14) + 1
		if runLen > 64 {
			runLen = 64
		}

		oldVal := hllGetRegister(registers, idx)
		if runLen > oldVal {
			hllSetRegister(registers, idx, runLen)
			updated++
		}
	}

	return updated
}

// PFCount 估计 HyperLogLog 的基数。支持同时估算多个 key 的合并基数。
func (db *RedisDB) PFCount(keys ...string) int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if len(keys) == 0 {
		return 0
	}

	if len(keys) == 1 {
		headOff, ok := db.dict.Get([]byte(keys[0]))
		if !ok {
			return 0
		}
		dataOff := db.ObjectDataOffset(headOff)
		if dataOff == 0 {
			return 0
		}
		return hllEstimate(db.arena.GetSlice(dataOff, hllBytes))
	}

	mergedOff := db.arena.Alloc(hllBytes)
	merged := db.arena.GetSlice(mergedOff, hllBytes)

	first := true
	for _, key := range keys {
		headOff, ok := db.dict.Get([]byte(key))
		if !ok {
			continue
		}
		dataOff := db.ObjectDataOffset(headOff)
		if dataOff == 0 {
			continue
		}
		registers := db.arena.GetSlice(dataOff, hllBytes)

		if first {
			copy(merged, registers)
			first = false
			continue
		}

		for i := 0; i < hllRegisters; i++ {
			a := hllGetRegister(merged, i)
			b := hllGetRegister(registers, i)
			if b > a {
				hllSetRegister(merged, i, b)
			}
		}
	}

	result := hllEstimate(merged)
	db.arena.Free(mergedOff)
	return result
}

// PFMerge 将多个 HyperLogLog 合并到目标 key 中。
func (db *RedisDB) PFMerge(dest string, sources ...string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	mergedOff := db.arena.Alloc(hllBytes)
	merged := db.arena.GetSlice(mergedOff, hllBytes)

	first := true
	for _, key := range sources {
		headOff, ok := db.dict.Get([]byte(key))
		if !ok {
			continue
		}
		dataOff := db.ObjectDataOffset(headOff)
		if dataOff == 0 {
			continue
		}
		registers := db.arena.GetSlice(dataOff, hllBytes)

		if first {
			copy(merged, registers)
			first = false
			continue
		}

		for i := 0; i < hllRegisters; i++ {
			a := hllGetRegister(merged, i)
			b := hllGetRegister(registers, i)
			if b > a {
				hllSetRegister(merged, i, b)
			}
		}
	}

	keyBytes := []byte(dest)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	newOff := db.arena.Alloc(hllBytes)
	db.arena.WriteBytes(newOff, merged)
	db.arena.Free(mergedOff)
	var headOff int
	if exists {
		headOff = existingHeadOff
		db.ObjectSetDataOffset(headOff, newOff)
	} else {
		headOff = db.NewObject(ObjHLL, ObjEncodingRaw, newOff)
		db.dict.Set(keyBytes, headOff)
	}
}

// hllGetRegister 读取指定索引处的 6 位寄存器值。
func hllGetRegister(registers []byte, idx int) int {
	if len(registers) == 0 {
		return 0
	}
	bitPos := idx * hllBits
	byteIdx := bitPos / 8
	bitIdx := bitPos % 8

	if bitIdx+hllBits <= 8 {
		shift := 8 - bitIdx - hllBits
		return int((registers[byteIdx] >> shift) & 0x3F)
	}

	bitsInFirst := 8 - bitIdx
	bitsInSecond := hllBits - bitsInFirst
	val := int(registers[byteIdx]&((1<<bitsInFirst)-1)) << bitsInSecond
	if byteIdx+1 < len(registers) {
		val |= int(registers[byteIdx+1] >> (8 - bitsInSecond))
	}
	return val
}

// hllSetRegister 设置指定索引处的 6 位寄存器值。
func hllSetRegister(registers []byte, idx int, val int) {
	if val > (1<<hllBits)-1 {
		val = (1 << hllBits) - 1
	}
	bitPos := idx * hllBits
	byteIdx := bitPos / 8
	bitIdx := bitPos % 8

	if bitIdx+hllBits <= 8 {
		shift := 8 - bitIdx - hllBits
		mask := byte(0x3F << shift)
		registers[byteIdx] &^= mask
		registers[byteIdx] |= byte(val << shift)
		return
	}

	bitsInFirst := 8 - bitIdx
	bitsInSecond := hllBits - bitsInFirst

	mask1 := byte((1 << bitsInFirst) - 1)
	registers[byteIdx] &^= mask1
	registers[byteIdx] |= byte((val >> bitsInSecond) & int(mask1))

	if byteIdx+1 < len(registers) {
		mask2 := byte(((1 << bitsInSecond) - 1) << (8 - bitsInSecond))
		registers[byteIdx+1] &^= mask2
		registers[byteIdx+1] |= byte((val & ((1 << bitsInSecond) - 1)) << (8 - bitsInSecond))
	}
}

func hllEstimate(registers []byte) int64 {
	var sum float64
	empty := 0

	for i := 0; i < hllRegisters; i++ {
		val := hllGetRegister(registers, i)
		sum += 1.0 / float64(int64(1)<<val)
		if val == 0 {
			empty++
		}
	}

	alpha := 0.7213 / (1.0 + 1.079/float64(hllRegisters))
	estimate := alpha * float64(hllRegisters) * float64(hllRegisters) / sum

	if estimate <= 2.5*float64(hllRegisters) {
		if empty > 0 {
			estimate = float64(hllRegisters) * math.Log(float64(hllRegisters)/float64(empty))
		}
	}

	return int64(estimate)
}

func murmurHash64(data []byte) uint64 {
	const (
		c1 = 0x87c37b91114253d5
		c2 = 0x4cf5ad432745937f
		r1 = 31
		r2 = 27
		m  = 5
		n  = 0x52dce729
	)

	nblocks := len(data) / 16
	var h1, h2 uint64

	for i := 0; i < nblocks; i++ {
		off := i * 16

		k1 := uint64(data[off]) | uint64(data[off+1])<<8 | uint64(data[off+2])<<16 | uint64(data[off+3])<<24 |
			uint64(data[off+4])<<32 | uint64(data[off+5])<<40 | uint64(data[off+6])<<48 | uint64(data[off+7])<<56

		k2 := uint64(data[off+8]) | uint64(data[off+9])<<8 | uint64(data[off+10])<<16 | uint64(data[off+11])<<24 |
			uint64(data[off+12])<<32 | uint64(data[off+13])<<40 | uint64(data[off+14])<<48 | uint64(data[off+15])<<56

		k1 *= c1
		k1 = (k1 << r1) | (k1 >> (64 - r1))
		k1 *= c2
		h1 ^= k1
		h1 = (h1 << r2) | (h1 >> (64 - r2))
		h1 += h2
		h1 = h1*m + n

		k2 *= c2
		k2 = (k2 << r1) | (k2 >> (64 - r1))
		k2 *= c1
		h2 ^= k2
		h2 = (h2 << r2) | (h2 >> (64 - r2))
		h2 += h1
		h2 = h2*5 + 0x38495ab5
	}

	tail := data[nblocks*16:]
	var k1, k2 uint64

	switch len(tail) {
	case 15:
		k2 ^= uint64(tail[14]) << 48
		fallthrough
	case 14:
		k2 ^= uint64(tail[13]) << 40
		fallthrough
	case 13:
		k2 ^= uint64(tail[12]) << 32
		fallthrough
	case 12:
		k2 ^= uint64(tail[11]) << 24
		fallthrough
	case 11:
		k2 ^= uint64(tail[10]) << 16
		fallthrough
	case 10:
		k2 ^= uint64(tail[9]) << 8
		fallthrough
	case 9:
		k2 ^= uint64(tail[8])
		k2 *= c2
		k2 = (k2 << r1) | (k2 >> (64 - r1))
		k2 *= c1
		h2 ^= k2
		fallthrough
	case 8:
		k1 ^= uint64(tail[7]) << 56
		fallthrough
	case 7:
		k1 ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		k1 ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		k1 ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		k1 ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		k1 ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		k1 ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		k1 ^= uint64(tail[0])
		k1 *= c1
		k1 = (k1 << r1) | (k1 >> (64 - r1))
		k1 *= c2
		h1 ^= k1
	}

	h1 ^= uint64(len(data))
	h2 ^= uint64(len(data))

	h1 += h2
	h2 += h1

	h1 ^= h1 >> 33
	h1 *= 0xff51afd7ed558ccd
	h1 ^= h1 >> 33
	h1 *= 0xc4ceb9fe1a85ec53
	h1 ^= h1 >> 33

	h2 ^= h2 >> 33
	h2 *= 0xff51afd7ed558ccd
	h2 ^= h2 >> 33
	h2 *= 0xc4ceb9fe1a85ec53
	h2 ^= h2 >> 33

	return h1 + h2
}

func countTrailingZeros(v uint64) int {
	if v == 0 {
		return 64
	}
	c := 63
	s := uint64(1 << 63)
	for (v & s) == 0 {
		c--
		s >>= 1
	}
	return 63 - c
}
