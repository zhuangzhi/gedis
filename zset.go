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
	"strconv"
	"time"
)

// ZAdd 向有序集合中添加成员及其分数。对外友好 API，member 入参 []byte。
func (db *RedisDB) ZAdd(key string, score float64, member []byte) int {
	pb := BufFromBytes(member)
	result := db.ZAddBuffer(key, score, pb)
	pb.Close()
	return result
}

// ZAddBuffer 向有序集合中添加成员，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) ZAddBuffer(key string, score float64, member *PooledBuffer) int {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.zAddBufferLocked(key, score, member)
}

func (db *RedisDB) zAddBufferLocked(key string, score float64, member *PooledBuffer) int {
	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zsl := zslCreate(db.arena)
		memberOff := db.arena.AllocBytes(member.Bytes())
		nodeOff := zslInsert(db.arena, &zsl, memberOff, score)
		if nodeOff == 0 {
			db.arena.Free(memberOff)
			return 0
		}

		dictDataOff := db.arena.Alloc(4 + 4*4)
		db.arena.WriteUint32(dictDataOff, uint32(zsl.headerOff))
		db.arena.WriteUint32(dictDataOff+4, uint32(zsl.tailOff))
		db.arena.WriteUint32(dictDataOff+8, uint32(zsl.length))
		db.arena.WriteUint32(dictDataOff+12, uint32(zsl.level))

		headOff = db.NewObject(ObjZSet, ObjEncodingSkiplist, dictDataOff)
		db.dict.Set(keyBytes, headOff)
		return 1
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		memberOff := db.arena.AllocBytes(member.Bytes())
		nodeOff := zslInsert(db.arena, &zsl, memberOff, score)
		if nodeOff == 0 {
			db.arena.Free(memberOff)
			return 0
		}
		zslSaveToArena(db.arena, dataOff, &zsl)
		return 1
	}

	return 0
}

// ZRem 从有序集合中删除指定成员。对外友好 API，member 入参 []byte。
func (db *RedisDB) ZRem(key string, member []byte) bool {
	pb := BufFromBytes(member)
	result := db.ZRemBuffer(key, pb)
	pb.Close()
	return result
}

// ZRemBuffer 从有序集合中删除指定成员，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) ZRemBuffer(key string, member *PooledBuffer) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		found := false
		var score float64
		for x != 0 {
			xScore := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			xMember := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
			if bytesEqual(xMember, member.Bytes()) {
				score = xScore
				found = true
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}

		if !found {
			return false
		}

		memberOff := db.arena.AllocBytes(member.Bytes())
		result := zslDelete(db.arena, &zsl, memberOff, score)
		db.arena.Free(memberOff)

		if result {
			zslSaveToArena(db.arena, dataOff, &zsl)
			if zsl.length == 0 {
				db.dict.Del(keyBytes)
				db.FreeObject(headOff)
			}
		}
		return result
	}

	return false
}

// ZScore 获取有序集合中指定成员的分数。对外友好 API，member 入参 []byte。
func (db *RedisDB) ZScore(key string, member []byte) (float64, bool) {
	pb := BufFromBytes(member)
	result1, result2 := db.ZScoreBuffer(key, pb)
	pb.Close()
	return result1, result2
}

// ZScoreBuffer 获取有序集合中指定成员的分数，入参 *PooledBuffer 避免堆分配。
func (db *RedisDB) ZScoreBuffer(key string, member *PooledBuffer) (float64, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0, false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			xScore := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			xMember := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
			if bytesEqual(xMember, member.Bytes()) {
				return xScore, true
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
	}

	return 0, false
}

// ZRange 获取有序集合中指定排名范围的成员。返回 *ZSlices，遍历后须 zs.Close()。
func (db *RedisDB) ZRange(key string, start, stop int) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		n := zsl.length

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

		totalBytes := 0
		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx := 0
		for x != 0 {
			if idx >= start {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				totalBytes += db.arena.SizeAt(xMemberOff)
			}
			if idx >= stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}

		result := NewZSlices()
		x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx = 0
		out := 0
		for x != 0 {
			if idx >= start && idx <= stop {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				sz := db.arena.SizeAt(xMemberOff)
				result.Add(db.arena.GetSlice(xMemberOff, sz))
				out++
			}
			if idx > stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}
		_ = totalBytes
		_ = out
		result.Finish()
		return result
	}

	return nil
}

