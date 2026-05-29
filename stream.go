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

// 流（Stream）实现，支持追加消费模型，包含消费者组（Consumer Group）功能。
// 流条目按时间戳+序列号组成的 ID 有序存储。
package gedis

import (
	"encoding/binary"
	"strconv"
	"strings"
)

// StreamEntry 流条目，包含唯一 ID 和键值对字段。
type StreamEntry struct {
	ID     string
	Fields map[string]*PooledBuffer
}

const (
	streamMetaSize  = 36
	streamEntryBase = 28
)

func streamMetaOff(dataOff int) int    { return dataOff }
func streamLastMs(dataOff int) int     { return dataOff }
func streamLastSeq(dataOff int) int    { return dataOff + 8 }
func streamFirstEntry(dataOff int) int { return dataOff + 16 }
func streamLastEntry(dataOff int) int  { return dataOff + 20 }
func streamEntryCount(dataOff int) int { return dataOff + 24 }
func streamGroupsDict(dataOff int) int { return dataOff + 28 }

func streamEntryMs(entOff int) int         { return entOff }
func streamEntrySeq(entOff int) int        { return entOff + 8 }
func streamEntryNext(entOff int) int       { return entOff + 16 }
func streamEntryPrev(entOff int) int       { return entOff + 20 }
func streamEntryFieldCount(entOff int) int { return entOff + 24 }
func streamEntryData(entOff int) int       { return entOff + streamEntryBase }

// XAdd 向流中添加一条新条目。自动生成或使用指定的 ID。
// 对应 Redis: XADD key ID field value [field value ...]
// 优化：入参使用 map[string]*PooledBuffer 替代 map[string][]byte，
// 调用方通过 Buf(s) 构建后即可 Close。
func (db *RedisDB) XAdd(key string, id string, fields map[string]*PooledBuffer) string {
	db.mu.Lock()
	defer db.mu.Unlock()

	ms, seq := parseStreamID(id)

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		dataOff := db.arena.Alloc(streamMetaSize)
		db.arena.WriteUint64(streamLastMs(dataOff), 0)
		db.arena.WriteUint64(streamLastSeq(dataOff), 0)
		db.arena.WriteUint32(streamFirstEntry(dataOff), 0)
		db.arena.WriteUint32(streamLastEntry(dataOff), 0)
		db.arena.WriteUint32(streamEntryCount(dataOff), 0)
		db.arena.WriteUint32(streamGroupsDict(dataOff), 0)

		lastMs := uint64(0)
		lastSeq := uint64(0)
		if ms == 0 && seq == 0 {
			ms = lastMs
			seq = lastSeq + 1
			if seq == 0 {
				ms++
			}
		}

		entOff := db.streamWriteEntry(ms, seq, fields)
		db.arena.WriteUint32(streamFirstEntry(dataOff), uint32(entOff))
		db.arena.WriteUint32(streamLastEntry(dataOff), uint32(entOff))
		db.arena.WriteUint32(streamEntryCount(dataOff), 1)
		db.arena.WriteUint64(streamLastMs(dataOff), uint64(ms))
		db.arena.WriteUint64(streamLastSeq(dataOff), uint64(seq))

		headOff = db.NewObject(ObjStream, ObjEncodingRaw, dataOff)
		db.dict.Set(keyBytes, headOff)
		return formatStreamID(ms, seq)
	}

	dataOff := db.ObjectDataOffset(headOff)
	lastMs := db.arena.ReadUint64(streamLastMs(dataOff))
	lastSeq := db.arena.ReadUint64(streamLastSeq(dataOff))

	if ms == 0 && seq == 0 {
		ms = lastMs
		seq = lastSeq + 1
		if seq == 0 {
			ms++
		}
	} else if ms == lastMs && seq <= lastSeq {
		seq = lastSeq + 1
		if seq == 0 {
			ms = lastMs + 1
		}
	}

	entOff := db.streamWriteEntry(ms, seq, fields)

	lastEntOff := int(db.arena.ReadUint32(streamLastEntry(dataOff)))
	if lastEntOff != 0 {
		db.arena.WriteUint32(streamEntryNext(lastEntOff), uint32(entOff))
		db.arena.WriteUint32(streamEntryPrev(entOff), uint32(lastEntOff))
	}

	db.arena.WriteUint32(streamLastEntry(dataOff), uint32(entOff))
	if db.arena.ReadUint32(streamFirstEntry(dataOff)) == 0 {
		db.arena.WriteUint32(streamFirstEntry(dataOff), uint32(entOff))
	}

	count := int(db.arena.ReadUint32(streamEntryCount(dataOff)))
	db.arena.WriteUint32(streamEntryCount(dataOff), uint32(count+1))
	db.arena.WriteUint64(streamLastMs(dataOff), ms)
	db.arena.WriteUint64(streamLastSeq(dataOff), seq)

	return formatStreamID(ms, seq)
}

