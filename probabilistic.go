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

import "math"

const (
	bfDefaultSize      = 1024
	bfDefaultHashCount = 3
)

func (db *RedisDB) BFReserve(key string, errorRate float64, capacity int) {
	db.mu.Lock()
	defer db.mu.Unlock()

	size := bfOptimalSize(capacity, errorRate)
	hashCount := bfOptimalHashCount(size, capacity)

	bfSize := 4 + 4 + (size+7)/8
	bfOff := db.arena.Alloc(bfSize)

	db.arena.WriteUint32(bfOff, uint32(size))
	db.arena.WriteUint32(bfOff+4, uint32(hashCount))

	keyBytes := []byte(key)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	headOff := db.NewObject(ObjBloom, ObjEncodingRaw, bfOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) BFAdd(key string, item []byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		bfSize := 4 + 4 + (bfDefaultSize+7)/8
		bfOff := db.arena.Alloc(bfSize)
		db.arena.WriteUint32(bfOff, uint32(bfDefaultSize))
		db.arena.WriteUint32(bfOff+4, uint32(bfDefaultHashCount))

		headOff = db.NewObject(ObjBloom, ObjEncodingRaw, bfOff)
		db.dict.Set(keyBytes, headOff)
	}

	dataOff := db.ObjectDataOffset(headOff)
	n := int(db.arena.ReadUint32(dataOff))
	k := int(db.arena.ReadUint32(dataOff + 4))
	bitArrayOff := dataOff + 8

	alreadyExists := true
	for i := 0; i < k; i++ {
		h := bfHash(item, i, n)
		byteIdx := bitArrayOff + h/8
		bitIdx := h % 8
		b := db.arena.ReadByte(byteIdx)
		if (b>>bitIdx)&1 == 0 {
			alreadyExists = false
			db.arena.WriteByte(byteIdx, b|(1<<bitIdx))
		}
	}

	if alreadyExists {
		return false
	}
	return true
}

func (db *RedisDB) BFExists(key string, item []byte) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	n := int(db.arena.ReadUint32(dataOff))
	k := int(db.arena.ReadUint32(dataOff + 4))
	bitArrayOff := dataOff + 8

	for i := 0; i < k; i++ {
		h := bfHash(item, i, n)
		byteIdx := bitArrayOff + h/8
		bitIdx := h % 8
		b := db.arena.ReadByte(byteIdx)
		if (b>>bitIdx)&1 == 0 {
			return false
		}
	}
	return true
}

func bfOptimalSize(capacity int, errorRate float64) int {
	if errorRate <= 0 || errorRate >= 1 {
		errorRate = 0.01
	}
	n := float64(capacity)
	m := -n * math.Log(errorRate) / (math.Ln2 * math.Ln2)
	if m < 1 {
		m = 1
	}
	return int(math.Ceil(m))
}

func bfOptimalHashCount(size, capacity int) int {
	if capacity <= 0 {
		return 1
	}
	k := int(float64(size) / float64(capacity) * 0.6931471805599453)
	if k < 1 {
		k = 1
	}
	return k
}

func bfHash(data []byte, seed int, n int) int {
	h := fnv32(data)
	h = h ^ uint32(seed)*0x9e3779b9
	h = h ^ (h >> 16)
	return int(h % uint32(n))
}

// === Cuckoo Filter ===

const (
	cfBucketSize      = 4
	cfMaxKicks        = 500
	cfFingerprintSize = 2
)

func (db *RedisDB) CFReserve(key string, capacity int) {
	db.mu.Lock()
	defer db.mu.Unlock()

	numBuckets := capacity / cfBucketSize
	if numBuckets < 1 {
		numBuckets = 1
	}

	cfSize := 4 + numBuckets*cfBucketSize*cfFingerprintSize
	cfOff := db.arena.Alloc(cfSize)
	db.arena.WriteUint32(cfOff, uint32(numBuckets))

	keyBytes := []byte(key)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	headOff := db.NewObject(ObjCuckoo, ObjEncodingRaw, cfOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) CFAdd(key string, item []byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		db.CFReserve(key, 1024)
		headOff, _ = db.dict.Get(keyBytes)
	}

	dataOff := db.ObjectDataOffset(headOff)
	numBuckets := int(db.arena.ReadUint32(dataOff))
	bucketsOff := dataOff + 4

	fp := cfFingerprint(item)
	i1 := int(fnv32(item) % uint32(numBuckets))
	i2 := i1 ^ int(fnv32U16(fp)%uint32(numBuckets))

	if cfInsertBucket(db.arena, bucketsOff, numBuckets, i1, fp) {
		return true
	}
	if cfInsertBucket(db.arena, bucketsOff, numBuckets, i2, fp) {
		return true
	}

	curIdx := i1
	for n := 0; n < cfMaxKicks; n++ {
		bucketOff := bucketsOff + curIdx*cfBucketSize*cfFingerprintSize
		slot := int(fnv32Byte(byte(n))) % cfBucketSize
		slotOff := bucketOff + slot*cfFingerprintSize

		oldFp := db.arena.ReadUint16(slotOff)
		db.arena.WriteUint16(slotOff, fp)

		fp = oldFp
		curIdx = curIdx ^ int(fnv32U16(fp)%uint32(numBuckets))

		if cfInsertBucket(db.arena, bucketsOff, numBuckets, curIdx, fp) {
			return true
		}
	}

	return false
}

