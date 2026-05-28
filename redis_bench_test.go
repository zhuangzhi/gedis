package gedis

import (
	"math/rand"
	"strconv"
	"sync"
	"testing"
)

const benchKeyPool = 10000

var (
	benchBytesValue      = []byte("value")
	benchBytesX          = []byte("x")
	benchBytesV          = []byte("v")
	benchBytesV1         = []byte("v1")
	benchBytesHello      = []byte("hello")
	benchBytesItem       = []byte("item")
	benchBytesMember     = []byte("member")
	benchBytesVal        = []byte("val")
	benchBytesNew        = []byte("new")
	benchBytesNewVal     = []byte("new-val")
	benchBytesY          = []byte("y")
	benchBytesA          = []byte("a")
	benchBytesB          = []byte("b")
	benchBytesM0         = []byte("m0")
	benchBytesM1         = []byte("m1")
	benchBytesWORLD      = []byte("WORLD")
	benchBytesInit       = []byte("init")
	benchBytesHelloWorld = []byte("hello world hello world")
	benchBytesLarge      = []byte("large-value-data")
	benchBytes0          = []byte("0")
)

func BenchmarkSet(b *testing.B) {
	db := New()
	var keyBuf, valBuf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		kn := copy(keyBuf[:], "key")
		kn += len(strconv.AppendInt(keyBuf[kn:kn], int64(i), 10))
		key := string(keyBuf[:kn])
		vn := copy(valBuf[:], "value")
		vn += len(strconv.AppendInt(valBuf[vn:vn], int64(i), 10))
		db.Set(key, []byte(string(valBuf[:vn])))
	}
}

func BenchmarkGet(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < benchKeyPool; i++ {
		n := copy(buf[:], "key")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := string(buf[:n])
		n2 := copy(buf[:], "value")
		n2 += len(strconv.AppendInt(buf[n2:n2], int64(i), 10))
		db.Set(key, []byte(string(buf[:n2])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "key")
		n += len(strconv.AppendInt(buf[n:n], int64(i%benchKeyPool), 10))
		pb, _ := db.Get(string(buf[:n]))
		if pb != nil {
			pb.Close()
		}
	}
}

func BenchmarkSetGet(b *testing.B) {
	db := New()
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "key")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := string(buf[:n])
		db.Set(key, benchBytesValue)
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
		var buf [24]byte
		i := 0
		for pb.Next() {
			n := copy(buf[:], "key")
			n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
			key := string(buf[:n])
			db.Set(key, benchBytesValue)
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
		db.LPush("list", benchBytesHello)
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
		db.RPush("list", benchBytesHello)
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
		db.HSet("hash", "field", benchBytesValue)
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
		db.SAdd("set", benchBytesMember)
		db.SIsMember("set", benchBytesMember)
	}
}

func BenchmarkZAddZScore(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZAdd("zset", float64(i), benchBytesMember)
		db.ZScore("zset", benchBytesMember)
	}
}

func BenchmarkZAddZRange(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 10000; i++ {
		n := copy(buf[:], "member")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
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
		db.BFAdd("bf", benchBytesItem)
		db.BFExists("bf", benchBytesItem)
	}
}

func BenchmarkPFAdd(b *testing.B) {
	db := New()
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "item")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		pb := BufFromBytes(buf[:n])
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
			var buf [24]byte
			for i := 0; i < b.N/10; i++ {
				n := copy(buf[:], "key")
				n += len(strconv.AppendInt(buf[n:n], int64(r.Intn(benchKeyPool)), 10))
				key := string(buf[:n])
				db.Set(key, benchBytesValue)
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
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	value := []byte("x")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000; j++ {
			n := copy(buf[:], "key")
			n += len(strconv.AppendInt(buf[n:n], int64(j+i*1000), 10))
			key := string(buf[:n])
			db.Set(key, value)
		}
	}
}

func BenchmarkSetThenGet(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < benchKeyPool; i++ {
		n := copy(buf[:], "key")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.Set(string(buf[:n]), benchBytesVal)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "key")
		n += len(strconv.AppendInt(buf[n:n], int64(i%benchKeyPool), 10))
		db.Set(string(buf[:n]), benchBytesNew)
	}
}

func BenchmarkGetSimple(b *testing.B) {
	db := New()
	db.Set("key", benchBytesValue)
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
		db.Set("key", benchBytesValue)
	}
}

func BenchmarkAppend(b *testing.B) {
	db := New()
	db.Set("key", benchBytesInit)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.Append("key", benchBytesX)
	}
}

func BenchmarkSetRange(b *testing.B) {
	db := New()
	db.Set("key", benchBytesHelloWorld)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SetRange("key", 6, benchBytesWORLD)
	}
}

func BenchmarkLIndex(b *testing.B) {
	db := New()
	db.Set("key", benchBytesHello)
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "item")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.RPush("list", []byte(string(buf[:n])))
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
		db.RPush("list", benchBytesX)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.RPush("list", benchBytesY)
	}
}

func BenchmarkLRange(b *testing.B) {
	db := New()
	for i := 0; i < 100; i++ {
		db.RPush("list", benchBytesX)
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
	var buf [24]byte
	value := []byte("x")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "f")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := string(buf[:n])
		db.HSet("hash", key, value)
	}
}

func BenchmarkHDel(b *testing.B) {
	db := New()
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "tmp")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := string(buf[:n])
		db.HSet("hash", key, benchBytesX)
		db.HDel("hash", key)
	}
}

