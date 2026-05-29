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

// 时间序列（TimeSeries）实现，使用分块（Chunk）存储采样数据。
// 每个 Chunk 可存储多个时间戳-值的采样点。
package gedis

// TSPoint 时间序列数据点，包含时间戳和值。
type TSPoint struct {
	Timestamp int64
	Value     float64
}

const (
	tsChunkSize     = 8192
	tsSampleSize    = 16
	tsMaxSamples    = (tsChunkSize - tsChunkBaseSize) / tsSampleSize
	tsMetaBaseSize  = 40
	tsChunkBaseSize = 28
)

func tsMetaFirstChunk(dataOff int) int  { return dataOff }
func tsMetaLastChunk(dataOff int) int   { return dataOff + 4 }
func tsMetaCount(dataOff int) int       { return dataOff + 8 }
func tsMetaMinTs(dataOff int) int       { return dataOff + 12 }
func tsMetaMaxTs(dataOff int) int       { return dataOff + 20 }
func tsMetaLabelsOff(dataOff int) int   { return dataOff + 28 }
func tsMetaLabelsCount(dataOff int) int  { return dataOff + 32 }

func tsChunkPrev(chunkOff int) int      { return chunkOff }
func tsChunkNext(chunkOff int) int      { return chunkOff + 4 }
func tsChunkCount(chunkOff int) int     { return chunkOff + 8 }
func tsChunkMinTs(chunkOff int) int     { return chunkOff + 12 }
func tsChunkMaxTs(chunkOff int) int     { return chunkOff + 20 }
func tsChunkSamples(chunkOff int) int   { return chunkOff + tsChunkBaseSize }

// TSAdd 向时间序列添加一个采样点。
func (db *RedisDB) TSAdd(key string, ts int64, val float64) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		chunkOff := db.tsNewChunk()
		db.tsChunkAddSample(chunkOff, 0, ts, val)

		dataOff := db.arena.Alloc(tsMetaBaseSize)
		db.arena.WriteUint32(tsMetaFirstChunk(dataOff), uint32(chunkOff))
		db.arena.WriteUint32(tsMetaLastChunk(dataOff), uint32(chunkOff))
		db.arena.WriteUint32(tsMetaCount(dataOff), 1)
		db.arena.WriteUint64(tsMetaMinTs(dataOff), uint64(ts))
		db.arena.WriteUint64(tsMetaMaxTs(dataOff), uint64(ts))

		headOff = db.NewObject(ObjTS, ObjEncodingRaw, dataOff)
		db.dict.Set(keyBytes, headOff)
		return 1
	}

	dataOff := db.ObjectDataOffset(headOff)
	lastChunkOff := int(db.arena.ReadUint32(tsMetaLastChunk(dataOff)))
	count := int(db.arena.ReadUint32(tsChunkCount(lastChunkOff)))

	if count >= tsMaxSamples {
		newChunkOff := db.tsNewChunk()
		db.arena.WriteUint32(tsChunkPrev(newChunkOff), uint32(lastChunkOff))
		db.arena.WriteUint32(tsChunkNext(lastChunkOff), uint32(newChunkOff))
		lastChunkOff = newChunkOff
		db.arena.WriteUint32(tsMetaLastChunk(dataOff), uint32(lastChunkOff))
		count = 0
	}

	db.tsChunkAddSample(lastChunkOff, count, ts, val)

	totalCount := db.arena.ReadUint32(tsMetaCount(dataOff))
	db.arena.WriteUint32(tsMetaCount(dataOff), totalCount+1)

	minTs := int64(db.arena.ReadUint64(tsMetaMinTs(dataOff)))
	maxTs := int64(db.arena.ReadUint64(tsMetaMaxTs(dataOff)))
	if ts < minTs {
		db.arena.WriteUint64(tsMetaMinTs(dataOff), uint64(ts))
	}
	if ts > maxTs {
		db.arena.WriteUint64(tsMetaMaxTs(dataOff), uint64(ts))
	}

	return int(totalCount + 1)
}

