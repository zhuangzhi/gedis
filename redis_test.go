package gedis

import (
	"fmt"
	"sync"
	"testing"
)

// ============================================================================
// Arena & Dict (no API changes needed)
// ============================================================================

func TestArenaAllocFree(t *testing.T) {
	a := NewArena(256)

	off1 := a.Alloc(10)
	if off1 != 4 {
		t.Fatalf("expected alloc offset 4, got %d", off1)
	}
	a.WriteBytes(off1, []byte("hello"))

	off2 := a.Alloc(20)
	if off2 <= off1 {
		t.Fatalf("expected off2 > off1")
	}

	a.Free(off1)

	off3 := a.Alloc(10)
	if off3 != off1 {
		t.Fatalf("expected reuse of freed block at %d, got %d", off1, off3)
	}
}

func TestDictSetGet(t *testing.T) {
	a := NewArena(1024)
	d := NewDict(a)

	d.Set([]byte("key1"), 100)
	d.Set([]byte("key2"), 200)

	v, ok := d.Get([]byte("key1"))
	if !ok || v != 100 {
		t.Fatalf("expected 100, got %d, ok=%v", v, ok)
	}

	v, ok = d.Get([]byte("key2"))
	if !ok || v != 200 {
		t.Fatalf("expected 200, got %d, ok=%v", v, ok)
	}

	_, ok = d.Get([]byte("nonexistent"))
	if ok {
		t.Fatal("expected not found")
	}

	d.Set([]byte("key1"), 999)
	v, ok = d.Get([]byte("key1"))
	if !ok || v != 999 {
		t.Fatalf("expected 999 after update, got %d", v)
	}
}

func TestDictDelete(t *testing.T) {
	a := NewArena(1024)
	d := NewDict(a)

	d.Set([]byte("key1"), 100)
	d.Set([]byte("key2"), 200)

	if !d.Del([]byte("key1")) {
		t.Fatal("expected delete to succeed")
	}
	_, ok := d.Get([]byte("key1"))
	if ok {
		t.Fatal("key1 should have been deleted")
	}

	v, ok := d.Get([]byte("key2"))
	if !ok || v != 200 {
		t.Fatalf("key2 should still exist")
	}
}

func TestDictRehash(t *testing.T) {
	a := NewArena(4096)
	d := NewDict(a)

	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("key%d", i)
		d.Set([]byte(k), i)
	}

	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("key%d", i)
		v, ok := d.Get([]byte(k))
		if !ok || v != i {
			t.Fatalf("key %s: expected %d, got %d, ok=%v", k, i, v, ok)
		}
	}
}

// ============================================================================
// Strings
// ============================================================================