func BenchmarkHGetAll(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "f")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.HSet("hash", string(buf[:n]), benchBytesV)
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
	db.HSet("hash", "count", benchBytes0)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HIncrBy("hash", "count", 1)
	}
}

func BenchmarkHExists(b *testing.B) {
	db := New()
	db.HSet("hash", "f1", benchBytesV1)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HExists("hash", "f1")
	}
}

func BenchmarkHSetLarge(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 1000; i++ {
		n := copy(buf[:], "f")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.HSet("hash", string(buf[:n]), benchBytesLarge)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "f")
		n += len(strconv.AppendInt(buf[n:n], int64(i%1000), 10))
		db.HSet("hash", string(buf[:n]), benchBytesNewVal)
	}
}

func BenchmarkSAdd(b *testing.B) {
	db := New()
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set", []byte(string(buf[:n])))
	}
}

func BenchmarkSMembers(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set", []byte(string(buf[:n])))
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
	var buf [24]byte
	for i := 0; i < 200; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set", []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "t")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := string(buf[:n])
		db.SAdd("set", []byte(key))
		db.SRem("set", []byte(key))
	}
}

func BenchmarkSInter(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set1", []byte(string(buf[:n])))
		n = copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i+50), 10))
		db.SAdd("set2", []byte(string(buf[:n])))
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
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set1", []byte(string(buf[:n])))
		n = copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i+50), 10))
		db.SAdd("set2", []byte(string(buf[:n])))
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
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
	}
}

func BenchmarkZRem(b *testing.B) {
	db := New()
	var buf [24]byte
	n := copy(buf[:], "m")
	n += len(strconv.AppendInt(buf[n:n], int64(9999), 10))
	db.ZAdd("zset", 9999.0, []byte(string(buf[:n])))
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZRem("zset", []byte(string(buf[:n])))
	}
}

func BenchmarkZCard(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 1000; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.ZCard("zset")
	}
}

func BenchmarkZRangeByScore(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 10000; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
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
	var buf [24]byte
	for i := 0; i < 10000; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
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
	var buf [24]byte
	for i := 0; i < 1000; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.ZAdd("zset", float64(i), []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			n := copy(buf[:], "m")
			n += len(strconv.AppendInt(buf[n:n], int64(j), 10))
			db.ZAdd("zset", float64(j), []byte(string(buf[:n])))
		}
		db.ZRemRangeByScore("zset", 0, 99)
	}
}

func BenchmarkPFCount(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 10000; i++ {
		n := copy(buf[:], "item")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		pb := BufFromBytes(buf[:n])
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
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "i")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		key := []byte(string(buf[:n]))
		db.BFAdd("bf", key)
		db.CFAdd("cf", key)
	}
}

func BenchmarkCFAddCFExists(b *testing.B) {
	db := New()
	db.CFReserve("cf", 100000)
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "x")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CFAdd("cf", []byte(string(buf[:n])))
		_ = db.CFExists("cf", benchBytesItem)
	}
}

func BenchmarkCFAddCFDel(b *testing.B) {
	db := New()
	db.CFReserve("cf", 100000)
	var buf [24]byte
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "t")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CFAdd("cf", []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "n")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CFAdd("cf", []byte(string(buf[:n])))
		n = copy(buf[:], "t")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CFDel("cf", []byte(string(buf[:n])))
	}
}

func BenchmarkCMSIncrBy(b *testing.B) {
	db := New()
	db.CMSInitByDim("cms", 2000, 10)
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := copy(buf[:], "item")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CMSIncrBy("cms", []byte(string(buf[:n])), 1)
	}
}

func BenchmarkCMSQuery(b *testing.B) {
	db := New()
	db.CMSInitByDim("cms", 2000, 10)
	var buf [24]byte
	for i := 0; i < 1000; i++ {
		n := copy(buf[:], "item")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.CMSIncrBy("cms", []byte(string(buf[:n])), i)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = db.CMSQuery("cms", benchBytesItem, benchBytesItem, benchBytesItem)
	}
}

func BenchmarkLLen(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 1000; i++ {
		n := copy(buf[:], "i")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.RPush("list", []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.LLen("list")
	}
}

func BenchmarkHLen(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "f")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.HSet("hash", string(buf[:n]), benchBytesV)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.HLen("hash")
	}
}

func BenchmarkSCard(b *testing.B) {
	db := New()
	var buf [24]byte
	for i := 0; i < 100; i++ {
		n := copy(buf[:], "m")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.SAdd("set", []byte(string(buf[:n])))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db.SCard("set")
	}
}

func BenchmarkGeoAdd(b *testing.B) {
	db := New()
	var buf [24]byte
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lon := 13.0 + float64(i)*0.001
		lat := 37.0 + float64(i)*0.001
		n := copy(buf[:], "p")
		n += len(strconv.AppendInt(buf[n:n], int64(i), 10))
		db.GeoAdd("geo", lon, lat, string(buf[:n]))
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
	db.Set("str", benchBytesHello)
	db.LPush("list", benchBytesA, benchBytesB)
	db.HSet("hash", "f1", benchBytesV1)
	db.SAdd("set", benchBytesM1)
	db.ZAdd("zset", 1.0, benchBytesM1)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = db.SIsMember("set", benchBytesM0)
	}
}