// XRead 从多个流中读取从指定 ID 之后的新条目。
func (db *RedisDB) XRead(streams map[string]string, count int) map[string][]StreamEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[string][]StreamEntry)

	for key, startID := range streams {
		headOff, ok := db.dict.Get([]byte(key))
		if !ok {
			continue
		}

		enc := db.ObjectEncoding(headOff)
		dataOff := db.ObjectDataOffset(headOff)
		if enc != ObjEncodingRaw {
			continue
		}

		startMs, startSeq := parseStreamID(startID)

		entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
		entries := make([]StreamEntry, 0)

		for entOff != 0 {
			ms := db.arena.ReadUint64(streamEntryMs(entOff))
			seq := db.arena.ReadUint64(streamEntrySeq(entOff))

			if ms < startMs || (ms == startMs && seq <= startSeq) {
				entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
				continue
			}

			entry := db.streamReadEntry(entOff)
			entries = append(entries, entry)

			if count > 0 && len(entries) >= count {
				break
			}

			entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
		}

		if len(entries) > 0 {
			result[key] = entries
		}
	}

	return result
}

// XGroupCreate 为指定流创建消费者组。
func (db *RedisDB) XGroupCreate(key, group, startID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return &StreamError{"no such key"}
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))

	if groupsOff == 0 {
		groupsDict := NewDict(db.arena)
		metaOff := db.arena.Alloc(12)
		groupsDict.StoreMeta(metaOff)
		db.arena.WriteUint32(streamGroupsDict(dataOff), uint32(metaOff))
		groupsOff = metaOff
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)

	startMs, startSeq := parseStreamID(startID)
	groupDataOff := db.arena.Alloc(20)
	db.arena.WriteUint64(groupDataOff, startMs)
	db.arena.WriteUint64(groupDataOff+8, startSeq)
	db.arena.WriteUint32(groupDataOff+16, 0)

	groupsDict.Set([]byte(group), groupDataOff)
	groupsDict.StoreMeta(groupsOff)
	return nil
}

func (db *RedisDB) XDel(key string, ids []string) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	dataOff := db.ObjectDataOffset(headOff)
	deleted := 0

	for _, id := range ids {
		ms, seq := parseStreamID(id)

		entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
		for entOff != 0 {
			eMs := db.arena.ReadUint64(streamEntryMs(entOff))
			eSeq := db.arena.ReadUint64(streamEntrySeq(entOff))

			if eMs == ms && eSeq == seq {
				prevOff := int(db.arena.ReadUint32(streamEntryPrev(entOff)))
				nextOff := int(db.arena.ReadUint32(streamEntryNext(entOff)))

				if prevOff != 0 {
					db.arena.WriteUint32(streamEntryNext(prevOff), uint32(nextOff))
				} else {
					db.arena.WriteUint32(streamFirstEntry(dataOff), uint32(nextOff))
				}

				if nextOff != 0 {
					db.arena.WriteUint32(streamEntryPrev(nextOff), uint32(prevOff))
				} else {
					db.arena.WriteUint32(streamLastEntry(dataOff), uint32(prevOff))
				}

				db.arena.Free(entOff)
				deleted++

				count := int(db.arena.ReadUint32(streamEntryCount(dataOff)))
				db.arena.WriteUint32(streamEntryCount(dataOff), uint32(count-1))
				break
			}

			entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
		}
	}

	if int(db.arena.ReadUint32(streamEntryCount(dataOff))) == 0 {
		db.dict.Del(keyBytes)
		db.FreeObject(headOff)
	}

	return deleted
}