func TestSetGet(t *testing.T) {
	db := New()
	db.Set("hello", Buf("world"))

	val, ok := db.Get("hello")
	if !ok || val.String() != "world" {
		t.Fatalf("expected 'world', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	_, ok = db.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestDel(t *testing.T) {
	db := New()
	db.Set("key1", Buf("val1"))
	db.Set("key2", Buf("val2"))

	if !db.Del("key1") {
		t.Fatal("expected delete to succeed")
	}
	if db.Exists("key1") {
		t.Fatal("key1 should not exist")
	}
	if !db.Exists("key2") {
		t.Fatal("key2 should exist")
	}
}

func TestAppend(t *testing.T) {
	db := New()
	n := db.Append("key", Buf("hello"))
	if n != 5 {
		t.Fatalf("expected len 5, got %d", n)
	}

	n = db.Append("key", Buf(" world"))
	if n != 11 {
		t.Fatalf("expected len 11, got %d", n)
	}

	val, _ := db.Get("key")
	if val.String() != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", val.String())
	}
	val.Close()
}

func TestStrlen(t *testing.T) {
	db := New()
	db.Set("key", Buf("hello"))
	if db.Strlen("key") != 5 {
		t.Fatalf("expected strlen 5, got %d", db.Strlen("key"))
	}
	if db.Strlen("nonexistent") != 0 {
		t.Fatal("expected strlen 0 for nonexistent")
	}
}

func TestIncrBy(t *testing.T) {
	db := New()
	val, err := db.IncrBy("counter", 1)
	if err != nil || val != 1 {
		t.Fatalf("expected 1, got %d, err=%v", val, err)
	}

	val, err = db.IncrBy("counter", 5)
	if err != nil || val != 6 {
		t.Fatalf("expected 6, got %d, err=%v", val, err)
	}

	val, err = db.IncrBy("counter", -2)
	if err != nil || val != 4 {
		t.Fatalf("expected 4, got %d, err=%v", val, err)
	}
}

// ============================================================================
// Lists
// ============================================================================

func TestLPushLPop(t *testing.T) {
	db := New()
	n := db.LPush("mylist", Buf("a"), Buf("b"), Buf("c"))
	if n != 3 {
		t.Fatalf("expected len 3, got %d", n)
	}

	val, ok := db.LPop("mylist")
	if !ok || val.String() != "c" {
		t.Fatalf("expected 'c', got '%s'", val.String())
	}
	val.Close()

	val, ok = db.LPop("mylist")
	if !ok || val.String() != "b" {
		t.Fatalf("expected 'b', got '%s'", val.String())
	}
	val.Close()

	val, ok = db.LPop("mylist")
	if !ok || val.String() != "a" {
		t.Fatalf("expected 'a', got '%s'", val.String())
	}
	val.Close()

	_, ok = db.LPop("mylist")
	if ok {
		t.Fatal("expected empty list")
	}
}

func TestRPushRPop(t *testing.T) {
	db := New()
	db.RPush("mylist", Buf("a"), Buf("b"), Buf("c"))

	val, ok := db.RPop("mylist")
	if !ok || val.String() != "c" {
		t.Fatalf("expected 'c', got '%s'", val.String())
	}
	val.Close()
}

func TestLRange(t *testing.T) {
	db := New()
	db.RPush("mylist", Buf("a"), Buf("b"), Buf("c"), Buf("d"))

	result := db.LRange("mylist", 1, 2)
	if len(result) != 2 || result[0].String() != "b" || result[1].String() != "c" {
		t.Fatalf("expected [b c], got %v", result)
	}
}

func TestLLen(t *testing.T) {
	db := New()
	if db.LLen("mylist") != 0 {
		t.Fatal("expected 0")
	}
	db.RPush("mylist", Buf("a"), Buf("b"))
	if db.LLen("mylist") != 2 {
		t.Fatalf("expected 2, got %d", db.LLen("mylist"))
	}
}

// ============================================================================
// Hashes
// ============================================================================

func TestHSetHGet(t *testing.T) {
	db := New()
	db.HSet("myhash", "f1", Buf("v1"))
	db.HSet("myhash", "f2", Buf("v2"))

	v, ok := db.HGet("myhash", "f1")
	if !ok || v.String() != "v1" {
		t.Fatalf("expected 'v1', got '%s'", v.String())
	}
	v.Close()

	_, ok = db.HGet("myhash", "nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestHDel(t *testing.T) {
	db := New()
	db.HSet("myhash", "f1", Buf("v1"))
	db.HSet("myhash", "f2", Buf("v2"))

	deleted := db.HDel("myhash", "f1")
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	_, ok := db.HGet("myhash", "f1")
	if ok {
		t.Fatal("f1 should be deleted")
	}

	_, ok = db.HGet("myhash", "f2")
	if !ok {
		t.Fatal("f2 should exist")
	}
}

func TestHGetAll(t *testing.T) {
	db := New()
	db.HSet("myhash", "a", Buf("1"))
	db.HSet("myhash", "b", Buf("2"))

	all := db.HGetAll("myhash")
	if len(all) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(all))
	}
	if all["a"].String() != "1" || all["b"].String() != "2" {
		t.Fatalf("unexpected values: %v", all)
	}
}