// ZRangeIter 对有序集合指定排名范围的成员进行零分配回调遍历。
// fn 接收的 []byte 仅在回调内有效，不得持有到回调外。
func (db *RedisDB) ZRangeIter(key string, start, stop int, fn func(member []byte)) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		n := zsl.length

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
			return
		}

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx := 0
		for x != 0 {
			if idx >= start && idx <= stop {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				fn(db.arena.GetSlice(xMemberOff, db.arena.SizeAt(xMemberOff)))
			}
			if idx > stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}
	}
}

// ZRangeWithScores 获取有序集合中指定排名范围的成员及其分数。
// 成员通过 *ZSlices 返回，scores 为对应的分数数组。遍历后须 zs.Close()。
func (db *RedisDB) ZRangeWithScores(key string, start, stop int) (*ZSlices, []float64) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		n := zsl.length

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
			return nil, nil
		}

		members := NewZSlices()
		scores := make([]float64, 0, stop-start+1)

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx := 0
		for x != 0 {
			if idx >= start && idx <= stop {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				member := db.arena.GetSlice(xMemberOff, db.arena.SizeAt(xMemberOff))
				score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
				members.Add(member)
				scores = append(scores, score)
			}
			if idx > stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}
		members.Finish()
		return members, scores
	}

	return nil, nil
}

// ZRangeByScore 获取有序集合中分数在 [min, max] 范围内的成员。返回 *ZSlices。
func (db *RedisDB) ZRangeByScore(key string, min, max float64) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		result := NewZSlices()

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			if score >= min && score <= max {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				member := db.arena.GetSlice(xMemberOff, db.arena.SizeAt(xMemberOff))
				result.Add(member)
			}
			if score > max {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
		result.Finish()
		return result
	}

	return nil
}

// ZRemRangeByScore 删除有序集合中分数在 [min, max] 范围内的成员。
func (db *RedisDB) ZRemRangeByScore(key string, min, max float64) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		removed := 0

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			next := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			if score >= min && score <= max {
				var update [zskiplistMaxLevel]int
				ptr := zsl.headerOff
				for i := zsl.level - 1; i >= 0; i-- {
					for {
						fwd := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, ptr, i)))
						if fwd == 0 || fwd == x {
							update[i] = ptr
							break
						}
						fwdScore := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, fwd))
						if fwdScore > score {
							update[i] = ptr
							break
						}
						ptr = fwd
					}
				}
				zslDeleteNode(db.arena, &zsl, x, update[:])
				removed++
			}
			x = next
		}

		zslSaveToArena(db.arena, dataOff, &zsl)
		if zsl.length == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
		return removed
	}

	return 0
}

// ZCard 获取有序集合中的成员数量。
func (db *RedisDB) ZCard(key string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		return zsl.length
	}

	if enc == ObjEncodingZiplist {
		n := ziplistLen(db.arena, dataOff)
		return n / 2
	}

	return 0
}

func (db *RedisDB) ZCount(key string, min, max float64) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	count := 0
	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			if score >= min && score <= max {
				count++
			}
			if score > max {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
	} else if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			v := ziplistGet(db.arena, zlOff, i+1)
			score, _ := strconv.ParseFloat(string(v), 64)
			if score >= min && score <= max {
				count++
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}
	}

	return count
}

func (db *RedisDB) ZLexCount(key string, min, max string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	count := 0
	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			member := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
			memberStr := string(member)

			minOk := true
			if len(min) > 0 && min != "-" && min != "+" {
				if min[0] == '(' {
					minOk = memberStr > min[1:]
				} else if min[0] == '[' {
					minOk = memberStr >= min[1:]
				} else {
					minOk = memberStr >= min
				}
			}

			maxOk := true
			if len(max) > 0 && max != "+" && max != "-" {
				if max[0] == '(' {
					maxOk = memberStr < max[1:]
				} else if max[0] == '[' {
					maxOk = memberStr <= max[1:]
				} else {
					maxOk = memberStr <= max
				}
			}

			if minOk && maxOk {
				count++
			}

			if memberStr > stripLexBound(max) && max != "+" && max != "-" {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
	}

	return count
}

func stripLexBound(s string) string {
	if len(s) > 0 && (s[0] == '[' || s[0] == '(') {
		return s[1:]
	}
	return s
}

