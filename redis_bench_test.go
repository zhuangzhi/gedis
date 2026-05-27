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
		pb := Buf(fmt.Sprintf("value%d", i))
		db.Set(fmt.Sprintf("key%d", i), pb)
		pb.Close()
	}
}

func BenchmarkGet(b *testing.B) {
	db := New()
	for i := 0; i < benchKeyPool; i++ {
		pb := Buf(fmt.Sprintf("value%d", i))
		db.Set(fmt.Sprintf("key%d", i), pb)
		pb.Close()
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
		pb := Buf("value")
		db.Set(key, pb)
		pb.Close()
		pb2, _ := db.Get(key)
		if pb2 != nil {
			pb2.Close()
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
			val := Buf("value")
			db.Set(key, val)
			val.Close()
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
		pb := Buf("hello")
		db.LPush("list", pb)
		pb.Close()
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
		pb := Buf("hello")
		db.RPush("list", pb)
		pb.Close()
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
		pb := Buf("value")
		db.HSet("hash", "field", pb)
		pb.Close()
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
		pb := Buf("member")
		db.SAdd("set", pb)
		pb.Close()
		pb2 := Buf("member")
		db.SIsMember("set", pb2)
		pb2.Close()
	}
}

func BenchmarkZAddZScore(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb := Buf("member")
		db.ZAdd("zset", float64(i), pb)
		pb.Close()
		db.ZScore("zset", Buf("member"))
	}
}

func BenchmarkZAddZRange(b *testing.B) {
	db := New()
	for i := 0; i < 10000; i++ {
		pb := Buf(fmt.Sprintf("member%d", i))
		db.ZAdd("zset", float64(i), pb)
		pb.Close()
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
		pb := Buf("item")
		db.BFAdd("bf", pb)
		pb.Close()
		db.BFExists("bf", Buf("item"))
	}
}

func BenchmarkPFAdd(b *testing.B) {
	db := New()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb := Buf(fmt.Sprintf("item%d", i))
		db.PFAdd("hll", pb)
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
				pb := Buf("value")
				db.Set(key, pb)
				pb.Close()
				v, _ := db.Get(key)
				if v != nil {
					v.Close()
				}
			}
		}(g)
	}
	wg.Wait()
}
