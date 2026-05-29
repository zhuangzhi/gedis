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

// Dict 哈希表。使用 FNV-1a 哈希 + 线性探测，所有数据存储在 Arena 中。
// 负载因子超过 75% 时自动 rehash（扩为 2 倍）。
//
// 每个槽位 8 字节:
//
//	[ keyOff (4B) | valOff (4B) ]
const (
	dictSlotSize   = 8  // 每个槽位的字节数
	dictInitSize   = 16 // 初始槽位数
	dictLoadFactor = 75 // 负载因子（百分比）
)

// Dict 使用 Arena 存储的哈希表
type Dict struct {
	arena *Arena // 关联的 Arena
	table int    // 槽位数组在 Arena 中的偏移
	size  int    // 槽位总数
	used  int    // 已使用槽位数
}

// NewDict 在给定 Arena 中创建一个新的 Dict。
func NewDict(arena *Arena) *Dict {
	tableOff := arena.Alloc(dictInitSize * dictSlotSize)
	d := &Dict{
		arena: arena,
		table: tableOff,
		size:  dictInitSize,
		used:  0,
	}
	for i := 0; i < dictInitSize; i++ {
		d.setSlot(i, 0, 0)
	}
	return d
}

// StoreMeta 将 Dict 元数据序列化到 Arena 指定偏移。
// 格式: [table(4B) | size(4B) | used(4B)]
func (d *Dict) StoreMeta(off int) {
	d.arena.WriteUint32(off, uint32(d.table))
	d.arena.WriteUint32(off+4, uint32(d.size))
	d.arena.WriteUint32(off+8, uint32(d.used))
}

// LoadDictMeta 从 Arena 中加载已持久化的 Dict 元数据。
func LoadDictMeta(arena *Arena, off int) *Dict {
	table := int(arena.ReadUint32(off))
	size := int(arena.ReadUint32(off + 4))
	used := int(arena.ReadUint32(off + 8))
	return &Dict{
		arena: arena,
		table: table,
		size:  size,
		used:  used,
	}
}

// 槽位访问器：每个槽位 = keyOff(4B) + valOff(4B)
func (d *Dict) slotKeyOff(idx int) int { return d.table + idx*dictSlotSize }
func (d *Dict) slotValOff(idx int) int { return d.table + idx*dictSlotSize + 4 }

func (d *Dict) setSlot(idx int, keyOff, valOff int) {
	d.arena.WriteUint32(d.slotKeyOff(idx), uint32(keyOff))
	d.arena.WriteUint32(d.slotValOff(idx), uint32(valOff))
}

func (d *Dict) getSlot(idx int) (keyOff, valOff int) {
	keyOff = int(d.arena.ReadUint32(d.slotKeyOff(idx)))
	valOff = int(d.arena.ReadUint32(d.slotValOff(idx)))
	return
}

// hashKey 对 key 进行 FNV-1a 哈希。
func (d *Dict) hashKey(key []byte) uint32 {
	return fnv32(key)
}

// keyEquals 判断槽位 key 是否与给定 key 相等（逐字节比较）。
func (d *Dict) keyEquals(keyOff int, key []byte) bool {
	if keyOff == 0 {
		return false
	}
	size := d.arena.SizeAt(keyOff)
	if size != len(key) {
		return false
	}
	stored := d.arena.GetSlice(keyOff, size)
	for i := 0; i < size; i++ {
		if stored[i] != key[i] {
			return false
		}
	}
	return true
}

// Set 插入或更新键值对。若 key 已存在则更新 valOff。
func (d *Dict) Set(key []byte, valOff int) {
	if d.used*100 >= d.size*dictLoadFactor {
		d.rehash()
	}

	h := d.hashKey(key)
	idx := int(h % uint32(d.size))

	for {
		kOff, _ := d.getSlot(idx)
		if kOff == 0 || d.keyEquals(kOff, key) {
			if kOff == 0 {
				keyArenaOff := d.arena.AllocBytes(key)
				d.setSlot(idx, keyArenaOff, valOff)
				d.used++
			} else {
				d.setSlot(idx, kOff, valOff)
			}
			return
		}
		idx = (idx + 1) % d.size
	}
}

// Get 根据 key 查找对应的 valOff。返回值 ok 指示是否找到。
func (d *Dict) Get(key []byte) (valOff int, ok bool) {
	h := d.hashKey(key)
	idx := int(h % uint32(d.size))

	for {
		kOff, vOff := d.getSlot(idx)
		if kOff == 0 {
			return 0, false
		}
		if d.keyEquals(kOff, key) {
			return vOff, true
		}
		idx = (idx + 1) % d.size
	}
}

// Del 删除指定 key。返回是否成功删除。
func (d *Dict) Del(key []byte) bool {
	h := d.hashKey(key)
	idx := int(h % uint32(d.size))

	for {
		kOff, _ := d.getSlot(idx)
		if kOff == 0 {
			return false
		}
		if d.keyEquals(kOff, key) {
			d.setSlot(idx, 0, 0)
			d.used--
			return true
		}
		idx = (idx + 1) % d.size
	}
}

// rehash 将哈希表扩容为 2 倍，重新哈希所有 key。
func (d *Dict) rehash() {
	newSize := d.size * 2
	newTableOff := d.arena.Alloc(newSize * dictSlotSize)

	oldTable := d.table
	oldSize := d.size

	d.table = newTableOff
	d.size = newSize
	d.used = 0

	for i := 0; i < newSize; i++ {
		d.arena.WriteUint32(newTableOff+i*dictSlotSize, 0)
		d.arena.WriteUint32(newTableOff+i*dictSlotSize+4, 0)
	}

	for i := 0; i < oldSize; i++ {
		kOff := int(d.arena.ReadUint32(oldTable + i*dictSlotSize))
		if kOff == 0 {
			continue
		}
		vOff := int(d.arena.ReadUint32(oldTable + i*dictSlotSize + 4))
		key := d.arena.ReadBytes(kOff, d.arena.SizeAt(kOff))
		d.Set(key, vOff)
	}
}

// fnv32 FNV-1a 哈希函数。
func fnv32(data []byte) uint32 {
	var h uint32 = 2166136261
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return h
}

// fnv32U16 零分配版 FNV-1a 哈希 for uint16。
func fnv32U16(v uint16) uint32 {
	h := uint32(2166136261)
	h ^= uint32(byte(v))
	h *= 16777619
	h ^= uint32(byte(v >> 8))
	h *= 16777619
	return h
}

// fnv32Byte 零分配版 FNV-1a 哈希 for byte。
func fnv32Byte(b byte) uint32 {
	h := uint32(2166136261)
	h ^= uint32(b)
	h *= 16777619
	return h
}

func (d *Dict) Len() int {
	return d.used
}

func NewDictFromArena(arena *Arena, used int) *Dict {
	return &Dict{
		arena: arena,
		table: arena.Alloc(dictInitSize * dictSlotSize),
		size:  dictInitSize,
		used:  used,
	}
}