func (db *RedisDB) XTrim(key string, maxLen int) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	dataOff := db.ObjectDataOffset(headOff)
	originalCount := int(db.arena.ReadUint32(streamEntryCount(dataOff)))

	if originalCount <= maxLen {
		return 0
	}

	removed := 0
	targetRemove := originalCount - maxLen

	for removed < targetRemove {
		entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
		if entOff == 0 {
			break
		}

		nextOff := int(db.arena.ReadUint32(streamEntryNext(entOff)))

		if nextOff != 0 {
			db.arena.WriteUint32(streamEntryPrev(nextOff), 0)
			db.arena.WriteUint32(streamFirstEntry(dataOff), uint32(nextOff))
		} else {
			db.arena.WriteUint32(streamFirstEntry(dataOff), 0)
			db.arena.WriteUint32(streamLastEntry(dataOff), 0)
		}

		db.arena.Free(entOff)
		removed++
	}

	count := int(db.arena.ReadUint32(streamEntryCount(dataOff)))
	db.arena.WriteUint32(streamEntryCount(dataOff), uint32(count-removed))

	if count-removed == 0 {
		db.dict.Del(keyBytes)
		db.FreeObject(headOff)
	}

	return removed
}

func (db *RedisDB) XReadGroup(group, consumer string, streams map[string]string, count int) map[string][]StreamEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[string][]StreamEntry)

	for key, startID := range streams {
		headOff, ok := db.dict.Get([]byte(key))
		if !ok {
			continue
		}

		dataOff := db.ObjectDataOffset(headOff)
		groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
		if groupsOff == 0 {
			continue
		}

		groupsDict := LoadDictMeta(db.arena, groupsOff)
		_, ok = groupsDict.Get([]byte(group))
		if !ok {
			continue
		}

		startMs, startSeq := parseStreamID(startID)
		entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
		entries := make([]StreamEntry, 0)

		for entOff != 0 {
			ms := db.arena.ReadUint64(streamEntryMs(entOff))
			seq := db.arena.ReadUint64(streamEntrySeq(entOff))

			if ms < startMs || (ms == startMs && seq <= startSeq) {
				entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
				continue
			}

			entry := db.streamReadEntry(entOff)
			entries = append(entries, entry)

			if count > 0 && len(entries) >= count {
				break
			}

			entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
		}

		if len(entries) > 0 {
			result[key] = entries
		}
	}

	return result
}

func (db *RedisDB) XLen(key string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return 0
	}

	dataOff := db.ObjectDataOffset(headOff)
	return int(db.arena.ReadUint32(streamEntryCount(dataOff)))
}

func (db *RedisDB) streamWriteEntry(ms, seq uint64, fields map[string]*PooledBuffer) int {
	fieldCount := len(fields)

	totalFieldsSize := 0
	for k, v := range fields {
		totalFieldsSize += 4 + len(k) + 4 + v.Len()
	}

	entSize := streamEntryBase + totalFieldsSize
	entOff := db.arena.Alloc(entSize)

	db.arena.WriteUint64(streamEntryMs(entOff), ms)
	db.arena.WriteUint64(streamEntrySeq(entOff), seq)
	db.arena.WriteUint32(streamEntryNext(entOff), 0)
	db.arena.WriteUint32(streamEntryPrev(entOff), 0)
	db.arena.WriteUint32(streamEntryFieldCount(entOff), uint32(fieldCount))

	pos := streamEntryData(entOff)
	for k, v := range fields {
		kb := []byte(k)
		db.arena.WriteUint32(pos, uint32(len(kb)))
		pos += 4
		db.arena.WriteBytes(pos, kb)
		pos += len(kb)
		vb := v.Bytes()
		db.arena.WriteUint32(pos, uint32(len(vb)))
		pos += 4
		db.arena.WriteBytes(pos, vb)
		pos += len(vb)
	}

	return entOff
}

func (db *RedisDB) streamReadEntry(entOff int) StreamEntry {
	ms := db.arena.ReadUint64(streamEntryMs(entOff))
	seq := db.arena.ReadUint64(streamEntrySeq(entOff))
	fieldCount := int(db.arena.ReadUint32(streamEntryFieldCount(entOff)))

	fields := make(map[string]*PooledBuffer, fieldCount)
	pos := streamEntryData(entOff)

	for i := 0; i < fieldCount; i++ {
		kLen := int(db.arena.ReadUint32(pos))
		pos += 4
		key := db.arena.ReadBytes(pos, kLen)
		pos += kLen
		vLen := int(db.arena.ReadUint32(pos))
		pos += 4
		val := db.arena.ReadBytes(pos, vLen)
		pos += vLen
		pb := NewBuf(len(val))
		pb.buf.Write(val)
		fields[string(key)] = pb
	}

	return StreamEntry{
		ID:     formatStreamID(ms, seq),
		Fields: fields,
	}
}

