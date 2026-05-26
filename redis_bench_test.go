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
	"fmt"
	"testing"
)

const benchKeyPool = 100

func BenchmarkArenaAlloc(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Alloc(64)
	}
}

func BenchmarkArenaAllocFree(b *testing.B) {
	a := NewArena(4096)
	off := a.Alloc(64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Free(off)
		off = a.Alloc(64)
	}
}

func BenchmarkArenaReadWrite(b *testing.B) {
	a := NewArena(4096 * 1024)
	off := a.Alloc(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.WriteUint32(off, uint32(i))
		_ = a.ReadUint32(off)
	}
}

func BenchmarkDictSet(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	d := NewDict(a)
	keys := make([][]byte, benchKeyPool)
	for i := 0; i < benchKeyPool; i++ {
		keys[i] = []byte(fmt.Sprintf("k%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Set(keys[i%benchKeyPool], i)
	}
}

func BenchmarkDictGet(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	d := NewDict(a)
	for i := 0; i < benchKeyPool; i++ {
		d.Set([]byte(fmt.Sprintf("k%d", i)), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Get([]byte(fmt.Sprintf("k%d", i%benchKeyPool)))
	}
}

func BenchmarkDictDel(b *testing.B) {
	a := NewArena(4096)
	d := NewDict(a)
	d.Set([]byte("key"), 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Del([]byte("key"))
		d.Set([]byte("key"), 100)
	}
}

func BenchmarkRedisSet(b *testing.B) {
	db := New()
	val := []byte("hello world value for benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Set("key", val)
	}
}

func BenchmarkRedisGet(b *testing.B) {
	db := New()
	val := []byte("hello world value for benchmark")
	for i := 0; i < benchKeyPool; i++ {
		db.Set(fmt.Sprintf("key%d", i), val)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Get(fmt.Sprintf("key%d", i%benchKeyPool))
	}
}

func BenchmarkRedisDel(b *testing.B) {
	db := New()
	val := []byte("hello world value for benchmark")
	db.Set("key", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Del("key")
		db.Set("key", val)
	}
}

func BenchmarkRedisIncrBy(b *testing.B) {
	db := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.IncrBy("counter", 1)
	}
}

func BenchmarkRedisLPush(b *testing.B) {
	db := New()
	val := []byte("hello")
	db.LPush("mylist", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.LPush("mylist", val)
		db.RPop("mylist")
	}
}

func BenchmarkRedisLPop(b *testing.B) {
	db := New()
	val := []byte("hello")
	db.LPush("mylist", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.LPop("mylist")
		db.LPush("mylist", val)
	}
}

func BenchmarkRedisRPush(b *testing.B) {
	db := New()
	val := []byte("hello")
	db.RPush("mylist", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.RPush("mylist", val)
		db.LPop("mylist")
	}
}

func BenchmarkRedisRPop(b *testing.B) {
	db := New()
	val := []byte("hello")
	db.RPush("mylist", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.RPop("mylist")
		db.RPush("mylist", val)
	}
}

func BenchmarkRedisHSet(b *testing.B) {
	db := New()
	val := []byte("value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HSet("hash", "field", val)
	}
}

func BenchmarkRedisHDel(b *testing.B) {
	db := New()
	val := []byte("value")
	db.HSet("hash", "field", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HDel("hash", "field")
		db.HSet("hash", "field", val)
	}
}
func BenchmarkRedisHGet(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.HSet("hash", fmt.Sprintf("field%d", i), []byte("value"))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HGet("hash", fmt.Sprintf("field%d", i%benchKeyPool))
	}
}

func BenchmarkRedisHIncrBy(b *testing.B) {
	db := New()
	db.HSet("hash", "counter", []byte("0"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HIncrBy("hash", "counter", 1)
		db.HIncrBy("hash", "counter", -1)
	}
}

func BenchmarkRedisSAdd(b *testing.B) {
	db := New()
	val := []byte("member")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SAdd("set", val)
	}
}

func BenchmarkRedisSRem(b *testing.B) {
	db := New()
	val := []byte("member")
	db.SAdd("set", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SRem("set", val)
		db.SAdd("set", val)
	}
}

func BenchmarkRedisSIsMember(b *testing.B) {
	db := New()
	val := []byte("member")
	db.SAdd("set", val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SIsMember("set", val)
	}
}

func BenchmarkRedisZAdd(b *testing.B) {
	db := New()
	val := []byte("member")
	db.ZAdd("zset", 1.0, val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZRem("zset", val)
		db.ZAdd("zset", 1.0, val)
	}
}

func BenchmarkRedisZScore(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZScore("zset", []byte(fmt.Sprintf("member%d", i%benchKeyPool)))
	}
}

