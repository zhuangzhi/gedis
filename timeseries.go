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
		db.arena.WriteUint32(tsMetaLabelsOff(dataOff), 0)

		headOff := db.NewObject(ObjTS, ObjEncodingRaw, dataOff)
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

// TSAggregation 聚合函数类型
type TSAggregation string

const (
	TSAggAvg   TSAggregation = "avg"
	TSAggSum   TSAggregation = "sum"
	TSAggMin   TSAggregation = "min"
	TSAggMax   TSAggregation = "max"
	TSAggCount TSAggregation = "count"
	TSAggFirst TSAggregation = "first"
	TSAggLast  TSAggregation = "last"
	TSAggStdP  TSAggregation = "std.p"
	TSAggVarP  TSAggregation = "var.p"
	TSAggRange TSAggregation = "range"
)

// TSRule 压缩/下采样规则
type TSRule struct {
	DestKey    string         // 目标时间序列键
	BucketSize int64          // 桶大小（毫秒）
	Aggregator TSAggregation  // 聚合函数
}

// TSMGETResult TS.MGET 查询结果
type TSMGETResult struct {
	Key     string
	Labels  map[string]string
	LatestTs int64
	LatestVal float64
}

// TSMQueryResult TS.MRANGE/TS.MREVRANGE 查询结果
type TSMQueryResult struct {
	Key     string
	Labels  map[string]string
	Points  []TSPoint
}

// TSQueryIndexResult TS.QUERYINDEX 查询结果
type TSQueryIndexResult struct {
	Key    string
	Labels map[string]string
}

func (db *RedisDB) TSAggregate(points []TSPoint, agg TSAggregation) float64 {
	if len(points) == 0 {
		return 0
	}

	switch agg {
	case TSAggAvg:
		sum := 0.0
		for _, p := range points {
			sum += p.Value
		}
		return sum / float64(len(points))
	case TSAggSum:
		sum := 0.0
		for _, p := range points {
			sum += p.Value
		}
		return sum
	case TSAggMin:
		min := points[0].Value
		for _, p := range points {
			if p.Value < min {
				min = p.Value
			}
		}
		return min
	case TSAggMax:
		max := points[0].Value
		for _, p := range points {
			if p.Value > max {
				max = p.Value
			}
		}
		return max
	case TSAggCount:
		return float64(len(points))
	case TSAggFirst:
		return points[0].Value
	case TSAggLast:
		return points[len(points)-1].Value
	case TSAggRange:
		min := points[0].Value
		max := points[0].Value
		for _, p := range points {
			if p.Value < min {
				min = p.Value
			}
			if p.Value > max {
				max = p.Value
			}
		}
		return max - min
	case TSAggStdP, TSAggVarP:
		mean := 0.0
		for _, p := range points {
			mean += p.Value
		}
		mean /= float64(len(points))
		var sumSq float64
		for _, p := range points {
			diff := p.Value - mean
			sumSq += diff * diff
		}
		variance := sumSq / float64(len(points))
		if agg == TSAggVarP {
			return variance
		}
		return variance
	default:
		return points[0].Value
	}
}

func (db *RedisDB) TSRangeWithAgg(key string, startTs, endTs int64, agg TSAggregation, bucketSize int64) []TSPoint {
	if startTs > endTs {
		return nil
	}
	
	points := db.TSRange(key, startTs, endTs)
	if len(points) == 0 || bucketSize <= 0 {
		return points
	}

	var buckets [][]TSPoint
	bucketMap := make(map[int64][]TSPoint)

	for _, p := range points {
		bucketTs := (p.Timestamp / bucketSize) * bucketSize
		bucketMap[bucketTs] = append(bucketMap[bucketTs], p)
	}

	for _, ts := range sortedKeys(bucketMap) {
		buckets = append(buckets, bucketMap[ts])
	}

	var result []TSPoint
	for _, bucket := range buckets {
		aggVal := db.TSAggregate(bucket, agg)
		result = append(result, TSPoint{
			Timestamp: bucket[0].Timestamp,
			Value:     aggVal,
		})
	}

	return result
}

func (db *RedisDB) TSRevRange(key string, startTs, endTs int64) []TSPoint {
	points := db.TSRange(key, startTs, endTs)
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
	return points
}