func parseStreamID(id string) (ms uint64, seq uint64) {
	if id == "*" {
		return 0, 0
	}
	if id == "0" || id == "0-0" {
		return 0, 0
	}
	if id == "$" {
		return ^uint64(0), ^uint64(0)
	}

	parts := strings.SplitN(id, "-", 2)
	if len(parts) >= 1 {
		ms, _ = strconv.ParseUint(parts[0], 10, 64)
	}
	if len(parts) >= 2 {
		seq, _ = strconv.ParseUint(parts[1], 10, 64)
	}
	return
}

func formatStreamID(ms, seq uint64) string {
	return strconv.FormatUint(ms, 10) + "-" + strconv.FormatUint(seq, 10)
}

type StreamError struct {
	Message string
}

func (e *StreamError) Error() string {
	return e.Message
}

func (db *RedisDB) XInfo(key string) map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)

	firstEntOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
	lastEntOff := int(db.arena.ReadUint32(streamLastEntry(dataOff)))

	var firstID, lastID string
	var firstMs, firstSeq, lastMs, lastSeq uint64

	if firstEntOff != 0 {
		firstMs = db.arena.ReadUint64(streamEntryMs(firstEntOff))
		firstSeq = db.arena.ReadUint64(streamEntrySeq(firstEntOff))
		firstID = formatStreamID(firstMs, firstSeq)
	}

	if lastEntOff != 0 {
		lastMs = db.arena.ReadUint64(streamEntryMs(lastEntOff))
		lastSeq = db.arena.ReadUint64(streamEntrySeq(lastEntOff))
		lastID = formatStreamID(lastMs, lastSeq)
	}

	entryCount := int(db.arena.ReadUint32(streamEntryCount(dataOff)))
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))

	groupCount := 0
	if groupsOff != 0 {
		groupsDict := LoadDictMeta(db.arena, groupsOff)
		groupCount = groupsDict.used
	}

	return map[string]interface{}{
		"length":         entryCount,
		"first-entry":    firstID,
		"last-entry":     lastID,
		"groups":         groupCount,
		"stream-live-sec": 0,
	}
}

func (db *RedisDB) XInfoGroups(key string) []map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return nil
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	result := make([]map[string]interface{}, 0, groupsDict.used)

	for i := 0; i < groupsDict.size; i++ {
		keyArenaOff, valOff := groupsDict.getSlot(i)
		if keyArenaOff == 0 {
			continue
		}

		keySize := db.arena.SizeAt(keyArenaOff)
		groupNameBytes := db.arena.ReadBytes(keyArenaOff, keySize)
		groupName := string(groupNameBytes)
		groupDataOff := valOff

		startMs := db.arena.ReadUint64(groupDataOff)
		startSeq := db.arena.ReadUint64(groupDataOff + 8)
		consumersDictOff := int(db.arena.ReadUint32(groupDataOff + 16))

		consumerCount := 0
		if consumersDictOff != 0 {
			consumersDict := LoadDictMeta(db.arena, consumersDictOff)
			consumerCount = consumersDict.used
		}

		result = append(result, map[string]interface{}{
			"name":            groupName,
			"last-delivered":  formatStreamID(startMs, startSeq),
			"consumers":       consumerCount,
			"pending":         0,
		})
	}

	return result
}