func (db *RedisDB) CFDel(key string, item []byte) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	numBuckets := int(db.arena.ReadUint32(dataOff))
	bucketsOff := dataOff + 4

	fp := cfFingerprint(item)
	i1 := int(fnv32(item) % uint32(numBuckets))
	i2 := i1 ^ int(fnv32U16(fp)%uint32(numBuckets))

	if cfRemoveFromBucket(db.arena, bucketsOff, i1, fp) {
		return true
	}
	if cfRemoveFromBucket(db.arena, bucketsOff, i2, fp) {
		return true
	}

	return false
}

func (db *RedisDB) CFExists(key string, item []byte) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	numBuckets := int(db.arena.ReadUint32(dataOff))
	bucketsOff := dataOff + 4

	fp := cfFingerprint(item)
	i1 := int(fnv32(item) % uint32(numBuckets))
	i2 := i1 ^ int(fnv32U16(fp)%uint32(numBuckets))

	if cfFindInBucket(db.arena, bucketsOff, i1, fp) {
		return true
	}
	if cfFindInBucket(db.arena, bucketsOff, i2, fp) {
		return true
	}

	return false
}

func cfFingerprint(data []byte) uint16 {
	h := fnv32(data)
	return uint16(h ^ (h >> 16))
}

func cfInsertBucket(arena *Arena, bucketsOff int, numBuckets, bucketIdx int, fp uint16) bool {
	bucketOff := bucketsOff + bucketIdx*cfBucketSize*cfFingerprintSize
	for i := 0; i < cfBucketSize; i++ {
		slotOff := bucketOff + i*cfFingerprintSize
		if arena.ReadUint16(slotOff) == 0 {
			arena.WriteUint16(slotOff, fp)
			return true
		}
	}
	return false
}

func cfRemoveFromBucket(arena *Arena, bucketsOff int, bucketIdx int, fp uint16) bool {
	bucketOff := bucketsOff + bucketIdx*cfBucketSize*cfFingerprintSize
	for i := 0; i < cfBucketSize; i++ {
		slotOff := bucketOff + i*cfFingerprintSize
		if arena.ReadUint16(slotOff) == fp {
			arena.WriteUint16(slotOff, 0)
			return true
		}
	}
	return false
}

func cfFindInBucket(arena *Arena, bucketsOff int, bucketIdx int, fp uint16) bool {
	bucketOff := bucketsOff + bucketIdx*cfBucketSize*cfFingerprintSize
	for i := 0; i < cfBucketSize; i++ {
		slotOff := bucketOff + i*cfFingerprintSize
		if arena.ReadUint16(slotOff) == fp {
			return true
		}
	}
	return false
}

// === Count-Min Sketch ===

const (
	cmsDefaultWidth = 2000
	cmsDefaultDepth = 10
)

func (db *RedisDB) CMSInitByDim(key string, width, depth int) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if width <= 0 {
		width = cmsDefaultWidth
	}
	if depth <= 0 {
		depth = cmsDefaultDepth
	}

	cmsSize := 4 + 4 + width*depth*4
	cmsOff := db.arena.Alloc(cmsSize)
	db.arena.WriteUint32(cmsOff, uint32(width))
	db.arena.WriteUint32(cmsOff+4, uint32(depth))

	keyBytes := []byte(key)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	headOff := db.NewObject(ObjCMS, ObjEncodingRaw, cmsOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) CMSIncrBy(key string, item []byte, inc int) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		db.CMSInitByDim(key, cmsDefaultWidth, cmsDefaultDepth)
		headOff, _ = db.dict.Get(keyBytes)
	}

	dataOff := db.ObjectDataOffset(headOff)
	width := int(db.arena.ReadUint32(dataOff))
	depth := int(db.arena.ReadUint32(dataOff + 4))
	countersOff := dataOff + 8

	minVal := int(^uint32(0) >> 1)
	baseH := fnv32(item)

	for d := 0; d < depth; d++ {
		h := int(((baseH ^ uint32(d)) * 16777619) % uint32(width))
		counterOff := countersOff + (d*width+h)*4
		val := int(db.arena.ReadUint32(counterOff)) + inc
		db.arena.WriteUint32(counterOff, uint32(val))
		if val < minVal {
			minVal = val
		}
	}

	return minVal
}