// TSAddWithLabels 向时间序列添加采样点并设置标签。
func (db *RedisDB) TSAddWithLabels(key string, ts int64, val float64, labels map[string]string) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		chunkOff := db.tsNewChunk()
		db.tsChunkAddSample(chunkOff, 0, ts, val)

		dataOff := db.arena.Alloc(tsMetaBaseSize)
		db.arena.WriteUint32(tsMetaFirstChunk(dataOff), uint32(chunkOff))
		db.arena.WriteUint32(tsMetaLastChunk(dataOff), uint32(chunkOff))
		db.arena.WriteUint32(tsMetaCount(dataOff), 1)
		db.arena.WriteUint64(tsMetaMinTs(dataOff), uint64(ts))
		db.arena.WriteUint64(tsMetaMaxTs(dataOff), uint64(ts))

		zlOff := db.tsNewLabelsZiplist(labels)
		db.arena.WriteUint32(tsMetaLabelsOff(dataOff), uint32(zlOff))
		db.arena.WriteUint32(tsMetaLabelsCount(dataOff), uint32(len(labels)))

		headOff = db.NewObject(ObjTS, ObjEncodingRaw, dataOff)
		db.dict.Set(keyBytes, headOff)
		return 1
	}

	dataOff := db.ObjectDataOffset(headOff)
	lastChunkOff := int(db.arena.ReadUint32(tsMetaLastChunk(dataOff)))
	count := int(db.arena.ReadUint32(tsChunkCount(lastChunkOff)))

	if count >= tsMaxSamples {
		newChunkOff := db.tsNewChunk()
		db.arena.WriteUint32(tsChunkPrev(newChunkOff), uint32(lastChunkOff))
		db.arena.WriteUint32(tsChunkNext(lastChunkOff), uint32(newChunkOff))
		lastChunkOff = newChunkOff
		db.arena.WriteUint32(tsMetaLastChunk(dataOff), uint32(lastChunkOff))
		count = 0
	}

	db.tsChunkAddSample(lastChunkOff, count, ts, val)

	totalCount := db.arena.ReadUint32(tsMetaCount(dataOff))
	db.arena.WriteUint32(tsMetaCount(dataOff), totalCount+1)

	minTs := int64(db.arena.ReadUint64(tsMetaMinTs(dataOff)))
	maxTs := int64(db.arena.ReadUint64(tsMetaMaxTs(dataOff)))
	if ts < minTs {
		db.arena.WriteUint64(tsMetaMinTs(dataOff), uint64(ts))
	}
	if ts > maxTs {
		db.arena.WriteUint64(tsMetaMaxTs(dataOff), uint64(ts))
	}

	return int(totalCount + 1)
}

// TSGetLabels 获取时间序列的标签。
func (db *RedisDB) TSGetLabels(key string) map[string]string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	zlOff := int(db.arena.ReadUint32(tsMetaLabelsOff(dataOff)))
	if zlOff == 0 {
		return nil
	}

	labels := make(map[string]string)
	n := ziplistLen(db.arena, zlOff)
	pos := zlOff + ziplistHeaderSize
	for i := 0; i < n; i += 2 {
		kBytes := ziplistGet(db.arena, zlOff, i)
		vBytes := ziplistGet(db.arena, zlOff, i+1)
		if kBytes != nil && vBytes != nil {
			labels[string(kBytes)] = string(vBytes)
		}
		pos += ziplistEntryTotalSize(db.arena, pos)
		pos += ziplistEntryTotalSize(db.arena, pos)
	}
	return labels
}

// tsNewLabelsZiplist 创建一个新的标签 ziplist。
func (db *RedisDB) tsNewLabelsZiplist(labels map[string]string) int {
	zlOff := ziplistNew(db.arena)
	for k, v := range labels {
		zlOff = ziplistInsert(db.arena, zlOff, []byte(k), false)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(v), false)
	}
	return zlOff
}

// TSRange 查询时间序列在指定时间范围内的采样点。
func (db *RedisDB) TSRange(key string, startTs, endTs int64) []TSPoint {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	chunkOff := int(db.arena.ReadUint32(tsMetaFirstChunk(dataOff)))
	var result []TSPoint

	for chunkOff != 0 {
		chunkMinTs := int64(db.arena.ReadUint64(tsChunkMinTs(chunkOff)))
		chunkMaxTs := int64(db.arena.ReadUint64(tsChunkMaxTs(chunkOff)))

		if chunkMaxTs >= startTs && chunkMinTs <= endTs {
			n := int(db.arena.ReadUint32(tsChunkCount(chunkOff)))
			samplesOff := tsChunkSamples(chunkOff)

			for i := 0; i < n; i++ {
				t := int64(db.arena.ReadUint64(samplesOff + i*tsSampleSize))
				v := db.arena.ReadFloat64(samplesOff + i*tsSampleSize + 8)
				if t >= startTs && t <= endTs {
					result = append(result, TSPoint{Timestamp: t, Value: v})
				}
			}
		}

		if chunkMinTs > endTs {
			break
		}

		chunkOff = int(db.arena.ReadUint32(tsChunkNext(chunkOff)))
	}

	return result
}

