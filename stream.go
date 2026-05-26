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
	"encoding/binary"
	"strconv"
	"strings"
)

type StreamEntry struct {
	ID     string
	Fields map[string][]byte
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

func (db *RedisDB) XAdd(key string, id string, fields map[string][]byte) string {
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
	return nil
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

func (db *RedisDB) streamWriteEntry(ms, seq uint64, fields map[string][]byte) int {
	fieldCount := len(fields)

	totalFieldsSize := 0
	for k, v := range fields {
		totalFieldsSize += 4 + len(k) + 4 + len(v)
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
		db.arena.WriteUint32(pos, uint32(len(v)))
		pos += 4
		db.arena.WriteBytes(pos, v)
		pos += len(v)
	}

	return entOff
}

func (db *RedisDB) streamReadEntry(entOff int) StreamEntry {
	ms := db.arena.ReadUint64(streamEntryMs(entOff))
	seq := db.arena.ReadUint64(streamEntrySeq(entOff))
	fieldCount := int(db.arena.ReadUint32(streamEntryFieldCount(entOff)))

	fields := make(map[string][]byte, fieldCount)
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
		fields[string(key)] = val
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

var _ = binary.LittleEndian