func TestHIncrBy(t *testing.T) {
	db := New()
	val, err := db.HIncrBy("myhash", "count", 1)
	if err != nil || val != 1 {
		t.Fatalf("expected 1, got %d", val)
	}

	val, err = db.HIncrBy("myhash", "count", 5)
	if err != nil || val != 6 {
		t.Fatalf("expected 6, got %d", val)
	}
}

// ============================================================================
// Sets
// ============================================================================

func TestSAddSIsMember(t *testing.T) {
	db := New()
	n := db.SAdd("myset", Buf("a"), Buf("b"), Buf("a"))
	if n != 2 {
		t.Fatalf("expected 2 added, got %d", n)
	}

	if !db.SIsMember("myset", Buf("a")) {
		t.Fatal("expected 'a' to be member")
	}
	if !db.SIsMember("myset", Buf("b")) {
		t.Fatal("expected 'b' to be member")
	}
	if db.SIsMember("myset", Buf("c")) {
		t.Fatal("expected 'c' not to be member")
	}
}

func TestSRem(t *testing.T) {
	db := New()
	db.SAdd("myset", Buf("a"), Buf("b"), Buf("c"))

	removed := db.SRem("myset", Buf("a"), Buf("d"))
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	if db.SIsMember("myset", Buf("a")) {
		t.Fatal("'a' should have been removed")
	}
}

func TestSCard(t *testing.T) {
	db := New()
	db.SAdd("myset", Buf("a"), Buf("b"), Buf("c"))
	if db.SCard("myset") != 3 {
		t.Fatalf("expected 3, got %d", db.SCard("myset"))
	}
}

func TestSMembers(t *testing.T) {
	db := New()
	db.SAdd("myset", Buf("a"), Buf("b"))

	members := db.SMembers("myset")
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

// ============================================================================
// Sorted Sets
// ============================================================================

func TestZAddZScore(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, Buf("a"))
	db.ZAdd("myzset", 2.0, Buf("b"))
	db.ZAdd("myzset", 3.0, Buf("c"))

	score, ok := db.ZScore("myzset", Buf("b"))
	if !ok || score != 2.0 {
		t.Fatalf("expected score 2.0, got %f", score)
	}

	_, ok = db.ZScore("myzset", Buf("nonexistent"))
	if ok {
		t.Fatal("expected not found")
	}
}

func TestZRange(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 3.0, Buf("c"))
	db.ZAdd("myzset", 1.0, Buf("a"))
	db.ZAdd("myzset", 2.0, Buf("b"))

	result := db.ZRange("myzset", 0, -1)
	if result.Len() != 3 {
		t.Fatalf("expected 3 members, got %d", result.Len())
	}
	if string(result.Get(0)) != "a" || string(result.Get(1)) != "b" || string(result.Get(2)) != "c" {
		t.Fatalf("unexpected order: %v, %v, %v", string(result.Get(0)), string(result.Get(1)), string(result.Get(2)))
	}
}

func TestZRangeWithScores(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.5, Buf("x"))
	db.ZAdd("myzset", 2.5, Buf("y"))

	members, scores := db.ZRangeWithScores("myzset", 0, -1)
	if len(members) != 2 || members[0] != "x" || scores[0] != 1.5 {
		t.Fatalf("unexpected result: members=%v, scores=%v", members, scores)
	}
}

func TestZRem(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, Buf("a"))
	db.ZAdd("myzset", 2.0, Buf("b"))

	if !db.ZRem("myzset", Buf("a")) {
		t.Fatal("expected remove to succeed")
	}
	_, ok := db.ZScore("myzset", Buf("a"))
	if ok {
		t.Fatal("'a' should be removed")
	}
}

func TestZCard(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, Buf("a"))
	db.ZAdd("myzset", 2.0, Buf("b"))
	if db.ZCard("myzset") != 2 {
		t.Fatalf("expected 2, got %d", db.ZCard("myzset"))
	}
}

