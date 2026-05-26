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

import "math/rand"

const (
	zskiplistMaxLevel = 32
	zskiplistP        = 0.25
)

type ZSkipList struct {
	headerOff int
	tailOff   int
	length    int
	level     int
}

func zslNodeMemberOff(arena *Arena, nodeOff int) int {
	return nodeOff
}

func zslNodeScoreOff(arena *Arena, nodeOff int) int {
	return nodeOff + 4
}

func zslNodeBackwardOff(arena *Arena, nodeOff int) int {
	return nodeOff + 12
}

func zslNodeLevelCountOff(arena *Arena, nodeOff int) int {
	return nodeOff + 16
}

func zslNodeLevelsOff(arena *Arena, nodeOff int) int {
	return nodeOff + 18
}

func zslLevelForwardOff(arena *Arena, nodeOff int, level int) int {
	return zslNodeLevelsOff(arena, nodeOff) + level*8
}

func zslLevelSpanOff(arena *Arena, nodeOff int, level int) int {
	return zslNodeLevelsOff(arena, nodeOff) + level*8 + 4
}

func zslNodeSize(levelCount int) int {
	return 18 + levelCount*8
}

func zslCreate(arena *Arena) *ZSkipList {
	headerSize := zslNodeSize(zskiplistMaxLevel)
	headerOff := arena.Alloc(headerSize)

	arena.WriteUint32(zslNodeMemberOff(arena, headerOff), 0)
	arena.WriteFloat64(zslNodeScoreOff(arena, headerOff), 0)
	arena.WriteUint32(zslNodeBackwardOff(arena, headerOff), 0)
	arena.WriteUint16(zslNodeLevelCountOff(arena, headerOff), uint16(zskiplistMaxLevel))

	for i := 0; i < zskiplistMaxLevel; i++ {
		arena.WriteUint32(zslLevelForwardOff(arena, headerOff, i), 0)
		arena.WriteUint32(zslLevelSpanOff(arena, headerOff, i), 0)
	}

	return &ZSkipList{
		headerOff: headerOff,
		tailOff:   0,
		length:    0,
		level:     1,
	}
}

func zslRandomLevel() int {
	level := 1
	for rand.Float64() < zskiplistP && level < zskiplistMaxLevel {
		level++
	}
	return level
}

func zslInsert(arena *Arena, zsl *ZSkipList, memberOff int, score float64) int {
	update := make([]int, zskiplistMaxLevel)
	rank := make([]int, zskiplistMaxLevel)

	x := zsl.headerOff
	for i := zsl.level - 1; i >= 0; i-- {
		if i == zsl.level-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}
		for {
			forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, x, i)))
			if forwardOff == 0 {
				break
			}
			fScore := arena.ReadFloat64(zslNodeScoreOff(arena, forwardOff))
			if fScore > score {
				break
			}
			if fScore == score {
				fMemberOff := int(arena.ReadUint32(zslNodeMemberOff(arena, forwardOff)))
				fMember := arena.ReadBytes(fMemberOff, arena.SizeAt(fMemberOff))
				member := arena.ReadBytes(memberOff, arena.SizeAt(memberOff))
				if bytesEqual(fMember, member) {
					arena.Free(memberOff)
					return 0
				}
				if string(fMember) > string(member) {
					break
				}
			}
			rank[i] += int(arena.ReadUint32(zslLevelSpanOff(arena, x, i)))
			x = forwardOff
		}
		update[i] = x
	}

	level := zslRandomLevel()
	if level > zsl.level {
		for i := zsl.level; i < level; i++ {
			rank[i] = 0
			update[i] = zsl.headerOff
			arena.WriteUint32(zslLevelSpanOff(arena, update[i], i), uint32(zsl.length))
		}
		zsl.level = level
	}

	nodeSize := zslNodeSize(level)
	nodeOff := arena.Alloc(nodeSize)
	arena.WriteUint32(zslNodeMemberOff(arena, nodeOff), uint32(memberOff))
	arena.WriteFloat64(zslNodeScoreOff(arena, nodeOff), score)
	arena.WriteUint16(zslNodeLevelCountOff(arena, nodeOff), uint16(level))

	x = update[0]
	forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, x, 0)))
	if forwardOff == 0 {
		arena.WriteUint32(zslNodeBackwardOff(arena, nodeOff), 0)
		zsl.tailOff = nodeOff
	} else {
		arena.WriteUint32(zslNodeBackwardOff(arena, nodeOff), uint32(x))
	}
	arena.WriteUint32(zslNodeBackwardOff(arena, forwardOff), uint32(nodeOff))

	for i := 0; i < level; i++ {
		prevForward := int(arena.ReadUint32(zslLevelForwardOff(arena, update[i], i)))
		arena.WriteUint32(zslLevelForwardOff(arena, nodeOff, i), uint32(prevForward))
		arena.WriteUint32(zslLevelForwardOff(arena, update[i], i), uint32(nodeOff))

		span := arena.ReadUint32(zslLevelSpanOff(arena, update[i], i))
		arena.WriteUint32(zslLevelSpanOff(arena, nodeOff, i), span-(uint32(rank[0])-uint32(rank[i])))
		arena.WriteUint32(zslLevelSpanOff(arena, update[i], i), uint32(rank[0])-uint32(rank[i])+1)
	}

	for i := level; i < zsl.level; i++ {
		span := arena.ReadUint32(zslLevelSpanOff(arena, update[i], i))
		arena.WriteUint32(zslLevelSpanOff(arena, update[i], i), span+1)
	}

	zsl.length++
	return nodeOff
}