func (db *RedisDB) XClaim(key, group, consumer string, minIdleMs int64, ids []string) []StreamEntry {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return nil
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	groupDataOff, ok := groupsDict.Get([]byte(group))
	if !ok {
		return nil
	}

	consumersDictOff := int(db.arena.ReadUint32(groupDataOff + 16))

	var consumersDict *Dict
	if consumersDictOff == 0 {
		consumersDict = NewDict(db.arena)
		metaOff := db.arena.Alloc(12)
		consumersDict.StoreMeta(metaOff)
		consumersDictOff = metaOff
		db.arena.WriteUint32(groupDataOff+16, uint32(consumersDictOff))
	} else {
		consumersDict = LoadDictMeta(db.arena, consumersDictOff)
	}

	consumerDataOffVal, exists := consumersDict.Get([]byte(consumer))
	if !exists {
		consumerDataOffVal = db.arena.Alloc(16)
		db.arena.WriteUint64(consumerDataOffVal, 0)
		db.arena.WriteUint64(consumerDataOffVal+8, 0)
		consumersDict.Set([]byte(consumer), consumerDataOffVal)
		consumersDict.StoreMeta(consumersDictOff)
	}

	result := make([]StreamEntry, 0, len(ids))

	for _, id := range ids {
		ms, seq := parseStreamID(id)
		entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))

		for entOff != 0 {
			eMs := db.arena.ReadUint64(streamEntryMs(entOff))
			eSeq := db.arena.ReadUint64(streamEntrySeq(entOff))

			if eMs == ms && eSeq == seq {
				entry := db.streamReadEntry(entOff)
				result = append(result, entry)
				break
			}
			entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
		}
	}

	groupsDict.StoreMeta(groupsOff)
	return result
}

func (db *RedisDB) XAutoClaim(key, group, consumer string, start string, count int) (string, []StreamEntry) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return "", nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return "", nil
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	_, ok = groupsDict.Get([]byte(group))
	if !ok {
		return "", nil
	}

	startMs, startSeq := parseStreamID(start)

	entOff := int(db.arena.ReadUint32(streamFirstEntry(dataOff)))
	result := make([]StreamEntry, 0, count)
	nextStartID := ""

	for entOff != 0 && len(result) < count {
		eMs := db.arena.ReadUint64(streamEntryMs(entOff))
		eSeq := db.arena.ReadUint64(streamEntrySeq(entOff))

		if eMs > startMs || (eMs == startMs && eSeq > startSeq) {
			entry := db.streamReadEntry(entOff)
			result = append(result, entry)
			nextStartID = formatStreamID(eMs, eSeq)
		}
		entOff = int(db.arena.ReadUint32(streamEntryNext(entOff)))
	}

	groupsDict.StoreMeta(groupsOff)
	return nextStartID, result
}

func (db *RedisDB) XPending(key, group string) []map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return nil
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	_, ok = groupsDict.Get([]byte(group))
	if !ok {
		return nil
	}

	return []map[string]interface{}{}
}

func (db *RedisDB) XGroupCreateConsumer(key, group, consumer string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return false
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	groupDataOff, ok := groupsDict.Get([]byte(group))
	if !ok {
		return false
	}

	consumersDictOff := int(db.arena.ReadUint32(groupDataOff + 16))

	var consumersDict *Dict
	if consumersDictOff == 0 {
		consumersDict = NewDict(db.arena)
		metaOff := db.arena.Alloc(12)
		consumersDict.StoreMeta(metaOff)
		consumersDictOff = metaOff
		db.arena.WriteUint32(groupDataOff+16, uint32(consumersDictOff))
	} else {
		consumersDict = LoadDictMeta(db.arena, consumersDictOff)
	}

	_, exists := consumersDict.Get([]byte(consumer))
	if exists {
		return true
	}

	consumerDataOff := db.arena.Alloc(16)
	db.arena.WriteUint64(consumerDataOff, 0)
	db.arena.WriteUint64(consumerDataOff+8, 0)
	consumersDict.Set([]byte(consumer), consumerDataOff)
	consumersDict.StoreMeta(consumersDictOff)
	groupsDict.StoreMeta(groupsOff)

	return true
}

func (db *RedisDB) XGroupDelConsumer(key, group, consumer string) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	dataOff := db.ObjectDataOffset(headOff)
	groupsOff := int(db.arena.ReadUint32(streamGroupsDict(dataOff)))
	if groupsOff == 0 {
		return 0
	}

	groupsDict := LoadDictMeta(db.arena, groupsOff)
	groupDataOff, ok := groupsDict.Get([]byte(group))
	if !ok {
		return 0
	}

	consumersDictOff := int(db.arena.ReadUint32(groupDataOff + 16))
	if consumersDictOff == 0 {
		return 0
	}

	consumersDict := LoadDictMeta(db.arena, consumersDictOff)
	consumerKey := []byte(consumer)
	if consumersDict.Del(consumerKey) {
		consumersDict.StoreMeta(consumersDictOff)
		groupsDict.StoreMeta(groupsOff)
		return 1
	}

	return 0
}

var _ = binary.LittleEndian