func BenchmarkRedisZRem(b *testing.B) {
	db := New()
	val := []byte("member")
	db.ZAdd("zset", 1.0, val)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZRem("zset", val)
		db.ZAdd("zset", 1.0, val)
	}
}

func BenchmarkRedisZRange(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZRange("zset", 0, 99)
	}
}

func BenchmarkRedisZRangeIter(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZRangeIter("zset", 0, 99, func(member []byte) {
			_ = member
		})
	}
}

func BenchmarkRedisPFAdd(b *testing.B) {
	db := New()
	val := []byte("item")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.PFAdd("hll", val)
	}
}

func BenchmarkRedisPFCount(b *testing.B) {
	db := New()
	for i := 0; i < 100000; i++ {
		db.PFAdd("hll", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.PFCount("hll")
	}
}

func BenchmarkRedisSetBit(b *testing.B) {
	db := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SetBit("bitmap", 0, 1)
	}
}

func BenchmarkRedisGetBit(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.SetBit("bitmap", i, 1)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.GetBit("bitmap", i%benchKeyPool)
	}
}

func BenchmarkRedisBitCount(b *testing.B) {
	db := New()
	for i := 0; i < 100000; i++ {
		db.SetBit("bitmap", i, 1)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.BitCount("bitmap", 0, -1)
	}
}

func BenchmarkRedisBFAdd(b *testing.B) {
	db := New()
	db.BFReserve("bf", 0.01, 1000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.BFAdd("bf", []byte("item"))
	}
}

func BenchmarkRedisBFExists(b *testing.B) {
	db := New()
	db.BFReserve("bf", 0.01, 1000000)
	for i := 0; i < benchKeyPool; i++ {
		db.BFAdd("bf", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.BFExists("bf", []byte(fmt.Sprintf("item%d", i%benchKeyPool)))
	}
}

func BenchmarkRedisCFAdd(b *testing.B) {
	db := New()
	db.CFReserve("cf", 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CFAdd("cf", []byte("item"))
	}
}

func BenchmarkRedisCFExists(b *testing.B) {
	db := New()
	db.CFReserve("cf", 4096)
	for i := 0; i < benchKeyPool; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CFExists("cf", []byte(fmt.Sprintf("item%d", i%benchKeyPool)))
	}
}

func BenchmarkRedisCMSIncr(b *testing.B) {
	db := New()
	db.CMSInitByDim("cms", 2000, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CMSIncrBy("cms", []byte(fmt.Sprintf("item%d", i%1000)), 1)
	}
}

func BenchmarkRedisTopKAdd(b *testing.B) {
	db := New()
	db.TopKReserve("topk", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.TopKAdd("topk", fmt.Sprintf("item%d", i%200))
	}
}

func BenchmarkRedisExists(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte("value"))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Exists(fmt.Sprintf("key%d", i%benchKeyPool))
	}
}

func BenchmarkRedisMixedRead(b *testing.B) {
	db := New()
	val := []byte("value")
	for i := 0; i < 1000; i++ {
		db.Set(fmt.Sprintf("key%d", i), val)
		db.LPush(fmt.Sprintf("list%d", i), val)
		db.HSet(fmt.Sprintf("hash%d", i), "f", val)
		db.SAdd(fmt.Sprintf("set%d", i), []byte(fmt.Sprintf("m%d", i)))
		db.ZAdd(fmt.Sprintf("zset%d", i), float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % 1000
		db.Get(fmt.Sprintf("key%d", j))
		db.LLen(fmt.Sprintf("list%d", j))
		db.HGet(fmt.Sprintf("hash%d", j), "f")
		db.SIsMember(fmt.Sprintf("set%d", j), []byte(fmt.Sprintf("m%d", j)))
		db.ZScore(fmt.Sprintf("zset%d", j), []byte(fmt.Sprintf("m%d", j)))
	}
}

func BenchmarkRedisConcurrentRead(b *testing.B) {
	db := New()
	val := []byte("value")
	for i := 0; i < benchKeyPool; i++ {
		db.Set(fmt.Sprintf("key%d", i), val)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.Get(fmt.Sprintf("key%d", i%benchKeyPool))
			i++
		}
	})
}

func BenchmarkRedisConcurrentWrite(b *testing.B) {
	db := New()
	val := []byte("value")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.Set(fmt.Sprintf("key%d", i%benchKeyPool), val)
			i++
		}
	})
}

func BenchmarkRedisConcurrentIncrBy(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			db.IncrBy("counter", 1)
		}
	})
}