func zslDelete(arena *Arena, zsl *ZSkipList, memberOff int, score float64) bool {
	update := make([]int, zskiplistMaxLevel)

	x := zsl.headerOff
	for i := zsl.level - 1; i >= 0; i-- {
		for {
			forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, x, i)))
			if forwardOff == 0 {
				break
			}
			fScore := arena.ReadFloat64(zslNodeScoreOff(arena, forwardOff))
			if fScore > score {
				break
			}
			if fScore == score {
				fMemberOff := int(arena.ReadUint32(zslNodeMemberOff(arena, forwardOff)))
				fMember := arena.ReadBytes(fMemberOff, arena.SizeAt(fMemberOff))
				member := arena.ReadBytes(memberOff, arena.SizeAt(memberOff))
				if bytesEqual(fMember, member) {
					break
				}
				if string(fMember) > string(member) {
					break
				}
			}
			x = forwardOff
		}
		update[i] = x
	}

	x = int(arena.ReadUint32(zslLevelForwardOff(arena, update[0], 0)))
	if x == 0 {
		return false
	}
	xScore := arena.ReadFloat64(zslNodeScoreOff(arena, x))
	xMemberOff := int(arena.ReadUint32(zslNodeMemberOff(arena, x)))
	xMember := arena.ReadBytes(xMemberOff, arena.SizeAt(xMemberOff))
	member := arena.ReadBytes(memberOff, arena.SizeAt(memberOff))
	if xScore != score || !bytesEqual(xMember, member) {
		return false
	}

	zslDeleteNode(arena, zsl, x, update)
	return true
}

func zslDeleteNode(arena *Arena, zsl *ZSkipList, nodeOff int, update []int) {
	level := int(arena.ReadUint16(zslNodeLevelCountOff(arena, nodeOff)))
	for i := 0; i < level; i++ {
		forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, nodeOff, i)))
		span := arena.ReadUint32(zslLevelSpanOff(arena, nodeOff, i))
		updateSpan := arena.ReadUint32(zslLevelSpanOff(arena, update[i], i))
		arena.WriteUint32(zslLevelForwardOff(arena, update[i], i), uint32(forwardOff))
		arena.WriteUint32(zslLevelSpanOff(arena, update[i], i), updateSpan+span-1)
	}

	for i := level; i < zsl.level; i++ {
		span := arena.ReadUint32(zslLevelSpanOff(arena, update[i], i))
		if span > 0 {
			arena.WriteUint32(zslLevelSpanOff(arena, update[i], i), span-1)
		}
	}

	backwardOff := int(arena.ReadUint32(zslNodeBackwardOff(arena, nodeOff)))
	forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, nodeOff, 0)))
	if forwardOff != 0 {
		arena.WriteUint32(zslNodeBackwardOff(arena, forwardOff), uint32(backwardOff))
	} else {
		zsl.tailOff = backwardOff
	}

	for zsl.level > 1 {
		if arena.ReadUint32(zslLevelForwardOff(arena, zsl.headerOff, zsl.level-1)) != 0 {
			break
		}
		zsl.level--
	}

	zsl.length--
	arena.Free(nodeOff)
}

func zslGetRank(arena *Arena, zsl *ZSkipList, memberOff int, score float64) int {
	rank := 0
	x := zsl.headerOff
	for i := zsl.level - 1; i >= 0; i-- {
		for {
			forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, x, i)))
			if forwardOff == 0 {
				break
			}
			fScore := arena.ReadFloat64(zslNodeScoreOff(arena, forwardOff))
			if fScore > score {
				break
			}
			if fScore == score {
				fMemberOff := int(arena.ReadUint32(zslNodeMemberOff(arena, forwardOff)))
				fMember := arena.ReadBytes(fMemberOff, arena.SizeAt(fMemberOff))
				member := arena.ReadBytes(memberOff, arena.SizeAt(memberOff))
				if bytesEqual(fMember, member) {
					return rank + int(arena.ReadUint32(zslLevelSpanOff(arena, x, i)))
				}
				if string(fMember) > string(member) {
					break
				}
			}
			rank += int(arena.ReadUint32(zslLevelSpanOff(arena, x, i)))
			x = forwardOff
		}
	}
	return 0
}

func zslGetElementByRank(arena *Arena, zsl *ZSkipList, rank int) int {
	traversed := 0
	x := zsl.headerOff
	for i := zsl.level - 1; i >= 0; i-- {
		for {
			forwardOff := int(arena.ReadUint32(zslLevelForwardOff(arena, x, i)))
			if forwardOff == 0 {
				break
			}
			span := int(arena.ReadUint32(zslLevelSpanOff(arena, x, i)))
			if traversed+span > rank {
				break
			}
			traversed += span
			x = forwardOff
		}
		if traversed == rank {
			return x
		}
	}
	return 0
}
