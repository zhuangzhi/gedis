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

func BenchmarkArenaAlloc(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Alloc(64)
	}
}

func BenchmarkArenaAllocFree(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	offs := make([]int, b.N)
	for i := 0; i < b.N; i++ {
		offs[i] = a.Alloc(64)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Free(offs[i])
		a.Alloc(64)
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
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = []byte(fmt.Sprintf("key%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Set(keys[i], i)
	}
}

func BenchmarkDictGet(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	d := NewDict(a)
	for i := 0; i < 10000; i++ {
		d.Set([]byte(fmt.Sprintf("key%d", i)), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 10000
		d.Get([]byte(fmt.Sprintf("key%d", idx)))
	}
}

func BenchmarkDictDel(b *testing.B) {
	a := NewArena(16 * 4096 * 1024)
	d := NewDict(a)
	for i := 0; i < b.N; i++ {
		d.Set([]byte(fmt.Sprintf("key%d", i)), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Del([]byte(fmt.Sprintf("key%d", i)))
	}
}

func BenchmarkRedisSet(b *testing.B) {
	val := []byte("hello world value for benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.Set("key", val)
	}
}

func BenchmarkRedisGet(b *testing.B) {
	db := New()
	val := []byte("hello world value for benchmark")
	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key%d", i), val)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Get(fmt.Sprintf("key%d", i%10000))
	}
}

func BenchmarkRedisDel(b *testing.B) {
	val := []byte("hello world value for benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		db.Set("key", val)
		b.StartTimer()
		db.Del("key")
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
	val := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.LPush("mylist", val)
	}
}

func BenchmarkRedisLPop(b *testing.B) {
	val := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		for j := 0; j < 100; j++ {
			db.LPush("mylist", val)
		}
		b.StartTimer()
		db.LPop("mylist")
	}
}

func BenchmarkRedisRPush(b *testing.B) {
	val := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.RPush("mylist", val)
	}
}

func BenchmarkRedisRPop(b *testing.B) {
	val := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		for j := 0; j < 100; j++ {
			db.RPush("mylist", val)
		}
		b.StartTimer()
		db.RPop("mylist")
	}
}

func BenchmarkRedisHSet(b *testing.B) {
	val := []byte("value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.HSet("hash", "field", val)
	}
}

func BenchmarkRedisHGet(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.HSet("hash", fmt.Sprintf("field%d", i), []byte("value"))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HGet("hash", fmt.Sprintf("field%d", i%10000))
	}
}

func BenchmarkRedisHDel(b *testing.B) {
	val := []byte("value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		db.HSet("hash", "field", val)
		b.StartTimer()
		db.HDel("hash", "field")
	}
}

func BenchmarkRedisHIncrBy(b *testing.B) {
	db := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.HIncrBy("hash", "counter", 1)
	}
}

func BenchmarkRedisSAdd(b *testing.B) {
	val := []byte("member")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.SAdd("set", val)
	}
}

func BenchmarkRedisSIsMember(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SIsMember("set", []byte(fmt.Sprintf("member%d", i%10000)))
	}
}

func BenchmarkRedisSRem(b *testing.B) {
	val := []byte("member")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		db.SAdd("set", val)
		b.StartTimer()
		db.SRem("set", val)
	}
}

func BenchmarkRedisZAdd(b *testing.B) {
	val := []byte("member")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.ZAdd("zset", 1.0, val)
	}
}

func BenchmarkRedisZScore(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZScore("zset", []byte(fmt.Sprintf("member%d", i%10000)))
	}
}

func BenchmarkRedisZRem(b *testing.B) {
	val := []byte("member")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		db.ZAdd("zset", 1.0, val)
		b.StartTimer()
		db.ZRem("zset", val)
	}
}

func BenchmarkRedisZRange(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ZRange("zset", 0, 99)
	}
}

func BenchmarkRedisPFAdd(b *testing.B) {
	val := []byte("item")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		b.StartTimer()
		db.SetBit("bitmap", i%10000, 1)
	}
}

func BenchmarkRedisGetBit(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.SetBit("bitmap", i, 1)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.GetBit("bitmap", i%10000)
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := New()
		db.BFReserve("bf", 0.01, 1000000)
		b.StartTimer()
		db.BFAdd("bf", []byte("item"))
	}
}

func BenchmarkRedisBFExists(b *testing.B) {
	db := New()
	db.BFReserve("bf", 0.01, 1000000)
	for i := 0; i < 10000; i++ {
		db.BFAdd("bf", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.BFExists("bf", []byte(fmt.Sprintf("item%d", i%10000)))
	}
}

func BenchmarkRedisCFAdd(b *testing.B) {
	db := New()
	db.CFReserve("cf", 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("item%d", i)))
	}
}

func BenchmarkRedisCFExists(b *testing.B) {
	db := New()
	db.CFReserve("cf", 4096)
	for i := 0; i < 10000; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CFExists("cf", []byte(fmt.Sprintf("item%d", i%10000)))
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
	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte("value"))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Exists(fmt.Sprintf("key%d", i%10000))
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
	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key%d", i), val)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.Get(fmt.Sprintf("key%d", i%10000))
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
			db.Set(fmt.Sprintf("key%d", i), val)
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