func (db *RedisDB) ZRangeByLex(key string, min, max string) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		result := NewZSlices()

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			member := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
			memberStr := string(member)

			minOk := true
			if len(min) > 0 && min != "-" && min != "+" {
				if min[0] == '(' {
					minOk = memberStr > min[1:]
				} else if min[0] == '[' {
					minOk = memberStr >= min[1:]
				} else {
					minOk = memberStr >= min
				}
			}

			maxOk := true
			if len(max) > 0 && max != "+" && max != "-" {
				if max[0] == '(' {
					maxOk = memberStr < max[1:]
				} else if max[0] == '[' {
					maxOk = memberStr <= max[1:]
				} else {
					maxOk = memberStr <= max
				}
			}

			if minOk && maxOk {
				result.Add(member)
			}

			if memberStr > stripLexBound(max) && max != "+" && max != "-" {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}

		result.Finish()
		return result
	}

	return nil
}

func zslLoadFromArena(arena *Arena, dataOff int) ZSkipList {
	return ZSkipList{
		headerOff: int(arena.ReadUint32(dataOff)),
		tailOff:   int(arena.ReadUint32(dataOff + 4)),
		length:    int(arena.ReadUint32(dataOff + 8)),
		level:     int(arena.ReadUint32(dataOff + 12)),
	}
}

func zslSaveToArena(arena *Arena, dataOff int, zsl *ZSkipList) {
	arena.WriteUint32(dataOff, uint32(zsl.headerOff))
	arena.WriteUint32(dataOff+4, uint32(zsl.tailOff))
	arena.WriteUint32(dataOff+8, uint32(zsl.length))
	arena.WriteUint32(dataOff+12, uint32(zsl.level))
}

func (db *RedisDB) ZRank(key string, member []byte) (int, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0, false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		memberOff := db.arena.AllocBytes(member)
		defer db.arena.Free(memberOff)

		score, ok := db.ZScoreBuffer(key, BufFromBytes(member))
		if !ok {
			return 0, false
		}

		rank := zslGetRank(db.arena, &zsl, memberOff, score)
		if rank == 0 {
			return 0, false
		}
		return rank - 1, true
	}
	return 0, false
}

func (db *RedisDB) ZRevRank(key string, member []byte) (int, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0, false
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		score, ok := db.ZScoreBuffer(key, BufFromBytes(member))
		if !ok {
			return 0, false
		}

		memberOff := db.arena.AllocBytes(member)
		defer db.arena.Free(memberOff)

		rank := zslGetRank(db.arena, &zsl, memberOff, score)
		if rank == 0 {
			return 0, false
		}
		return zsl.length - rank, true
	}
	return 0, false
}

func (db *RedisDB) ZIncrBy(key string, member []byte, delta float64) float64 {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zlOff := ziplistNew(db.arena)
		zlOff = ziplistInsert(db.arena, zlOff, member, false)
		scoreStr := strconv.FormatFloat(delta, 'f', -1, 64)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(scoreStr), false)
		headOff = db.NewObject(ObjZSet, ObjEncodingZiplist, zlOff)
		db.dict.Set(keyBytes, headOff)
		return delta
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		var existingScore float64
		var found bool

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			xScore := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			xMember := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
			if bytesEqual(xMember, member) {
				existingScore = xScore
				found = true
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}

		if !found {
			newScore := delta
			memberOff := db.arena.AllocBytes(member)
			zslInsert(db.arena, &zsl, memberOff, newScore)
			zslSaveToArena(db.arena, dataOff, &zsl)
			return newScore
		}
		newScore := existingScore + delta
		memberOff := db.arena.AllocBytes(member)
		zslDelete(db.arena, &zsl, memberOff, existingScore)
		zslInsert(db.arena, &zsl, memberOff, newScore)
		zslSaveToArena(db.arena, dataOff, &zsl)
		return newScore
	}

	if enc == ObjEncodingZiplist {
		zlOff := dataOff
		n := ziplistLen(db.arena, zlOff)
		pos := zlOff + ziplistHeaderSize
		for i := 0; i < n; i += 2 {
			if ziplistEntryDataEquals(db.arena, pos, member) {
				valBytes := ziplistGet(db.arena, zlOff, i+1)
				oldScore, _ := strconv.ParseFloat(string(valBytes), 64)
				newScore := oldScore + delta
				newScoreStr := strconv.FormatFloat(newScore, 'f', -1, 64)
				zlOff = ziplistDelete(db.arena, zlOff, i+1)
				zlOff = ziplistInsertAt(db.arena, zlOff, i+1, []byte(newScoreStr))
				db.ObjectSetDataOffset(headOff, zlOff)
				return newScore
			}
			pos += ziplistEntryTotalSize(db.arena, pos)
			pos += ziplistEntryTotalSize(db.arena, pos)
		}
		newScore := delta
		zlOff = ziplistInsert(db.arena, zlOff, member, false)
		scoreStr := strconv.FormatFloat(newScore, 'f', -1, 64)
		zlOff = ziplistInsert(db.arena, zlOff, []byte(scoreStr), false)
		db.ObjectSetDataOffset(headOff, zlOff)
		return newScore
	}
	return delta
}