func (db *RedisDB) CMSQuery(key string, items ...[]byte) []int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		result := make([]int, len(items))
		return result
	}

	dataOff := db.ObjectDataOffset(headOff)
	width := int(db.arena.ReadUint32(dataOff))
	depth := int(db.arena.ReadUint32(dataOff + 4))
	countersOff := dataOff + 8

	result := make([]int, len(items))

	for idx, item := range items {
		minVal := int(^uint32(0) >> 1)
		baseH := fnv32(item)
		for d := 0; d < depth; d++ {
			h := int(((baseH ^ uint32(d)) * 16777619) % uint32(width))
			val := int(db.arena.ReadUint32(countersOff + (d*width+h)*4))
			if val < minVal {
				minVal = val
			}
		}
		result[idx] = minVal
	}

	return result
}

// === Top-K ===

const (
	topkDefaultK = 10
)

type TopKItem struct {
	Item  string
	Count int
}

func (db *RedisDB) TopKReserve(key string, k int) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if k <= 0 {
		k = topkDefaultK
	}

	topkSize := 4 + k*8
	topkOff := db.arena.Alloc(topkSize)
	db.arena.WriteUint32(topkOff, uint32(k))

	keyBytes := []byte(key)
	existingHeadOff, exists := db.dict.Get(keyBytes)
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	headOff := db.NewObject(ObjTopK, ObjEncodingRaw, topkOff)
	db.dict.Set(keyBytes, headOff)
}

func (db *RedisDB) TopKAdd(key string, items ...string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		db.TopKReserve(key, topkDefaultK)
		headOff, _ = db.dict.Get(keyBytes)
	}

	dataOff := db.ObjectDataOffset(headOff)
	k := int(db.arena.ReadUint32(dataOff))
	itemsOff := dataOff + 4

	for _, item := range items {
		itemBytes := []byte(item)

		found := false
		for i := 0; i < k; i++ {
			slotOff := itemsOff + i*8
			entryOff := int(db.arena.ReadUint32(slotOff))
			if entryOff == 0 {
				continue
			}
			count := int(db.arena.ReadUint32(slotOff + 4))
			entrySize := db.arena.SizeAt(entryOff)
			entryData := db.arena.ReadBytes(entryOff, entrySize)
			if string(entryData) == item {
				db.arena.WriteUint32(slotOff+4, uint32(count+1))
				found = true
				break
			}
		}

		if !found {
			minCount := int(^uint32(0) >> 1)
			minIdx := -1
			for i := 0; i < k; i++ {
				slotOff := itemsOff + i*8
				entryOff := db.arena.ReadUint32(slotOff)
				count := int(db.arena.ReadUint32(slotOff + 4))
				if entryOff == 0 || count < minCount {
					minCount = count
					minIdx = i
				}
			}

			if minIdx >= 0 {
				slotOff := itemsOff + minIdx*8
				oldEntryOff := int(db.arena.ReadUint32(slotOff))
				if oldEntryOff != 0 {
					db.arena.Free(oldEntryOff)
				}
				newEntryOff := db.arena.AllocBytes(itemBytes)
				db.arena.WriteUint32(slotOff, uint32(newEntryOff))
				db.arena.WriteUint32(slotOff+4, 1)
			}
		}
	}
}

func (db *RedisDB) TopKList(key string) []TopKItem {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	k := int(db.arena.ReadUint32(dataOff))
	itemsOff := dataOff + 4

	var result []TopKItem

	for i := 0; i < k; i++ {
		slotOff := itemsOff + i*8
		entryOff := int(db.arena.ReadUint32(slotOff))
		if entryOff == 0 {
			continue
		}
		count := int(db.arena.ReadUint32(slotOff + 4))
		entrySize := db.arena.SizeAt(entryOff)
		entryData := db.arena.ReadBytes(entryOff, entrySize)
		result = append(result, TopKItem{
			Item:  string(entryData),
			Count: count,
		})
	}

	return result
}