func (db *RedisDB) TSRevRangeWithAgg(key string, startTs, endTs int64, agg TSAggregation, bucketSize int64) []TSPoint {
	points := db.TSRangeWithAgg(key, startTs, endTs, agg, bucketSize)
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
	return points
}

// TSCreateRule 创建压缩/下采样规则
func (db *RedisDB) TSCreateRule(key, destKey string, bucketSize int64, agg TSAggregation) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return false
	}

	rulesOff := db.arena.Alloc(16 + len(destKey) + 1)
	db.arena.WriteUint64(rulesOff, uint64(bucketSize))
	db.arena.WriteUint64(rulesOff+8, uint64(len(destKey)))
	db.arena.WriteBytes(rulesOff+16, []byte(destKey))
	db.arena.WriteByte(rulesOff+16+len(destKey), 0)

	db.tsSetRules(db.ObjectDataOffset(headOff), rulesOff)

	return true
}

func (db *RedisDB) tsSetRules(dataOff, rulesOff int) {
	db.arena.WriteUint32(dataOff+36, uint32(rulesOff))
}

// TSDeleteRule 删除压缩规则
func (db *RedisDB) TSDeleteRule(key, destKey string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return false
	}

	_ = db.ObjectDataOffset(headOff)

	return true
}

// TSMGET 批量获取多个时间序列的最新数据点
func (db *RedisDB) TSMGET(keys []string) []TSMGETResult {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []TSMGETResult
	for _, key := range keys {
		headOff, ok := db.dict.Get([]byte(key))
		if !ok {
			continue
		}

		_ = db.ObjectDataOffset(headOff)
		labels := db.TSGetLabels(key)

		ts, val, ok := db.TSLast(key)
		if !ok {
			continue
		}

		results = append(results, TSMGETResult{
			Key:      key,
			Labels:   labels,
			LatestTs: ts,
			LatestVal: val,
		})
	}

	return results
}

// TSMRANGE 批量范围查询，支持标签过滤
func (db *RedisDB) TSMRANGE(startTs, endTs int64, filter map[string]string, agg TSAggregation, bucketSize int64, reverse bool) []TSMQueryResult {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []TSMQueryResult

	iter := db.dict.Iterator()
	for iter.Next() {
		key := string(iter.Key())
		headOff := iter.Value()

		objType := db.ObjectType(headOff)
		if objType != ObjTS {
			continue
		}

		labels := db.TSGetLabels(key)

		if !db.tsLabelsMatch(filter, labels) {
			continue
		}

		var points []TSPoint
		if reverse {
			points = db.TSRevRangeWithAgg(key, startTs, endTs, agg, bucketSize)
		} else {
			points = db.TSRangeWithAgg(key, startTs, endTs, agg, bucketSize)
		}

		results = append(results, TSMQueryResult{
			Key:    key,
			Labels: labels,
			Points: points,
		})
	}

	return results
}

func (db *RedisDB) tsLabelsMatch(filter, labels map[string]string) bool {
	for k, v := range filter {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// TSQUERYINDEX 按标签查询时间序列
func (db *RedisDB) TSQUERYINDEX(filter map[string]string) []TSQueryIndexResult {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []TSQueryIndexResult

	iter := db.dict.Iterator()
	for iter.Next() {
		key := string(iter.Key())
		headOff := iter.Value()

		objType := db.ObjectType(headOff)
		if objType != ObjTS {
			continue
		}

		labels := db.TSGetLabels(key)

		if !db.tsLabelsMatch(filter, labels) {
			continue
		}

		results = append(results, TSQueryIndexResult{
			Key:    key,
			Labels: labels,
		})
	}

	return results
}

type tsBucket struct {
	timestamp int64
	points    []TSPoint
}

func (db *RedisDB) tsDownsample(points []TSPoint, bucketSize int64, agg TSAggregation) []TSPoint {
	if len(points) == 0 || bucketSize <= 0 {
		return points
	}

	buckets := make(map[int64][]TSPoint)
	for _, p := range points {
		bucketTs := (p.Timestamp / bucketSize) * bucketSize
		buckets[bucketTs] = append(buckets[bucketTs], p)
	}

	var result []TSPoint
	for _, ts := range sortedKeys(buckets) {
		aggVal := db.TSAggregate(buckets[ts], agg)
		result = append(result, TSPoint{
			Timestamp: ts,
			Value:     aggVal,
		})
	}

	return result
}

func sortedKeys(m map[int64][]TSPoint) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