// ============================================================================
// Bitmap
// ============================================================================

func TestBitMapSetGetBit(t *testing.T) {
	db := New()
	old := db.SetBit("mybitmap", 7, 1)
	if old != 0 {
		t.Fatalf("expected old bit 0, got %d", old)
	}

	bit := db.GetBit("mybitmap", 7)
	if bit != 1 {
		t.Fatalf("expected bit 1, got %d", bit)
	}

	old = db.SetBit("mybitmap", 7, 0)
	if old != 1 {
		t.Fatalf("expected old bit 1, got %d", old)
	}
}

func TestBitCount(t *testing.T) {
	db := New()
	db.SetBit("mybitmap", 1, 1)
	db.SetBit("mybitmap", 2, 1)
	db.SetBit("mybitmap", 5, 1)

	count := db.BitCount("mybitmap", 0, -1)
	if count != 3 {
		t.Fatalf("expected count 3, got %d", count)
	}
}

func TestBitOp(t *testing.T) {
	db := New()
	db.SetBit("bm1", 0, 1)
	db.SetBit("bm1", 1, 0)
	db.SetBit("bm2", 0, 1)
	db.SetBit("bm2", 1, 1)

	db.BitOp("AND", "result", "bm1", "bm2")
	if db.GetBit("result", 0) != 1 || db.GetBit("result", 1) != 0 {
		t.Fatal("AND operation failed")
	}

	db.BitOp("OR", "result", "bm1", "bm2")
	if db.GetBit("result", 0) != 1 || db.GetBit("result", 1) != 1 {
		t.Fatal("OR operation failed")
	}
}

// ============================================================================
// HyperLogLog, Geo, Stream, Timeseries, Probabilistic, JSON
// ============================================================================

func TestHyperLogLog(t *testing.T) {
	db := New()
	updated := db.PFAdd("hll", Buf("a"), Buf("b"), Buf("c"))
	if updated != 3 {
		t.Fatalf("expected 3 updated, got %d", updated)
	}

	updated = db.PFAdd("hll", Buf("a"), Buf("b"))
	if updated != 0 {
		t.Fatalf("expected 0 updated, got %d", updated)
	}

	count := db.PFCount("hll")
	if count <= 0 {
		t.Fatalf("expected positive count, got %d", count)
	}
}

func TestGeoAddDist(t *testing.T) {
	db := New()
	db.GeoAdd("locations", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("locations", 15.087269, 37.502669, "Catania")

	dist := db.GeoDist("locations", "Palermo", "Catania", "m")
	if dist <= 0 {
		t.Fatalf("expected positive distance, got %f", dist)
	}
}

func TestStream(t *testing.T) {
	db := New()
	id := db.XAdd("mystream", "*", map[string]*PooledBuffer{
		"name": Buf("Alice"),
		"age":  Buf("30"),
	})
	if id == "" {
		t.Fatal("expected non-empty stream id")
	}

	id2 := db.XAdd("mystream", "*", map[string]*PooledBuffer{
		"name": Buf("Bob"),
	})
	if id2 == "" {
		t.Fatal("expected non-empty stream id")
	}

	if db.XLen("mystream") != 2 {
		t.Fatalf("expected 2 entries, got %d", db.XLen("mystream"))
	}

	result := db.XRead(map[string]string{"mystream": "0-0"}, 10)
	entries, ok := result["mystream"]
	if !ok || len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %v", result)
	}
}

func TestTimeSeries(t *testing.T) {
	db := New()
	db.TSAdd("ts1", 1000, 1.5)
	db.TSAdd("ts1", 2000, 2.5)
	db.TSAdd("ts1", 3000, 3.5)

	ts, val, ok := db.TSLast("ts1")
	if !ok || ts != 3000 || val != 3.5 {
		t.Fatalf("expected (3000, 3.5), got (%d, %f)", ts, val)
	}

	points := db.TSRange("ts1", 1000, 2000)
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}
}

