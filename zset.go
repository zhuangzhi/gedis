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

func (db *RedisDB) ZAdd(key string, score float64, member []byte) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)

	if !ok {
		zsl := zslCreate(db.arena)
		memberOff := db.arena.AllocBytes(member)
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
		memberOff := db.arena.AllocBytes(member)
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

func (db *RedisDB) ZRem(key string, member []byte) bool {
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
			if bytesEqual(xMember, member) {
				score = xScore
				found = true
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}

		if !found {
			return false
		}

		memberOff := db.arena.AllocBytes(member)
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

func (db *RedisDB) ZScore(key string, member []byte) (float64, bool) {
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
			if bytesEqual(xMember, member) {
				return xScore, true
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
	}

	return 0, false
}

type ZSlices struct {
	offsets []int
	members []byte
}

func (s *ZSlices) Len() int {
	return len(s.offsets)
}

func (s *ZSlices) Add(data []byte) {
	s.offsets = append(s.offsets, len(s.members))
	s.members = append(s.members, data...)
}

func (s *ZSlices) Get(i int) []byte {
	from := s.offsets[i]
	if i+1 < len(s.offsets) {
		return s.members[from:s.offsets[i+1]]
	}
	return s.members[from:]
}

func (db *RedisDB) ZRange(key string, start, stop int) ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return ZSlices{}
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
			return ZSlices{}
		}

		count := stop - start + 1
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

		result := ZSlices{
			offsets: make([]int, count),
			members: make([]byte, totalBytes),
		}
		mPos := 0
		x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx = 0
		out := 0
		for x != 0 {
			if idx >= start {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				sz := db.arena.SizeAt(xMemberOff)
				result.offsets[out] = mPos
				copy(result.members[mPos:], db.arena.GetSlice(xMemberOff, sz))
				mPos += sz
				out++
			}
			if idx >= stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}
		return result
	}

	return ZSlices{}
}

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

func (db *RedisDB) ZRangeWithScores(key string, start, stop int) ([]string, []float64) {
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

		members := make([]string, 0, stop-start+1)
		scores := make([]float64, 0, stop-start+1)

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		idx := 0
		for x != 0 {
			if idx >= start && idx <= stop {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				member := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
				score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
				members = append(members, string(member))
				scores = append(scores, score)
			}
			if idx > stop {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
			idx++
		}
		return members, scores
	}

	return nil, nil
}

func (db *RedisDB) ZRangeByScore(key string, min, max float64) [][]byte {
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
		var result [][]byte

		x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
		for x != 0 {
			score := db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x))
			if score >= min && score <= max {
				xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
				member := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))
				result = append(result, member)
			}
			if score > max {
				break
			}
			x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
		}
		return result
	}

	return nil
}

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

	return 0
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
