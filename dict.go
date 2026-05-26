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

const (
	dictSlotSize   = 8
	dictInitSize   = 16
	dictLoadFactor = 75
)

type Dict struct {
	arena *Arena
	table int
	size  int
	used  int
}

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

func (d *Dict) StoreMeta(off int) {
	d.arena.WriteUint32(off, uint32(d.table))
	d.arena.WriteUint32(off+4, uint32(d.size))
	d.arena.WriteUint32(off+8, uint32(d.used))
}

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

func (d *Dict) slotKeyOff(idx int) int {
	return d.table + idx*dictSlotSize
}

func (d *Dict) slotValOff(idx int) int {
	return d.table + idx*dictSlotSize + 4
}

func (d *Dict) setSlot(idx int, keyOff, valOff int) {
	d.arena.WriteUint32(d.slotKeyOff(idx), uint32(keyOff))
	d.arena.WriteUint32(d.slotValOff(idx), uint32(valOff))
}

func (d *Dict) getSlot(idx int) (keyOff, valOff int) {
	keyOff = int(d.arena.ReadUint32(d.slotKeyOff(idx)))
	valOff = int(d.arena.ReadUint32(d.slotValOff(idx)))
	return
}

func (d *Dict) hashKey(key []byte) uint32 {
	return fnv32(key)
}

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

func fnv32(data []byte) uint32 {
	var h uint32 = 2166136261
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return h
}

func fnv32U16(v uint16) uint32 {
	h := uint32(2166136261)
	h ^= uint32(byte(v))
	h *= 16777619
	h ^= uint32(byte(v >> 8))
	h *= 16777619
	return h
}

func fnv32Byte(b byte) uint32 {
	h := uint32(2166136261)
	h ^= uint32(b)
	h *= 16777619
	return h
}