func TestBloomFilter(t *testing.T) {
	db := New()
	db.BFReserve("bf", 0.01, 1000)
	db.BFAdd("bf", Buf("item1"))
	db.BFAdd("bf", Buf("item2"))

	if !db.BFExists("bf", Buf("item1")) {
		t.Fatal("expected item1 to exist")
	}
	if !db.BFExists("bf", Buf("item2")) {
		t.Fatal("expected item2 to exist")
	}
	if db.BFExists("bf", Buf("item3")) {
		t.Fatal("expected item3 not to exist (may be false positive)")
	}
}

func TestCuckooFilter(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1024)
	db.CFAdd("cf", Buf("x"))
	db.CFAdd("cf", Buf("y"))

	if !db.CFExists("cf", Buf("x")) {
		t.Fatal("expected x to exist")
	}

	db.CFDel("cf", Buf("x"))
	if db.CFExists("cf", Buf("x")) {
		t.Fatal("expected x to be deleted")
	}
}

func TestCountMinSketch(t *testing.T) {
	db := New()
	db.CMSInitByDim("cms", 100, 5)

	db.CMSIncrBy("cms", Buf("a"), 5)
	db.CMSIncrBy("cms", Buf("b"), 3)
	db.CMSIncrBy("cms", Buf("a"), 2)

	counts := db.CMSQuery("cms", Buf("a"), Buf("b"), Buf("c"))
	if counts[0] < 7 {
		t.Fatalf("expected a >= 7, got %d", counts[0])
	}
	if counts[2] != 0 {
		t.Fatalf("expected c = 0, got %d", counts[2])
	}
}

func TestTopK(t *testing.T) {
	db := New()
	db.TopKReserve("topk", 3)

	db.TopKAdd("topk", "a", "b", "a", "c", "b", "a")
	items := db.TopKList("topk")
	if len(items) == 0 {
		t.Fatal("expected non-empty topk")
	}
}

func TestJSON(t *testing.T) {
	db := New()
	err := db.JsonSet("doc", "$", map[string]interface{}{
		"name": "John",
		"age":  float64(30),
	})
	if err != nil {
		t.Fatalf("json set error: %v", err)
	}

	val, err := db.JsonGet("doc", "name")
	if err != nil || val != "John" {
		t.Fatalf("expected 'John', got %v", val)
	}

	err = db.JsonDel("doc", "age")
	if err != nil {
		t.Fatalf("json del error: %v", err)
	}

	val, err = db.JsonGet("doc", "age")
	if val != nil {
		t.Fatalf("expected nil after delete, got %v", val)
	}
}

func TestThrottle(t *testing.T) {
	db := New()
	result := db.Throttle("ratelimiter", 10, 1, 1000)
	if !result.Allowed {
		t.Fatal("expected allowed")
	}
	if result.Remaining != 9 {
		t.Fatalf("expected 9 remaining, got %d", result.Remaining)
	}
}

func TestFlushAll(t *testing.T) {
	db := New()
	db.Set("key", Buf("value"))
	db.FlushAll()

	if db.Exists("key") {
		t.Fatal("key should not exist after FlushAll")
	}
}

func TestConcurrency(t *testing.T) {
	db := New()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", n)
			pb := Buf(fmt.Sprintf("val%d", n))
			db.Set(key, pb)
			pb.Close()
		}(i)
	}

	wg.Wait()

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		if !db.Exists(key) {
			t.Fatalf("key %s should exist", key)
		}
	}
}

func TestNoPointerLeak(t *testing.T) {
	db := New()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key%d", i)
		pb := Buf(fmt.Sprintf("value%d", i))
		db.Set(key, pb)
		pb.Close()
	}

	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key%d", i)
		db.Del(key)
	}

	db.FlushAll()

	val, ok := db.Get("nonexistent")
	if ok || val != nil {
		t.Fatal("expected nothing in flushed db")
	}
}
