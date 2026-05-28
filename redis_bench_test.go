package gedis

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

const benchKeyPool = 10000

func BenchmarkSet(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}
}

func BenchmarkGet(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb, _ := db.Get(fmt.Sprintf("key%d", i%benchKeyPool))
		if pb != nil {
			pb.Close()
		}
	}
}

func BenchmarkSetGet(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		db.Set(key, []byte("value"))
		pb, _ := db.Get(key)
		if pb != nil {
			pb.Close()
		}
	}
}

func BenchmarkSetGetThreaded(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key%d", i)
			db.Set(key, []byte("value"))
			v, _ := db.Get(key)
			if v != nil {
				v.Close()
			}
			i++
		}
	})
}

func BenchmarkLPushLPop(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.LPush("list", []byte("hello"))
		v, _ := db.LPop("list")
		if v != nil {
			v.Close()
		}
	}
}

func BenchmarkRPushRPop(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.RPush("list", []byte("hello"))
		v, _ := db.RPop("list")
		if v != nil {
			v.Close()
		}
	}
}

func BenchmarkHSetHGet(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HSet("hash", "field", []byte("value"))
		v, _ := db.HGet("hash", "field")
		if v != nil {
			v.Close()
		}
	}
}

func BenchmarkSAddSIsMember(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SAdd("set", []byte("member"))
		db.SIsMember("set", []byte("member"))
	}
}

func BenchmarkZAddZScore(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZAdd("zset", float64(i), []byte("member"))
		db.ZScore("zset", []byte("member"))
	}
}

func BenchmarkZAddZRange(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZRangeIter("zset", 0, 99, func(member []byte) {
			_ = member
		})
	}
}

func BenchmarkBFAddBFExists(b *testing.B) {
	db := New()
	db.BFReserve("bf", 0.01, 100000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.BFAdd("bf", []byte("item"))
		db.BFExists("bf", []byte("item"))
	}
}

func BenchmarkPFAdd(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb := Buf(fmt.Sprintf("item%d", i))
		db.PFAddBuffer("hll", pb)
		pb.Close()
	}
}

func BenchmarkThreadedSetGet(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(gid)))
			for i := 0; i < b.N/10; i++ {
				key := fmt.Sprintf("key%d", r.Intn(benchKeyPool))
				db.Set(key, []byte("value"))
				v, _ := db.Get(key)
				if v != nil {
					v.Close()
				}
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkManySets(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000; j++ {
			key := fmt.Sprintf("key%d", j+i*1000)
			db.Set(key, []byte("x"))
		}
	}
}

func BenchmarkSetThenGet(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte("val"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.Set(fmt.Sprintf("key%d", i%benchKeyPool), []byte("new"))
	}
}

func BenchmarkGetSimple(b *testing.B) {
	db := New()
	db.Set("key", []byte("value"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb, _ := db.Get("key")
		if pb != nil {
			pb.Close()
		}
	}
}

func BenchmarkSetSimple(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.Set("key", []byte("value"))
	}
}

func BenchmarkAppend(b *testing.B) {
	db := New()
	db.Set("key", []byte("init"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.Append("key", []byte("x"))
	}
}

func BenchmarkSetRange(b *testing.B) {
	db := New()
	db.Set("key", []byte("hello world hello world"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SetRange("key", 6, []byte("WORLD"))
	}
}

func BenchmarkLIndex(b *testing.B) {
	db := New()
	db.Set("key", []byte("hello"))
	for i := 0; i < 100; i++ {
		db.RPush("list", []byte(fmt.Sprintf("item%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, _ := db.LIndex("list", 50)
		if v != nil {
			v.Close()
		}
	}
}

func BenchmarkLInsert(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.RPush("list", []byte("x"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.RPush("list", []byte("y"))
	}
}

func BenchmarkLRange(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.RPush("list", []byte("x"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := db.LRange("list", 0, 9)
		result.Close()
	}
}

func BenchmarkHSet(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HSet("hash", fmt.Sprintf("f%d", i), []byte("v"))
	}
}

func BenchmarkHDel(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HSet("hash", fmt.Sprintf("tmp%d", i), []byte("x"))
		db.HDel("hash", fmt.Sprintf("tmp%d", i))
	}
}

func BenchmarkHGetAll(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.HSet("hash", fmt.Sprintf("f%d", i), []byte("v"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		all := db.HGetAll("hash")
		all.Close()
	}
}

func BenchmarkHIncrBy(b *testing.B) {
	db := New()
	db.HSet("hash", "count", []byte("0"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HIncrBy("hash", "count", 1)
	}
}

func BenchmarkHExists(b *testing.B) {
	db := New()
	db.HSet("hash", "f1", []byte("v1"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HExists("hash", "f1")
	}
}

func BenchmarkHSetLarge(b *testing.B) {
	db := New()
	for i := 0; i < 1000; i++ {
		db.HSet("hash", fmt.Sprintf("f%d", i), []byte("large-value-data"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HSet("hash", fmt.Sprintf("f%d", i%1000), []byte("new-val"))
	}
}

func BenchmarkSAdd(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("m%d", i)))
	}
}

func BenchmarkSMembers(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		members := db.SMembers("set")
		members.Close()
	}
}

func BenchmarkSRem(b *testing.B) {
	db := New()
	for i := 0; i < 200; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("t%d", i)))
		db.SRem("set", []byte(fmt.Sprintf("t%d", i)))
	}
}

func BenchmarkSInter(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.SAdd("set1", []byte(fmt.Sprintf("m%d", i)))
		db.SAdd("set2", []byte(fmt.Sprintf("m%d", i+50)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := db.SInter("set1", "set2")
		result.Close()
	}
}

func BenchmarkSUnion(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.SAdd("set1", []byte(fmt.Sprintf("m%d", i)))
		db.SAdd("set2", []byte(fmt.Sprintf("m%d", i+50)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := db.SUnion("set1", "set2")
		result.Close()
	}
}

func BenchmarkZAdd(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
}

func BenchmarkZRem(b *testing.B) {
	db := New()
	db.ZAdd("zset", 9999.0, []byte(fmt.Sprintf("m%d", 9999)))
	for i := 0; i < b.N; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZRem("zset", []byte(fmt.Sprintf("m%d", i)))
	}
}

func BenchmarkZCard(b *testing.B) {
	db := New()
	for i := 0; i < 1000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZCard("zset")
	}
}

func BenchmarkZRangeByScore(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := db.ZRangeByScore("zset", 1000, 1099)
		result.Close()
	}
}

func BenchmarkZRange(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		zs := db.ZRange("zset", 0, 99)
		zs.Close()
	}
}

func BenchmarkZRemRangeByScore(b *testing.B) {
	db := New()
	for i := 0; i < 1000; i++ {
		db.ZAdd("zset", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			db.ZAdd("zset", float64(j), []byte(fmt.Sprintf("m%d", j)))
		}
		db.ZRemRangeByScore("zset", 0, 99)
	}
}

func BenchmarkPFCount(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		pb := Buf(fmt.Sprintf("item%d", i))
		db.PFAddBuffer("hll", pb)
		pb.Close()
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.PFCount("hll")
	}
}