func (db *RedisDB) TSLast(key string) (int64, float64, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0, 0, false
	}

	dataOff := db.ObjectDataOffset(headOff)
	lastChunkOff := int(db.arena.ReadUint32(tsMetaLastChunk(dataOff)))
	if lastChunkOff == 0 {
		return 0, 0, false
	}

	n := int(db.arena.ReadUint32(tsChunkCount(lastChunkOff)))
	if n == 0 {
		return 0, 0, false
	}

	samplesOff := tsChunkSamples(lastChunkOff)
	lastIdx := n - 1
	t := int64(db.arena.ReadUint64(samplesOff + lastIdx*tsSampleSize))
	v := db.arena.ReadFloat64(samplesOff + lastIdx*tsSampleSize + 8)
	return t, v, true
}

func (db *RedisDB) TSDel(key string, startTs, endTs int64) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0
	}

	dataOff := db.ObjectDataOffset(headOff)
	chunkOff := int(db.arena.ReadUint32(tsMetaFirstChunk(dataOff)))
	deleted := 0

	for chunkOff != 0 {
		chunkMinTs := int64(db.arena.ReadUint64(tsChunkMinTs(chunkOff)))
		chunkMaxTs := int64(db.arena.ReadUint64(tsChunkMaxTs(chunkOff)))

		if chunkMaxTs >= startTs && chunkMinTs <= endTs {
			n := int(db.arena.ReadUint32(tsChunkCount(chunkOff)))
			samplesOff := tsChunkSamples(chunkOff)

			keep := make([]TSPoint, 0, n)
			for i := 0; i < n; i++ {
				t := int64(db.arena.ReadUint64(samplesOff + i*tsSampleSize))
				v := db.arena.ReadFloat64(samplesOff + i*tsSampleSize + 8)
				if t < startTs || t > endTs {
					keep = append(keep, TSPoint{Timestamp: t, Value: v})
				} else {
					deleted++
				}
			}

			db.arena.WriteUint32(tsChunkCount(chunkOff), 0)
			for i, pt := range keep {
				db.tsChunkAddSample(chunkOff, i, pt.Timestamp, pt.Value)
			}
		}

		if chunkMinTs > endTs {
			break
		}

		chunkOff = int(db.arena.ReadUint32(tsChunkNext(chunkOff)))
	}

	totalCount := int(db.arena.ReadUint32(tsMetaCount(dataOff)))
	db.arena.WriteUint32(tsMetaCount(dataOff), uint32(totalCount-deleted))

	return deleted
}

func (db *RedisDB) tsNewChunk() int {
	chunkOff := db.arena.Alloc(tsChunkSize)
	db.arena.WriteUint32(tsChunkPrev(chunkOff), 0)
	db.arena.WriteUint32(tsChunkNext(chunkOff), 0)
	db.arena.WriteUint32(tsChunkCount(chunkOff), 0)
	db.arena.WriteUint64(tsChunkMinTs(chunkOff), ^uint64(0))
	db.arena.WriteUint64(tsChunkMaxTs(chunkOff), 0)
	return chunkOff
}

func (db *RedisDB) tsChunkAddSample(chunkOff int, idx int, ts int64, val float64) {
	samplesOff := tsChunkSamples(chunkOff)
	off := samplesOff + idx*tsSampleSize
	db.arena.WriteUint64(off, uint64(ts))
	db.arena.WriteFloat64(off+8, val)

	db.arena.WriteUint32(tsChunkCount(chunkOff), uint32(idx+1))

	chunkMinTs := int64(db.arena.ReadUint64(tsChunkMinTs(chunkOff)))
	chunkMaxTs := int64(db.arena.ReadUint64(tsChunkMaxTs(chunkOff)))
	if ts < chunkMinTs {
		db.arena.WriteUint64(tsChunkMinTs(chunkOff), uint64(ts))
	}
	if ts > chunkMaxTs {
		db.arena.WriteUint64(tsChunkMaxTs(chunkOff), uint64(ts))
	}
}