func (db *RedisDB) ZPopMin(key string, count int) *ZSlices {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	result := NewZSlices()
	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)
		for i := 0; i < count && zsl.length > 0; i++ {
			x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
			if x == 0 {
				break
			}
			memberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			member := db.arena.ReadBytes(memberOff, db.arena.SizeAt(memberOff))
			result.Add(member)
			zslDelete(db.arena, &zsl, memberOff, score)
		}
		zslSaveToArena(db.arena, dataOff, &zsl)
		if zsl.length == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
	}
	result.Finish()
	return result
}

func (db *RedisDB) BZMPop(timeoutSeconds int, numKeys int, keys []string, where string) (string, *PooledBuffer, float64, bool) {
	for t := 0; t < timeoutSeconds*10; t++ {
		for _, key := range keys {
			var member *PooledBuffer
			var score float64
			var found bool

			db.mu.Lock()
			keyBytes := []byte(key)
			headOff, ok := db.dict.Get(keyBytes)
			if ok {
				enc := db.ObjectEncoding(headOff)
				dataOff := db.ObjectDataOffset(headOff)

				if enc == ObjEncodingSkiplist {
					zsl := zslLoadFromArena(db.arena, dataOff)
					if zsl.length > 0 {
						var nodeOff int
						if where == "MIN" {
							nodeOff = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
						} else {
							nodeOff = zsl.tailOff
						}
						if nodeOff != 0 {
							memberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, nodeOff)))
							m := db.arena.ReadBytes(memberOff, db.arena.SizeAt(memberOff))
							score = db.arena.ReadFloat64(zslNodeScoreOff(db.arena, nodeOff))
							member = NewBuf(len(m))
							member.buf.Write(m)

							zslDelete(db.arena, &zsl, memberOff, score)
							zslSaveToArena(db.arena, dataOff, &zsl)
							if zsl.length == 0 {
								db.dict.Del(keyBytes)
								db.FreeObject(headOff)
							} else {
								db.ObjectSetDataOffset(headOff, dataOff)
							}
							found = true
						}
					}
				}
			}
			db.mu.Unlock()

			if found {
				return key, member, score, true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", nil, 0, false
}

func (db *RedisDB) ZPopMax(key string, count int) *ZSlices {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	result := NewZSlices()
	if enc == ObjEncodingSkiplist {
		zsl := zslLoadFromArena(db.arena, dataOff)

		type nodeInfo struct {
			off   int
			score float64
		}
		nodes := make([]nodeInfo, 0, zsl.length)
		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			nodes = append(nodes, nodeInfo{off: x, score: score})
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}

		sortByScoreDesc := make([]nodeInfo, len(nodes))
		copy(sortByScoreDesc, nodes)
		for i := 0; i < len(sortByScoreDesc)-1; i++ {
			for j := i + 1; j < len(sortByScoreDesc); j++ {
				if sortByScoreDesc[j].score > sortByScoreDesc[i].score {
					sortByScoreDesc[i], sortByScoreDesc[j] = sortByScoreDesc[j], sortByScoreDesc[i]
				}
			}
		}

		popCount := count
		if popCount > len(sortByScoreDesc) {
			popCount = len(sortByScoreDesc)
		}
		for i := 0; i < popCount; i++ {
			nodeOff := sortByScoreDesc[i].off
			memberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, nodeOff)))
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, nodeOff))
			member := db.arena.ReadBytes(memberOff, db.arena.SizeAt(memberOff))
			result.Add(member)
			zslDelete(db.arena, &zsl, memberOff, score)
		}

		zslSaveToArena(db.arena, dataOff, &zsl)
		if zsl.length == 0 {
			db.dict.Del(keyBytes)
			db.FreeObject(headOff)
		}
	}
	result.Finish()
	return result
}