func BenchmarkXAdd(b *testing.B) {
	db := New()
	fields := map[string]*PooledBuffer{
		"f1": Buf("value1"),
		"f2": Buf("value2"),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.XAdd("stream", "*", fields)
	}
}

func BenchmarkBFCuckoo(b *testing.B) {
	db := New()
	db.BFReserve("bf", 0.01, 100000)
	db.CFReserve("cf", 10000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.BFAdd("bf", []byte(fmt.Sprintf("i%d", i)))
		db.CFAdd("cf", []byte(fmt.Sprintf("i%d", i)))
	}
}

func BenchmarkCFAddCFExists(b *testing.B) {
	db := New()
	db.CFReserve("cf", 100000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("x%d", i)))
		_ = db.CFExists("cf", []byte("item"))
	}
}

func BenchmarkCFAddCFDel(b *testing.B) {
	db := New()
	db.CFReserve("cf", 100000)
	for i := 0; i < b.N; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("t%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("n%d", i)))
		db.CFDel("cf", []byte(fmt.Sprintf("t%d", i)))
	}
}

func BenchmarkCMSIncrBy(b *testing.B) {
	db := New()
	db.CMSInitByDim("cms", 2000, 10)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.CMSIncrBy("cms", []byte(fmt.Sprintf("item%d", i)), 1)
	}
}

func BenchmarkCMSQuery(b *testing.B) {
	db := New()
	db.CMSInitByDim("cms", 2000, 10)
	for i := 0; i < 1000; i++ {
		db.CMSIncrBy("cms", []byte(fmt.Sprintf("item%d", i)), i)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = db.CMSQuery("cms", []byte("item0"), []byte("item1"), []byte("item2"))
	}
}

func BenchmarkLLen(b *testing.B) {
	db := New()
	for i := 0; i < 1000; i++ {
		db.RPush("list", []byte(fmt.Sprintf("i%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.LLen("list")
	}
}

func BenchmarkHLen(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.HSet("hash", fmt.Sprintf("f%d", i), []byte("v"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HLen("hash")
	}
}

func BenchmarkSCard(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.SAdd("set", []byte(fmt.Sprintf("m%d", i)))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SCard("set")
	}
}

func BenchmarkGeoAdd(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lon := 13.0 + float64(i)*0.001
		lat := 37.0 + float64(i)*0.001
		db.GeoAdd("geo", lon, lat, fmt.Sprintf("p%d", i))
	}
}

func BenchmarkGeoDist(b *testing.B) {
	db := New()
	db.GeoAdd("geo", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("geo", 15.087269, 37.502669, "Catania")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.GeoDist("geo", "Palermo", "Catania", "m")
	}
}

func BenchmarkBitOp(b *testing.B) {
	db := New()
	db.SetBit("k1", 10, 1)
	db.SetBit("k1", 100, 1)
	db.SetBit("k2", 20, 1)
	db.SetBit("k2", 100, 1)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.BitOp("AND", "dst", "k1", "k2")
	}
}

func BenchmarkObjectEncoding(b *testing.B) {
	db := New()
	db.Set("str", []byte("hello"))
	db.LPush("list", []byte("a"), []byte("b"))
	db.HSet("hash", "f1", []byte("v1"))
	db.SAdd("set", []byte("m1"))
	db.ZAdd("zset", 1.0, []byte("m1"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = db.SIsMember("set", []byte("m0"))
	}
}
