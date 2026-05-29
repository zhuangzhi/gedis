package gedis

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
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
	db.Set("hello", []byte("world"))

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
	db.Set("key1", []byte("val1"))
	db.Set("key2", []byte("val2"))

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
	n := db.Append("key", []byte("hello"))
	if n != 5 {
		t.Fatalf("expected len 5, got %d", n)
	}

	n = db.Append("key", []byte(" world"))
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
	db.Set("key", []byte("hello"))
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
	n := db.LPush("mylist", []byte("a"), []byte("b"), []byte("c"))
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
	db.RPush("mylist", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.RPop("mylist")
	if !ok || val.String() != "c" {
		t.Fatalf("expected 'c', got '%s'", val.String())
	}
	val.Close()
}

func TestLRange(t *testing.T) {
	db := New()
	db.RPush("mylist", []byte("a"), []byte("b"), []byte("c"), []byte("d"))

	result := db.LRange("mylist", 1, 2)
	if result.Len() != 2 || string(result.Get(0)) != "b" || string(result.Get(1)) != "c" {
		t.Fatalf("expected [b c], got %v", result)
	}
	result.Close()
}

func TestLLen(t *testing.T) {
	db := New()
	if db.LLen("mylist") != 0 {
		t.Fatal("expected 0")
	}
	db.RPush("mylist", []byte("a"), []byte("b"))
	if db.LLen("mylist") != 2 {
		t.Fatalf("expected 2, got %d", db.LLen("mylist"))
	}
}

// ============================================================================
// Hashes
// ============================================================================

func TestHSetHGet(t *testing.T) {
	db := New()
	db.HSet("myhash", "f1", []byte("v1"))
	db.HSet("myhash", "f2", []byte("v2"))

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
	db.HSet("myhash", "f1", []byte("v1"))
	db.HSet("myhash", "f2", []byte("v2"))

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
	db.HSet("myhash", "a", []byte("1"))
	db.HSet("myhash", "b", []byte("2"))

	all := db.HGetAll("myhash")
	if all.Len() != 4 {
		t.Fatalf("expected 4 entries (2 fields x 2), got %d", all.Len())
	}
	fields := make(map[string]string)
	for i := 0; i < all.Len(); i += 2 {
		fields[string(all.Get(i))] = string(all.Get(i + 1))
	}
	if fields["a"] != "1" || fields["b"] != "2" {
		t.Fatalf("unexpected values: %v", fields)
	}
	all.Close()
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
	n := db.SAdd("myset", []byte("a"), []byte("b"), []byte("a"))
	if n != 2 {
		t.Fatalf("expected 2 added, got %d", n)
	}

	if !db.SIsMember("myset", []byte("a")) {
		t.Fatal("expected 'a' to be member")
	}
	if !db.SIsMember("myset", []byte("b")) {
		t.Fatal("expected 'b' to be member")
	}
	if db.SIsMember("myset", []byte("c")) {
		t.Fatal("expected 'c' not to be member")
	}
}

func TestSRem(t *testing.T) {
	db := New()
	db.SAdd("myset", []byte("a"), []byte("b"), []byte("c"))

	removed := db.SRem("myset", []byte("a"), []byte("d"))
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	if db.SIsMember("myset", []byte("a")) {
		t.Fatal("'a' should have been removed")
	}
}

func TestSCard(t *testing.T) {
	db := New()
	db.SAdd("myset", []byte("a"), []byte("b"), []byte("c"))
	if db.SCard("myset") != 3 {
		t.Fatalf("expected 3, got %d", db.SCard("myset"))
	}
}

func TestSMembers(t *testing.T) {
	db := New()
	db.SAdd("myset", []byte("a"), []byte("b"))

	members := db.SMembers("myset")
	if members.Len() != 2 {
		t.Fatalf("expected 2 members, got %d", members.Len())
	}
	members.Close()
}

// ============================================================================
// Sorted Sets
// ============================================================================

func TestZAddZScore(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, []byte("a"))
	db.ZAdd("myzset", 2.0, []byte("b"))
	db.ZAdd("myzset", 3.0, []byte("c"))

	score, ok := db.ZScore("myzset", []byte("b"))
	if !ok || score != 2.0 {
		t.Fatalf("expected score 2.0, got %f", score)
	}

	_, ok = db.ZScore("myzset", []byte("nonexistent"))
	if ok {
		t.Fatal("expected not found")
	}
}

func TestZRange(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 3.0, []byte("c"))
	db.ZAdd("myzset", 1.0, []byte("a"))
	db.ZAdd("myzset", 2.0, []byte("b"))

	result := db.ZRange("myzset", 0, -1)
	if result.Len() != 3 {
		t.Fatalf("expected 3 members, got %d", result.Len())
	}
	if string(result.Get(0)) != "a" || string(result.Get(1)) != "b" || string(result.Get(2)) != "c" {
		t.Fatalf("unexpected order: %v, %v, %v", string(result.Get(0)), string(result.Get(1)), string(result.Get(2)))
	}
	result.Close()
}

func TestZRangeWithScores(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.5, []byte("x"))
	db.ZAdd("myzset", 2.5, []byte("y"))

	members, scores := db.ZRangeWithScores("myzset", 0, -1)
	if members.Len() != 2 || string(members.Get(0)) != "x" || scores[0] != 1.5 {
		t.Fatalf("unexpected result: members=%v, scores=%v", members, scores)
	}
	members.Close()
}

func TestZRem(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, []byte("a"))
	db.ZAdd("myzset", 2.0, []byte("b"))

	if !db.ZRem("myzset", []byte("a")) {
		t.Fatal("expected remove to succeed")
	}
	_, ok := db.ZScore("myzset", []byte("a"))
	if ok {
		t.Fatal("'a' should be removed")
	}
}

func TestZCard(t *testing.T) {
	db := New()
	db.ZAdd("myzset", 1.0, []byte("a"))
	db.ZAdd("myzset", 2.0, []byte("b"))
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
	updated := db.PFAddBuffer("hll", Buf("a"), Buf("b"), Buf("c"))
	if updated != 3 {
		t.Fatalf("expected 3 updated, got %d", updated)
	}

	updated = db.PFAddBuffer("hll", Buf("a"), Buf("b"))
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
	db.BFAdd("bf", []byte("item1"))
	db.BFAdd("bf", []byte("item2"))

	if !db.BFExists("bf", []byte("item1")) {
		t.Fatal("expected item1 to exist")
	}
	if !db.BFExists("bf", []byte("item2")) {
		t.Fatal("expected item2 to exist")
	}
	if db.BFExists("bf", []byte("item3")) {
		t.Fatal("expected item3 not to exist (may be false positive)")
	}
}

func TestCuckooFilter(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1024)
	db.CFAdd("cf", []byte("x"))
	db.CFAdd("cf", []byte("y"))

	if !db.CFExists("cf", []byte("x")) {
		t.Fatal("expected x to exist")
	}

	db.CFDel("cf", []byte("x"))
	if db.CFExists("cf", []byte("x")) {
		t.Fatal("expected x to be deleted")
	}
}

func TestCountMinSketch(t *testing.T) {
	db := New()
	db.CMSInitByDim("cms", 100, 5)

	db.CMSIncrBy("cms", []byte("a"), 5)
	db.CMSIncrBy("cms", []byte("b"), 3)
	db.CMSIncrBy("cms", []byte("a"), 2)

	counts := db.CMSQuery("cms", []byte("a"), []byte("b"), []byte("c"))
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
	db.Set("key", []byte("value"))
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
			db.SetBuffer(key, pb)
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
		db.SetBuffer(key, pb)
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

// ============================================================================
// Strings Extended
// ============================================================================

func TestGetRange(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello world"))

	v, ok := db.GetRange("key", 0, 4)
	if !ok || v.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s'", v.String())
	}
	v.Close()

	v, ok = db.GetRange("key", -5, -1)
	if !ok || v.String() != "world" {
		t.Fatalf("expected 'world', got '%s'", v.String())
	}
	v.Close()

	v, ok = db.GetRange("key", 100, 200)
	if ok {
		t.Fatal("expected not found for out-of-range")
	}

	_, ok = db.GetRange("nonexistent", 0, 5)
	if ok {
		t.Fatal("expected not found")
	}
}

func TestGetRangeInt(t *testing.T) {
	db := New()
	db.IncrBy("counter", 42)

	v, ok := db.GetRange("counter", 0, 0)
	if !ok || v.String() != "4" {
		t.Fatalf("expected '4', got '%s'", v.String())
	}
	v.Close()
}

func TestSetRange(t *testing.T) {
	db := New()
	n := db.SetRange("key", 0, []byte("HELLO"))
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	v, _ := db.Get("key")
	if v.String() != "HELLO" {
		t.Fatalf("expected 'HELLO', got '%s'", v.String())
	}
	v.Close()

	n = db.SetRange("key", 6, []byte("WORLD"))
	if n != 11 {
		t.Fatalf("expected 11, got %d", n)
	}
	v, _ = db.Get("key")
	if v.String()[:5] != "HELLO" || v.String()[6:] != "WORLD" {
		t.Fatalf("expected HELLO...WORLD, got '%s'", v.String())
	}
	v.Close()
}

func TestSetRangeInt(t *testing.T) {
	db := New()
	db.IncrBy("num", 10)
	db.SetRange("num", 1, []byte("X"))

	v, _ := db.Get("num")
	if v.String() != "1X" {
		t.Fatalf("expected '1X', got '%s'", v.String())
	}
	v.Close()
}

func TestIncrByFloat(t *testing.T) {
	db := New()
	val, err := db.IncrByFloat("f", 0.5)
	if err != nil || val != 0.5 {
		t.Fatalf("expected 0.5, got %f", val)
	}

	val, err = db.IncrByFloat("f", 1.25)
	if err != nil || val != 1.75 {
		t.Fatalf("expected 1.75, got %f", val)
	}

	val, err = db.IncrByFloat("f", -0.25)
	if err != nil || val != 1.5 {
		t.Fatalf("expected 1.5, got %f", val)
	}
}

func TestIncrByFloatFromInt(t *testing.T) {
	db := New()
	db.IncrBy("n", 5)
	val, err := db.IncrByFloat("n", 0.5)
	if err != nil || val != 5.5 {
		t.Fatalf("expected 5.5, got %f", val)
	}
}

func TestStrlenInt(t *testing.T) {
	db := New()
	db.IncrBy("n", 100)
	if db.Strlen("n") != 3 {
		t.Fatalf("expected strlen 3, got %d", db.Strlen("n"))
	}
}

func TestAppendNew(t *testing.T) {
	db := New()
	n := db.Append("newkey", []byte("hello"))
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	v, _ := db.Get("newkey")
	if v.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s'", v.String())
	}
	v.Close()
}

func TestAppendInt(t *testing.T) {
	db := New()
	db.IncrBy("n", 10)
	n := db.Append("n", []byte("0"))
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
	v, _ := db.Get("n")
	if v.String() != "100" {
		t.Fatalf("expected '100', got '%s'", v.String())
	}
	v.Close()
}

func TestGetInt(t *testing.T) {
	db := New()
	db.IncrBy("n", 42)
	v, ok := db.Get("n")
	if !ok || v.String() != "42" {
		t.Fatalf("expected '42', got '%s'", v.String())
	}
	v.Close()
}

func TestIncrByStringValue(t *testing.T) {
	db := New()
	db.Set("key", []byte("100"))
	val, err := db.IncrBy("key", 50)
	if err != nil || val != 150 {
		t.Fatalf("expected 150, got %d", val)
	}
}

func TestIncrByZeroData(t *testing.T) {
	db := New()
	db.Set("key", []byte("0"))
	db.IncrBy("key", 10)
	v, _ := db.Get("key")
	if v.String() != "10" {
		t.Fatalf("expected '10', got '%s'", v.String())
	}
	v.Close()
}

func TestGetRangeNegativeStart(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))

	v, ok := db.GetRange("key", -3, -1)
	if !ok || v.String() != "llo" {
		t.Fatalf("expected 'llo', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// Lists Extended
// ============================================================================

func TestLRangeNegative(t *testing.T) {
	db := New()
	db.RPush("l", []byte("a"), []byte("b"), []byte("c"), []byte("d"))

	result := db.LRange("l", -3, -1)
	if result.Len() != 3 || string(result.Get(0)) != "b" {
		t.Fatalf("expected [b c d], got %v", result)
	}
	result.Close()
}

func TestLRangeOutOfBounds(t *testing.T) {
	db := New()
	db.RPush("l", []byte("a"), []byte("b"))

	result := db.LRange("l", 0, 100)
	if result.Len() != 2 {
		t.Fatalf("expected 2, got %d", result.Len())
	}
	result.Close()

	result = db.LRange("l", -100, 0)
	if result.Len() != 1 || string(result.Get(0)) != "a" {
		t.Fatalf("expected [a], got %v", result)
	}
	result.Close()
}

func TestLIndexNegative(t *testing.T) {
	db := New()
	db.RPush("l", []byte("a"), []byte("b"), []byte("c"))

	v, ok := db.LIndex("l", -1)
	if !ok || v.String() != "c" {
		t.Fatalf("expected 'c', got '%s'", v.String())
	}
	v.Close()

	_, ok = db.LIndex("l", 10)
	if ok {
		t.Fatal("expected not found")
	}
}

func TestLLenEmpty(t *testing.T) {
	db := New()
	if db.LLen("nonexistent") != 0 {
		t.Fatal("expected 0")
	}
}

func TestRPopEmpty(t *testing.T) {
	db := New()
	db.RPush("l", []byte("a"))
	_, _ = db.RPop("l")
	_, ok := db.RPop("l")
	if ok {
		t.Fatal("expected empty list")
	}
}

func TestLPushLPopEmpty(t *testing.T) {
	db := New()
	db.LPush("l", []byte("a"))
	db.LPop("l")
	_, ok := db.LPop("l")
	if ok {
		t.Fatal("expected empty")
	}
}

func TestLRangeEmpty(t *testing.T) {
	db := New()
	result := db.LRange("nonexistent", 0, 10)
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestLIndexNonexistent(t *testing.T) {
	db := New()
	_, ok := db.LIndex("nonexistent", 0)
	if ok {
		t.Fatal("expected not found")
	}
}

// ============================================================================
// LTrim / LMove
// ============================================================================

func TestLTrim(t *testing.T) {
	db := New()
	db.LPush("l", []byte("d"), []byte("c"), []byte("b"), []byte("a"))

	ok := db.LTrim("l", 0, 1)
	if !ok {
		t.Fatal("expected trim success")
	}

	if db.LLen("l") != 2 {
		t.Fatalf("expected 2, got %d", db.LLen("l"))
	}

	first, _ := db.LIndex("l", 0)
	if first.String() != "a" {
		t.Fatalf("expected first 'a', got '%s'", first.String())
	}
}

func TestLTrimNonexistent(t *testing.T) {
	db := New()
	ok := db.LTrim("nonexistent", 0, 1)
	if ok {
		t.Fatal("expected false for nonexistent key")
	}
}

func TestLTrimOutOfRange(t *testing.T) {
	db := New()
	db.LPush("l", []byte("a"), []byte("b"))

	ok := db.LTrim("l", 5, 10)
	if !ok {
		t.Fatal("expected trim success even with out of range")
	}

	if db.LLen("l") != 0 {
		t.Fatalf("expected 0, got %d", db.LLen("l"))
	}
}

func TestLTrimNegativeIndices(t *testing.T) {
	db := New()
	db.LPush("l", []byte("e"), []byte("d"), []byte("c"), []byte("b"), []byte("a"))

	ok := db.LTrim("l", 1, -1)
	if !ok {
		t.Fatal("expected trim success")
	}

	if db.LLen("l") != 4 {
		t.Fatalf("expected 4, got %d", db.LLen("l"))
	}
}

func TestLMove(t *testing.T) {
	db := New()
	db.LPush("src", []byte("b"), []byte("a"))

	elem, ok := db.LMove("src", "dst", "LEFT", "RIGHT")
	if !ok {
		t.Fatal("expected move success")
	}
	if elem.String() != "a" {
		t.Fatalf("expected 'a', got '%s'", elem.String())
	}
	elem.Close()

	if db.LLen("src") != 1 {
		t.Fatalf("expected src len 1, got %d", db.LLen("src"))
	}
	if db.LLen("dst") != 1 {
		t.Fatalf("expected dst len 1, got %d", db.LLen("dst"))
	}
}

func TestLMoveRightToLeft(t *testing.T) {
	db := New()
	db.LPush("src", []byte("b"), []byte("a"))

	elem, ok := db.LMove("src", "dst", "RIGHT", "LEFT")
	if !ok {
		t.Fatal("expected move success")
	}
	if elem.String() != "b" {
		t.Fatalf("expected 'b', got '%s'", elem.String())
	}
	elem.Close()

	first, _ := db.LIndex("dst", 0)
	if first.String() != "b" {
		t.Fatalf("expected dst first 'b', got '%s'", first.String())
	}
}

func TestLMoveNonexistentSrc(t *testing.T) {
	db := New()
	elem, ok := db.LMove("nonexistent", "dst", "LEFT", "RIGHT")
	if ok || elem != nil {
		t.Fatal("expected failure for nonexistent src")
	}
}

func TestLMoveCreateDst(t *testing.T) {
	db := New()
	db.LPush("src", []byte("a"))

	elem, ok := db.LMove("src", "dst", "LEFT", "LEFT")
	if !ok {
		t.Fatal("expected move success")
	}
	elem.Close()

	if db.LLen("dst") != 1 {
		t.Fatalf("expected dst len 1, got %d", db.LLen("dst"))
	}
}

// ============================================================================
// Hashes Extended
// ============================================================================

func TestHExists(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))

	if !db.HExists("h", "f1") {
		t.Fatal("expected f1 to exist")
	}
	if db.HExists("h", "f2") {
		t.Fatal("expected f2 not to exist")
	}
	if db.HExists("nonexistent", "f1") {
		t.Fatal("expected false for nonexistent hash")
	}
}

func TestHLen(t *testing.T) {
	db := New()
	if db.HLen("nonexistent") != 0 {
		t.Fatal("expected 0")
	}

	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))
	if db.HLen("h") != 2 {
		t.Fatalf("expected 2, got %d", db.HLen("h"))
	}

	db.HSet("h", "f1", []byte("new"))
	if db.HLen("h") != 2 {
		t.Fatalf("expected still 2, got %d", db.HLen("h"))
	}
}

func TestHDelMulti(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))
	db.HSet("h", "f3", []byte("v3"))

	deleted := db.HDel("h", "f1", "f3", "nonexistent")
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}
	if db.HExists("h", "f1") || db.HExists("h", "f3") {
		t.Fatal("f1, f3 should be deleted")
	}
	if !db.HExists("h", "f2") {
		t.Fatal("f2 should exist")
	}
}

func TestHDelNonexistent(t *testing.T) {
	db := New()
	deleted := db.HDel("nonexistent", "f1")
	if deleted != 0 {
		t.Fatalf("expected 0, got %d", deleted)
	}
}

func TestHDelAll(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HDel("h", "f1")

	_, ok := db.HGet("h", "f1")
	if ok {
		t.Fatal("hash should be auto-deleted")
	}
}

func TestHSetUpdate(t *testing.T) {
	db := New()
	n := db.HSet("h", "f1", []byte("v1"))
	if n != 1 {
		t.Fatalf("expected 1 new, got %d", n)
	}
	n = db.HSet("h", "f1", []byte("v2"))
	if n != 0 {
		t.Fatalf("expected 0 (updated), got %d", n)
	}
	v, _ := db.HGet("h", "f1")
	if v.String() != "v2" {
		t.Fatalf("expected 'v2', got '%s'", v.String())
	}
	v.Close()
}

func TestHIncrByNewField(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	val, err := db.HIncrBy("h", "count", 5)
	if err != nil || val != 5 {
		t.Fatalf("expected 5, got %d", val)
	}
}

func TestHGetAllEmpty(t *testing.T) {
	db := New()
	result := db.HGetAll("nonexistent")
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestHGetNotFound(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	_, ok := db.HGet("h", "f2")
	if ok {
		t.Fatal("expected not found")
	}
	_, ok = db.HGet("nonexistent", "f1")
	if ok {
		t.Fatal("expected not found")
	}
}

// ============================================================================
// Sets Extended
// ============================================================================

func TestSInter(t *testing.T) {
	db := New()
	db.SAdd("s1", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("s2", []byte("b"), []byte("c"), []byte("d"))

	result := db.SInter("s1", "s2")
	if result.Len() != 2 {
		t.Fatalf("expected 2, got %d", result.Len())
	}
	result.Close()
}

func TestSInterEmpty(t *testing.T) {
	db := New()
	db.SAdd("s1", []byte("a"), []byte("b"))
	db.SAdd("s2", []byte("c"), []byte("d"))

	result := db.SInter("s1", "s2")
	if result.Len() != 0 {
		t.Fatalf("expected 0, got %d", result.Len())
	}
	result.Close()
}

func TestSInterNonexistent(t *testing.T) {
	db := New()
	db.SAdd("s1", []byte("a"))
	result := db.SInter("s1", "nonexistent")
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestSUnion(t *testing.T) {
	db := New()
	db.SAdd("s1", []byte("a"), []byte("b"))
	db.SAdd("s2", []byte("b"), []byte("c"))

	result := db.SUnion("s1", "s2")
	if result.Len() != 3 {
		t.Fatalf("expected 3, got %d", result.Len())
	}
	result.Close()
}

func TestSMembersEmpty(t *testing.T) {
	db := New()
	result := db.SMembers("nonexistent")
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestSCardEmpty(t *testing.T) {
	db := New()
	if db.SCard("nonexistent") != 0 {
		t.Fatal("expected 0")
	}
}

func TestSRemAll(t *testing.T) {
	db := New()
	db.SAdd("s", []byte("a"), []byte("b"))
	db.SRem("s", []byte("a"), []byte("b"))

	if db.SCard("s") != 0 {
		t.Fatal("set should be empty")
	}
}

// ============================================================================
// ZSet Extended
// ============================================================================

func TestZRangeByScore(t *testing.T) {
	db := New()
	for i := 0; i < 100; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}

	result := db.ZRangeByScore("z", 10, 19)
	if result.Len() != 10 {
		t.Fatalf("expected 10, got %d", result.Len())
	}
	result.Close()
}

func TestZRangeByScoreEmpty(t *testing.T) {
	db := New()
	result := db.ZRangeByScore("nonexistent", 0, 10)
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestZRemRangeByScore(t *testing.T) {
	db := New()
	for i := 0; i < 50; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}

	db.ZRemRangeByScore("z", 0, 9)
	if db.ZCard("z") != 40 {
		t.Fatalf("expected 40, got %d", db.ZCard("z"))
	}
}

func TestZScoreNonexistentKey(t *testing.T) {
	db := New()
	_, ok := db.ZScore("nonexistent", []byte("a"))
	if ok {
		t.Fatal("expected not found")
	}
}

func TestZCardEmpty(t *testing.T) {
	db := New()
	if db.ZCard("nonexistent") != 0 {
		t.Fatal("expected 0")
	}
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 10.0, []byte("a"))
	if db.ZCard("z") != 2 {
		t.Fatalf("expected 2, got %d", db.ZCard("z"))
	}
}

// ============================================================================
// Geo Extended
// ============================================================================

func TestGeoPos(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "Palermo")

	positions := db.GeoPos("g", "Palermo", "nonexistent")
	if len(positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(positions))
	}
	if positions[0][0] == 0 && positions[0][1] == 0 {
		t.Fatal("expected Palermo coords")
	}
	if positions[1][0] != 0 || positions[1][1] != 0 {
		t.Fatal("expected 0,0 for nonexistent")
	}
}

func TestGeoPosEmpty(t *testing.T) {
	db := New()
	result := db.GeoPos("nonexistent", "X")
	if len(result) != 1 || result[0][0] != 0 {
		t.Fatal("expected 0,0")
	}
}

func TestGeoRadius(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("g", 15.087269, 37.502669, "Catania")

	result := db.GeoRadius("g", 15.0, 37.0, 200000, "m")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	result.Close()
}

func TestGeoRadiusEmpty(t *testing.T) {
	db := New()
	result := db.GeoRadius("nonexistent", 0, 0, 100, "m")
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestGeoRadiusByMember(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("g", 15.087269, 37.502669, "Catania")

	result := db.GeoRadiusByMember("g", "Palermo", 200000, "m")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	result.Close()
}

func TestGeoRadiusByMemberBad(t *testing.T) {
	db := New()
	result := db.GeoRadiusByMember("nonexistent", "X", 100, "m")
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestGeoDistUnit(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("g", 15.087269, 37.502669, "Catania")

	dM := db.GeoDist("g", "Palermo", "Catania", "m")
	dKm := db.GeoDist("g", "Palermo", "Catania", "km")
	if dM <= 0 || dKm <= 0 {
		t.Fatalf("expected positive distances: m=%f, km=%f", dM, dKm)
	}
}

// ============================================================================
// Bloom / Cuckoo Extended
// ============================================================================

func TestBFAddAutoCreate(t *testing.T) {
	db := New()
	db.BFAdd("bf", []byte("x"))
	if !db.BFExists("bf", []byte("x")) {
		t.Fatal("expected x to exist")
	}
}

func TestBFReserveCustom(t *testing.T) {
	db := New()
	db.BFReserve("bf", 0.001, 5000)
	db.BFAdd("bf", []byte("a"))
	if !db.BFExists("bf", []byte("a")) {
		t.Fatal("expected a to exist")
	}
}

func TestBFExistsNonexistent(t *testing.T) {
	db := New()
	if db.BFExists("nonexistent", []byte("x")) {
		t.Fatal("expected false")
	}
}

func TestCFAddAutoCreate(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1024)
	db.CFAdd("cf", []byte("x"))
	if !db.CFExists("cf", []byte("x")) {
		t.Fatal("expected x to exist")
	}
}

func TestCFExistsNonexistent(t *testing.T) {
	db := New()
	if db.CFExists("nonexistent", []byte("x")) {
		t.Fatal("expected false")
	}
}

func TestTopKReserve(t *testing.T) {
	db := New()
	db.TopKReserve("topk", 3)

	db.TopKAdd("topk", "a", "b", "a", "c", "b", "a")
	items := db.TopKList("topk")
	if len(items) == 0 {
		t.Fatal("expected non-empty topk")
	}
	foundA := false
	for _, it := range items {
		if it.Item == "a" {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Fatal("expected 'a' in topk")
	}
}

func TestCMSEmptyQuery(t *testing.T) {
	db := New()
	db.CMSInitByDim("cms", 100, 5)
	counts := db.CMSQuery("cms", []byte("x"))
	if len(counts) != 1 || counts[0] != 0 {
		t.Fatalf("expected [0], got %v", counts)
	}
}

func TestCMSInitByDimEdge(t *testing.T) {
	db := New()
	db.CMSInitByDim("cms", 100, 5)
	db.CMSIncrBy("cms", []byte("a"), 1)
	counts := db.CMSQuery("cms", []byte("a"))
	if counts[0] < 1 {
		t.Fatalf("expected >= 1, got %d", counts[0])
	}
}

// ============================================================================
// Stream Extended
// ============================================================================

func TestStreamXGroupCreate(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f1": Buf("v1")})
	err := db.XGroupCreate("s", "mygroup", "0-0")
	if err != nil {
		t.Fatalf("group create error: %v", err)
	}
}

func TestStreamXGroupCreateBadKey(t *testing.T) {
	db := New()
	err := db.XGroupCreate("nonexistent", "g", "0-0")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "no such key" {
		t.Fatalf("expected 'no such key', got '%s'", err.Error())
	}
}

func TestStreamXReadGroup(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f1": Buf("v1")})
	db.XAdd("s", "*", map[string]*PooledBuffer{"f2": Buf("v2")})
	db.XGroupCreate("s", "g", "0-0")

	result := db.XReadGroup("g", "c1", map[string]string{"s": "0-0"}, 10)
	if len(result) == 0 {
		t.Fatal("expected entries")
	}
}

func TestStreamXReadGroupEmpty(t *testing.T) {
	db := New()
	result := db.XReadGroup("g", "c1", map[string]string{"nonexistent": "0-0"}, 10)
	if len(result) != 0 {
		t.Fatal("expected empty")
	}
}

func TestStreamXLen(t *testing.T) {
	db := New()
	if db.XLen("nonexistent") != 0 {
		t.Fatal("expected 0")
	}
	db.XAdd("s", "*", map[string]*PooledBuffer{"f1": Buf("v1")})
	if db.XLen("s") != 1 {
		t.Fatalf("expected 1, got %d", db.XLen("s"))
	}
}

func TestStreamXReadEmpty(t *testing.T) {
	db := New()
	result := db.XRead(map[string]string{"nonexistent": "0-0"}, 10)
	if len(result) != 0 {
		t.Fatal("expected empty")
	}
}

func TestStreamXReadCount(t *testing.T) {
	db := New()
	for i := 0; i < 10; i++ {
		db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	}
	result := db.XRead(map[string]string{"s": "0-0"}, 3)
	if len(result["s"]) != 3 {
		t.Fatalf("expected 3, got %d", len(result["s"]))
	}
}

// ============================================================================
// JSON Extended
// ============================================================================

func TestJsonArrAppend(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{
		"items": []interface{}{"a", "b"},
	})

	err := db.JsonArrAppend("doc", "items", "c", "d")
	if err != nil {
		t.Fatalf("arr append error: %v", err)
	}

	val, _ := db.JsonGet("doc", "items")
	arr, ok := val.([]interface{})
	if !ok || len(arr) != 4 {
		t.Fatalf("expected [a b c d], got %v", val)
	}
}

func TestJsonArrAppendNonexistentKey(t *testing.T) {
	db := New()
	err := db.JsonArrAppend("nonexistent", "arr", "x")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestJsonObjLen(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{"a": "1", "b": "2", "c": "3"})

	n, err := db.JsonObjLen("doc", "")
	if err != nil || n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestJsonDelAll(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{"a": "1"})
	db.JsonDel("doc", "")
	if db.Exists("doc") {
		t.Fatal("doc should be deleted")
	}
}

// ============================================================================
// HyperLogLog Extended
// ============================================================================

func TestPFCountEmpty(t *testing.T) {
	db := New()
	count := db.PFCount("hll")
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

// ============================================================================
// Graph
// ============================================================================

func TestGraphQuery(t *testing.T) {
	db := New()
	results, err := db.GraphQuery("g", "MATCH (n)-[e]->(m) RETURN n,m")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	_ = results
}

func TestGraphQueryNonexistent(t *testing.T) {
	db := New()
	results, err := db.GraphQuery("nonexistent", "MATCH (n)-[e]->(m) RETURN n,m")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if results != nil {
		t.Fatal("expected nil")
	}
}

// ============================================================================
// Search
// ============================================================================

func TestFTSearch(t *testing.T) {
	db := New()
	db.FTCreate("idx", map[string]string{"title": "TEXT", "body": "TEXT"})
	db.FTAdd("idx", "doc1", map[string]string{"title": "hello world", "body": "lorem ipsum"})
	db.FTAdd("idx", "doc2", map[string]string{"title": "goodbye", "body": "dolor sit"})

	results := db.FTSearch("idx", "hello", 10)
	if results.Len() == 0 {
		t.Fatal("expected search results")
	}
	results.Close()
}

func TestFTSearchNoMatch(t *testing.T) {
	db := New()
	db.FTCreate("idx", map[string]string{"title": "TEXT"})
	db.FTAdd("idx", "doc1", map[string]string{"title": "hello"})

	results := db.FTSearch("idx", "nonexistent", 10)
	if results.Len() != 0 {
		t.Fatal("expected empty results")
	}
	results.Close()
}

// ============================================================================
// ZSlices Extended
// ============================================================================

func TestZSlicesLen(t *testing.T) {
	zs := NewZSlices()
	if zs.Len() != 0 {
		t.Fatal("expected 0")
	}
	zs.Add([]byte("hello"))
	zs.Finish()
	if zs.Len() != 1 {
		t.Fatal("expected 1")
	}
	zs.Close()
}

func TestZSlicesGet(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("a"))
	zs.Add([]byte("b"))
	zs.Finish()

	if string(zs.Get(0)) != "a" || string(zs.Get(1)) != "b" {
		t.Fatal("unexpected get")
	}
	zs.Close()
}

func TestZSlicesGetOutOfRange(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("a"))
	if zs.Get(10) != nil {
		t.Fatal("expected nil")
	}
	zs.Close()
}

func TestZSlicesBytes(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("hello"))
	zs.Add([]byte("world"))
	zs.Finish()

	b := zs.Bytes()
	if b == nil {
		t.Fatal("expected non-nil bytes")
	}
	zs.Close()
}

// ============================================================================
// Ziplist Edge Cases
// ============================================================================

func TestZiplistEdgeCases(t *testing.T) {
	db := New()

	db.LPush("l", []byte("a"))
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))

	v, _ := db.HGet("h", "f1")
	if v.String() != "v1" {
		t.Fatalf("expected 'v1', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestDelNonexistent(t *testing.T) {
	db := New()
	if db.Del("nonexistent") {
		t.Fatal("expected false")
	}
}

func TestFlushAllMultiple(t *testing.T) {
	db := New()
	db.Set("k1", []byte("v1"))
	db.Set("k2", []byte("v2"))
	db.FlushAll()

	if db.Exists("k1") || db.Exists("k2") {
		t.Fatal("all keys should be gone")
	}

	db.Set("k3", []byte("v3"))
	if !db.Exists("k3") {
		t.Fatal("new key should exist after flush")
	}
}

func TestSetOverwrite(t *testing.T) {
	db := New()
	db.Set("key", []byte("first"))
	db.Set("key", []byte("second"))
	v, _ := db.Get("key")
	if v.String() != "second" {
		t.Fatalf("expected 'second', got '%s'", v.String())
	}
	v.Close()
}

func TestGetWrongType(t *testing.T) {
	db := New()
	db.LPush("list", []byte("a"))
	_, ok := db.Get("list")
	if ok {
		t.Fatal("expected not ok for wrong type")
	}
}

func TestZRemNonexistent(t *testing.T) {
	db := New()
	if db.ZRem("nonexistent", []byte("a")) {
		t.Fatal("expected false")
	}
}

func TestSIsMemberNonexistent(t *testing.T) {
	db := New()
	if db.SIsMember("nonexistent", []byte("a")) {
		t.Fatal("expected false")
	}
}

func TestSRemNonexistentKey(t *testing.T) {
	db := New()
	removed := db.SRem("nonexistent", []byte("a"))
	if removed != 0 {
		t.Fatal("expected 0")
	}
}

// ============================================================================
// Object Refcount
// ============================================================================

func TestFreeObjectRefcount(t *testing.T) {
	db := New()
	headOff := db.NewObject(ObjString, ObjEncodingRaw, 0)
	db.IncrRefcount(headOff)
	db.FreeObject(headOff)
}

func TestDecrRefcountZero(t *testing.T) {
	db := New()
	headOff := db.NewObject(ObjString, ObjEncodingRaw, 0)
	rc := db.DecrRefcount(headOff)
	if rc != 0 {
		t.Fatalf("expected 0, got %d", rc)
	}
	db.ObjectRefcount(headOff)
}

// ============================================================================
// ZSet Iteration & HyperLogLog Extended
// ============================================================================

func TestZRangeIter(t *testing.T) {
	db := New()
	for i := 0; i < 10; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}

	var members []string
	db.ZRangeIter("z", 0, -1, func(member []byte) {
		members = append(members, string(member))
	})
	if len(members) != 10 {
		t.Fatalf("expected 10, got %d", len(members))
	}

	members = nil
	db.ZRangeIter("z", -3, -1, func(member []byte) {
		members = append(members, string(member))
	})
	if len(members) != 3 {
		t.Fatalf("expected 3, got %d", len(members))
	}
}

func TestZRangeIterNonexistent(t *testing.T) {
	db := New()
	db.ZRangeIter("nonexistent", 0, -1, func(member []byte) {
		t.Fatal("should not be called")
	})
}

func TestZRangeIterEmptyRange(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	var called bool
	db.ZRangeIter("z", 5, 10, func(member []byte) {
		called = true
	})
	if called {
		t.Fatal("should not be called for empty range")
	}
}

func TestPFMerge(t *testing.T) {
	db := New()
	db.PFAdd("hll1", []byte("a"), []byte("b"), []byte("c"))
	db.PFAdd("hll2", []byte("c"), []byte("d"), []byte("e"))

	db.PFMerge("merged", "hll1", "hll2")
	count := db.PFCount("merged")
	if count <= 0 {
		t.Fatal("expected positive count")
	}
}

func TestPFMergeNonexistent(t *testing.T) {
	db := New()
	db.PFMerge("merged", "nonexistent")
	count := db.PFCount("merged")
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestPFCountWithData(t *testing.T) {
	db := New()
	db.PFAdd("hll", []byte("a"))
	if db.PFCount("hll") <= 0 {
		t.Fatal("expected positive count")
	}
}

// ============================================================================
// Bloom/Cuckoo Overwrite & Kick
// ============================================================================

func TestBFReserveOverwrite(t *testing.T) {
	db := New()
	db.BFReserve("bf", 0.01, 100)
	db.BFReserve("bf", 0.001, 5000)
	db.BFAdd("bf", []byte("x"))
	if !db.BFExists("bf", []byte("x")) {
		t.Fatal("expected x to exist")
	}
}

func TestCFReserveOverwrite(t *testing.T) {
	db := New()
	db.CFReserve("cf", 100)
	db.CFReserve("cf", 500)
	db.CFAdd("cf", []byte("x"))
	if !db.CFExists("cf", []byte("x")) {
		t.Fatal("expected x to exist")
	}
}

func TestCFDel(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1024)
	db.CFAdd("cf", []byte("x"))
	if !db.CFDel("cf", []byte("x")) {
		t.Fatal("expected delete to succeed")
	}
	if db.CFExists("cf", []byte("x")) {
		t.Fatal("x should be deleted")
	}
}

func TestCFDelNonexistent(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1024)
	if db.CFDel("cf", []byte("x")) {
		t.Fatal("expected false for nonexistent item")
	}
}

func TestCFAddMany(t *testing.T) {
	db := New()
	db.CFReserve("cf", 32)
	for i := 0; i < 50; i++ {
		db.CFAdd("cf", []byte(fmt.Sprintf("item%d", i)))
	}
	if !db.CFExists("cf", []byte("item0")) {
		t.Fatal("item0 should exist")
	}
}

// ============================================================================
// PoolBuffer ZSlices Edge
// ============================================================================

func TestZSlicesCloseWithoutFinish(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("test"))
	zs.Close()
}

// ============================================================================
// AppendBuffer Edge
// ============================================================================

func TestAppendBufferNew(t *testing.T) {
	db := New()
	pb := Buf("hello")
	n := db.AppendBuffer("newkey", pb)
	pb.Close()
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

// ============================================================================
// PoolBuffer Write
// ============================================================================

func TestPoolBufferWrite(t *testing.T) {
	pb := Buf("")
	pb.Write([]byte("hello"))
	if pb.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s'", pb.String())
	}
	pb.Close()
}

func TestPoolBufferWriteByte(t *testing.T) {
	pb := Buf("")
	pb.WriteByte('x')
	if pb.String() != "x" {
		t.Fatalf("expected 'x', got '%s'", pb.String())
	}
	pb.Close()
}

func TestPoolBufferWriteString(t *testing.T) {
	pb := Buf("")
	pb.WriteString("test")
	if pb.String() != "test" {
		t.Fatalf("expected 'test', got '%s'", pb.String())
	}
	pb.Close()
}

// ============================================================================
// SetRange Buffer
// ============================================================================

func TestSetRangeBuffer(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))

	pb := Buf("WORLD")
	n := db.SetRangeBuffer("key", 6, pb)
	pb.Close()
	if n != 11 {
		t.Fatalf("expected 11, got %d", n)
	}
}

// ============================================================================
// IntSet
// ============================================================================

func TestIntSetAdd(t *testing.T) {
	db := New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], 42)
	db.SAdd("is", buf[:])

	binary.LittleEndian.PutUint64(buf[:], 42)
	n := db.SAdd("is", buf[:])
	if n != 0 {
		t.Fatalf("expected 0 (duplicate), got %d", n)
	}

	binary.LittleEndian.PutUint64(buf[:], 100)
	n = db.SAdd("is", buf[:])
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestIntSetRemove(t *testing.T) {
	db := New()
	var buf [8]byte
	for i := 0; i < 5; i++ {
		binary.LittleEndian.PutUint64(buf[:], uint64(i))
		db.SAdd("is", buf[:])
	}

	binary.LittleEndian.PutUint64(buf[:], 3)
	removed := db.SRem("is", buf[:])
	if removed != 1 {
		t.Fatalf("expected 1, got %d", removed)
	}
}

func TestIntSetMember(t *testing.T) {
	db := New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], 42)
	db.SAdd("is", buf[:])

	binary.LittleEndian.PutUint64(buf[:], 42)
	if !db.SIsMember("is", buf[:]) {
		t.Fatal("expected member")
	}

	binary.LittleEndian.PutUint64(buf[:], 99)
	if db.SIsMember("is", buf[:]) {
		t.Fatal("expected not member")
	}
}

func TestIntSetMembers(t *testing.T) {
	db := New()
	var buf [8]byte
	for i := 0; i < 5; i++ {
		binary.LittleEndian.PutUint64(buf[:], uint64(i))
		db.SAdd("is", buf[:])
	}

	result := db.SMembers("is")
	if result == nil || result.Len() != 5 {
		t.Fatalf("expected 5, got %v", result)
	}
	result.Close()
}

// ============================================================================
// Search Intersection
// ============================================================================

func TestFTSearchIntersection(t *testing.T) {
	db := New()
	db.FTCreate("idx", map[string]string{"title": "TEXT", "body": "TEXT"})
	db.FTAdd("idx", "doc1", map[string]string{"title": "hello", "body": "world"})
	db.FTAdd("idx", "doc2", map[string]string{"title": "hello", "body": "test"})

	results := db.FTSearch("idx", "hello world", 10)
	if results.Len() == 0 {
		t.Fatal("expected results")
	}
	results.Close()
}

// ============================================================================
// BitField Tests
// ============================================================================

func TestBitFieldGet(t *testing.T) {
	db := New()
	db.SetBit("bf", 0, 1)
	db.SetBit("bf", 7, 1)

	results := db.BitField("bf",
		Buf("GET"), Buf("u8"), Buf("0"),
	)
	if len(results) != 1 || results[0] != 129 {
		t.Fatalf("unexpected results: %v", results)
	}

	results = db.BitField("bf",
		Buf("GET"), Buf("u8"), Buf("8"),
	)
	if len(results) != 1 || results[0] != 0 {
		t.Fatalf("expected [0], got %v", results)
	}
}

func TestBitFieldSet(t *testing.T) {
	db := New()
	results := db.BitField("bf",
		Buf("SET"), Buf("u8"), Buf("0"), Buf("42"),
		Buf("GET"), Buf("u8"), Buf("0"),
	)
	if len(results) != 2 || results[0] != 0 || results[1] != 42 {
		t.Fatalf("unexpected results: %v", results)
	}
}

func TestBitFieldIncrBy(t *testing.T) {
	db := New()
	results := db.BitField("bf",
		Buf("INCRBY"), Buf("i8"), Buf("0"), Buf("5"),
		Buf("INCRBY"), Buf("i8"), Buf("0"), Buf("-2"),
	)
	if len(results) != 2 || results[0] != 5 || results[1] != 3 {
		t.Fatalf("unexpected results: %v", results)
	}
}

func TestBitFieldSigned(t *testing.T) {
	db := New()
	db.SetBit("bf", 0, 1)

	results := db.BitField("bf",
		Buf("GET"), Buf("i8"), Buf("0"),
	)
	if len(results) != 1 || results[0] != -128 {
		t.Fatalf("expected -128, got %v", results)
	}
}

func TestBitFieldNewKey(t *testing.T) {
	db := New()
	results := db.BitField("newkey",
		Buf("SET"), Buf("u16"), Buf("0"), Buf("999"),
	)
	if len(results) != 1 || results[0] != 0 {
		t.Fatalf("expected [0], got %v", results)
	}
	if db.GetBit("newkey", 8) != 1 {
		t.Fatal("expected bit 8 set")
	}
}

func TestBitFieldWide(t *testing.T) {
	db := New()
	results := db.BitField("bf",
		Buf("SET"), Buf("u32"), Buf("32"), Buf("123456"),
		Buf("GET"), Buf("u32"), Buf("32"),
	)
	if results[1] != 123456 {
		t.Fatalf("expected 123456, got %v", results)
	}
}

func TestBitFieldBadArgs(t *testing.T) {
	db := New()
	results := db.BitField("bf",
		Buf("UNKNOWN"), Buf("u8"), Buf("0"),
	)
	if len(results) != 0 {
		t.Fatalf("expected empty, got %v", results)
	}
}

// ============================================================================
// Graph
// ============================================================================

func TestGraphAddNode(t *testing.T) {
	db := New()
	db.graphAddNode("g", "n1", []string{"Person"}, map[string]string{"name": "Alice"})

	if !db.Exists("g:node:n1") {
		t.Fatal("node key should exist")
	}
}

func TestGraphAddEdge(t *testing.T) {
	db := New()
	db.graphAddNode("g", "n1", []string{"Person"}, nil)
	db.graphAddNode("g", "n2", []string{"Person"}, nil)
	db.graphAddEdge("g", "e1", "KNOWS", "n1", "n2", map[string]string{"since": "2024"})

	if !db.Exists("g:edge:e1") {
		t.Fatal("edge key should exist")
	}
}

func TestGraphQueryWithNodes(t *testing.T) {
	db := New()
	_, err := db.GraphQuery("g", "MATCH (n:Person) RETURN n")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
}

// ============================================================================
// TimeSeries
// ============================================================================

func TestTSDel(t *testing.T) {
	db := New()
	for i := int64(0); i < 10; i++ {
		db.TSAdd("ts", i, float64(i))
	}

	deleted := db.TSDel("ts", 3, 6)
	if deleted != 4 {
		t.Fatalf("expected 4 deleted, got %d", deleted)
	}

	points := db.TSRange("ts", 0, 100)
	if len(points) != 6 {
		t.Fatalf("expected 6 remaining, got %d", len(points))
	}
}

func TestTSDelNonexistent(t *testing.T) {
	db := New()
	deleted := db.TSDel("nonexistent", 0, 100)
	if deleted != 0 {
		t.Fatalf("expected 0, got %d", deleted)
	}
}

func TestTSLast(t *testing.T) {
	db := New()
	db.TSAdd("ts", 100, 1.0)
	db.TSAdd("ts", 200, 2.0)

	timestamp, val, ok := db.TSLast("ts")
	if !ok || timestamp != 200 || val != 2.0 {
		t.Fatalf("expected (200, 2.0, true), got (%d, %f, %v)", timestamp, val, ok)
	}
}

func TestTSLastNonexistent(t *testing.T) {
	db := New()
	_, _, ok := db.TSLast("nonexistent")
	if ok {
		t.Fatal("expected false")
	}
}

// ============================================================================
// Cell / Throttle
// ============================================================================

func TestThrottleBurst(t *testing.T) {
	db := New()
	for i := 0; i < 10; i++ {
		db.Throttle("c", 10, 10, 1000)
	}
	r := db.Throttle("c", 10, 10, 1000)
	if r.Allowed {
		t.Fatal("expected throttle to kick in")
	}
}

func TestCellReset(t *testing.T) {
	db := New()
	db.Throttle("c", 1, 1, 1000)
	_ = db.Throttle("c", 1, 1, 1000)
	r := db.Throttle("c", 1, 1, 1000)
	if r.Allowed {
		t.Fatal("expected throttle to kick in")
	}
	db.CellReset("c")
	r = db.Throttle("c", 1, 1, 1000)
	if !r.Allowed {
		t.Fatal("expected throttle to pass after reset")
	}
}

// ============================================================================
// HyperLogLog
// ============================================================================

func TestPFCountMany(t *testing.T) {
	db := New()
	items := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		items[i] = []byte(fmt.Sprintf("item%d", i))
	}
	db.PFAdd("hll", items...)

	count := db.PFCount("hll")
	if count < 800 || count > 1200 {
		t.Fatalf("expected ~1000, got %d", count)
	}
}

// ============================================================================
// Arena
// ============================================================================

func TestArenaAllocString(t *testing.T) {
	a := NewArena(256)
	off := a.AllocString("hello")
	if off == 0 {
		t.Fatal("expected non-zero offset")
	}
	s := a.ReadString(off)
	if s != "hello" {
		t.Fatalf("expected 'hello', got '%s'", s)
	}
}

func TestArenaReadString(t *testing.T) {
	a := NewArena(256)
	off := a.AllocString("test")
	s := a.ReadString(off)
	if s != "test" {
		t.Fatalf("expected 'test', got '%s'", s)
	}
}

func TestArenaFreeZero(t *testing.T) {
	a := NewArena(256)
	a.Free(0)
}

func TestArenaReadBytes(t *testing.T) {
	a := NewArena(256)
	off := a.AllocString("data")
	b := a.ReadBytes(off, 4)
	if string(b) != "data" {
		t.Fatalf("expected 'data', got '%s'", string(b))
	}

	b = a.ReadBytes(off, 0)
	if len(b) != 0 {
		t.Fatal("expected empty")
	}
}

// ============================================================================
// Ziplist
// ============================================================================

func TestZiplistFind(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))

	v, ok := db.HGet("h", "f2")
	if !ok || v.String() != "v2" {
		t.Fatalf("expected 'v2', got '%s'", v.String())
	}
	v.Close()
}

func TestZiplistLargeValue(t *testing.T) {
	large := make([]byte, 100)
	for i := range large {
		large[i] = 'x'
	}
	db := New()
	db.HSet("h", "f1", large)
	v, ok := db.HGet("h", "f1")
	if !ok || len(v.Bytes()) != 100 {
		t.Fatalf("expected len 100, got %d", len(v.Bytes()))
	}
	v.Close()
}

func TestZiplistWriteEntryInt(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("a"))
	db.HIncrBy("h", "count", 42)
	val, _ := db.HGet("h", "count")
	if val.String() != "42" {
		t.Fatalf("expected '42', got '%s'", val.String())
	}
	val.Close()
}

func TestZiplistManyFields(t *testing.T) {
	db := New()
	for i := 0; i < 500; i++ {
		pb := Buf(fmt.Sprintf("v%d", i))
		db.HSetBuffer("big", fmt.Sprintf("f%d", i), pb)
		pb.Close()
	}

	all := db.HGetAll("big")
	if all.Len() != 500*2 {
		t.Fatalf("expected %d, got %d", 500*2, all.Len())
	}
	all.Close()
}

// ============================================================================
// Stream parseStreamID edge cases
// ============================================================================

func TestStreamParseStreamID(t *testing.T) {
	db := New()
	id := db.XAdd("s", "100-5", map[string]*PooledBuffer{"f": Buf("v")})
	if id != "100-5" {
		t.Fatalf("expected '100-5', got '%s'", id)
	}

	db.XAdd("s", "100-10", map[string]*PooledBuffer{"f": Buf("v")})
	id = db.XAdd("s", "100-5", map[string]*PooledBuffer{"f": Buf("v")})
	if id != "100-11" {
		t.Fatalf("expected '100-11', got '%s'", id)
	}
}

// ============================================================================
// JSON path edge cases
// ============================================================================

func TestJSONNestedPath(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{
		"user": map[string]interface{}{"name": "Alice", "age": float64(30)},
	})

	val, err := db.JsonGet("doc", "user.name")
	if err != nil || val != "Alice" {
		t.Fatalf("expected 'Alice', got %v, err=%v", val, err)
	}
}

func TestJSONNestedSet(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{
		"user": map[string]interface{}{"name": "Alice"},
	})
	db.JsonSet("doc", "user.age", float64(25))
	val, _ := db.JsonGet("doc", "user.age")
	if val != float64(25) {
		t.Fatalf("expected 25, got %v", val)
	}
}

// ============================================================================
// GeoDist edge cases
// ============================================================================

func TestGeoDistSamePoint(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "A")
	db.GeoAdd("g", 13.361389, 38.115556, "B")

	d := db.GeoDist("g", "A", "B", "m")
	if d != 0 {
		t.Fatalf("expected 0, got %f", d)
	}
}

func TestGeoDistInches(t *testing.T) {
	db := New()
	db.GeoAdd("g", 0, 0, "A")
	db.GeoAdd("g", 0, 1, "B")

	d := db.GeoDist("g", "A", "B", "in")
	if d <= 0 {
		t.Fatalf("expected positive distance, got %f", d)
	}
}

// ============================================================================
// More Bit Operations
// ============================================================================

func TestBitOpAND(t *testing.T) {
	db := New()
	db.SetBit("k1", 0, 1)
	db.SetBit("k2", 0, 1)
	db.SetBit("k2", 1, 1)

	n := db.BitOp("AND", "dest", "k1", "k2")
	if n < 1 {
		t.Fatalf("expected dest len >= 1, got %d", n)
	}
	if db.GetBit("dest", 0) != 1 {
		t.Fatal("bit 0 should be set")
	}
	if db.GetBit("dest", 1) != 0 {
		t.Fatal("bit 1 should not be set")
	}
}

func TestBitOpXOR(t *testing.T) {
	db := New()
	db.SetBit("k1", 0, 1)
	db.SetBit("k1", 1, 1)
	db.SetBit("k2", 1, 1)

	n := db.BitOp("XOR", "dest", "k1", "k2")
	if n < 1 {
		t.Fatalf("expected dest len >= 1, got %d", n)
	}
	if db.GetBit("dest", 0) != 1 {
		t.Fatal("bit 0 should be set (1 XOR 0 = 1)")
	}
}

// ============================================================================
// SetBit / GetBit Edge Cases
// ============================================================================

func TestSetBitLarge(t *testing.T) {
	db := New()
	val := db.SetBit("bf", 10000, 1)
	if val != 0 {
		t.Fatal("expected 0 for new bit")
	}
	if db.GetBit("bf", 10000) != 1 {
		t.Fatal("expected bit set")
	}
}

func TestBitCountRange(t *testing.T) {
	db := New()
	for i := 0; i < 8; i++ {
		db.SetBit("bf", i, 1)
	}
	count := db.BitCount("bf", 0, 0)
	if count != 8 {
		t.Fatalf("expected 8, got %d", count)
	}
}

// ============================================================================
// ZAdd / ZRem Edge Cases
// ============================================================================

func TestZAddMultipleBuffers(t *testing.T) {
	db := New()
	db.ZAddBuffer("z", 1.0, BufFromBytes([]byte{1, 0, 0, 0, 0, 0, 0, 0}))
	db.ZAddBuffer("z", 2.0, BufFromBytes([]byte{2, 0, 0, 0, 0, 0, 0, 0}))

	if db.ZCard("z") != 2 {
		t.Fatalf("expected 2, got %d", db.ZCard("z"))
	}
}

func TestZRemAll(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZRem("z", []byte("a"))

	if db.ZCard("z") != 0 {
		t.Fatal("zset should be empty")
	}
}

// ============================================================================
// Ziplist large encoding edge cases
// ============================================================================

func TestZiplistLargePrevLen(t *testing.T) {
	db := New()
	for i := 0; i < 200; i++ {
		pb := Buf(fmt.Sprintf("val%d", i))
		db.HSetBuffer("h", fmt.Sprintf("f%d", i), pb)
		pb.Close()
	}

	v, ok := db.HGet("h", "f0")
	if !ok {
		t.Fatal("expected field to exist")
	}
	v.Close()

	v, ok = db.HGet("h", "f199")
	if !ok {
		t.Fatal("expected field to exist")
	}
	v.Close()
}

func TestZiplistVeryLargeData(t *testing.T) {
	large := make([]byte, 20000)
	for i := range large {
		large[i] = byte('a' + i%26)
	}
	db := New()
	db.HSet("h", "big", large)

	v, ok := db.HGet("h", "big")
	if !ok || len(v.Bytes()) != 20000 {
		t.Fatalf("expected len 20000, got %d", len(v.Bytes()))
	}
	v.Close()
}

func TestZiplistMediumData(t *testing.T) {
	medium := make([]byte, 1000)
	for i := range medium {
		medium[i] = byte('a' + i%26)
	}
	db := New()
	db.HSet("h", "med", medium)

	v, ok := db.HGet("h", "med")
	if !ok || len(v.Bytes()) != 1000 {
		t.Fatalf("expected len 1000, got %d", len(v.Bytes()))
	}
	v.Close()
}

// ============================================================================
// SetBit / GetBit additional edge cases
// ============================================================================

func TestGetBitLarge(t *testing.T) {
	db := New()
	if db.GetBit("nonexistent", 1000) != 0 {
		t.Fatal("expected 0 for nonexistent")
	}
	db.SetBit("bf", 1000, 1)
	if db.GetBit("bf", 1000) != 1 {
		t.Fatal("expected 1")
	}
	db.SetBit("bf", 1000, 0)
	if db.GetBit("bf", 1000) != 0 {
		t.Fatal("expected 0 after clear")
	}
}

func TestBitOpNOT(t *testing.T) {
	db := New()
	db.SetBit("k1", 0, 1)
	db.SetBit("k1", 7, 1)

	n := db.BitOp("NOT", "dest", "k1")
	if n < 1 {
		t.Fatalf("expected dest len >= 1, got %d", n)
	}
	if db.GetBit("dest", 0) != 0 {
		t.Fatal("bit 0 should be 0")
	}
	if db.GetBit("dest", 1) != 1 {
		t.Fatal("bit 1 should be 1")
	}
}

func TestBitOpOR(t *testing.T) {
	db := New()
	db.SetBit("k1", 0, 1)
	db.SetBit("k2", 1, 1)

	n := db.BitOp("OR", "dest", "k1", "k2")
	if n < 1 {
		t.Fatalf("expected dest len >= 1, got %d", n)
	}
	if db.GetBit("dest", 0) != 1 {
		t.Fatal("bit 0 should be 1")
	}
	if db.GetBit("dest", 1) != 1 {
		t.Fatal("bit 1 should be 1")
	}
}

func TestBitOpBadOp(t *testing.T) {
	db := New()
	db.SetBit("k1", 0, 1)
	n := db.BitOp("INVALID", "dest", "k1")
	if n < 0 {
		t.Fatalf("unexpected negative: %d", n)
	}
}

func TestBitCountByteRange(t *testing.T) {
	db := New()
	for i := 0; i < 8; i++ {
		db.SetBit("bf", i, 1)
	}
	for i := 8; i < 16; i++ {
		db.SetBit("bf", i, 1)
	}
	count := db.BitCount("bf", 0, 0)
	if count != 8 {
		t.Fatalf("expected 8 in first byte, got %d", count)
	}
	count = db.BitCount("bf", 1, 1)
	if count != 8 {
		t.Fatalf("expected 8 in second byte, got %d", count)
	}
}

func TestSetBitUpdateExisting(t *testing.T) {
	db := New()
	old := db.SetBit("bf", 0, 1)
	if old != 0 {
		t.Fatal("expected old 0")
	}
	old = db.SetBit("bf", 0, 1)
	if old != 1 {
		t.Fatal("expected old 1")
	}
	old = db.SetBit("bf", 0, 0)
	if old != 1 {
		t.Fatal("expected old 1 when clearing")
	}
}

// ============================================================================
// Stream multi-add edge cases
// ============================================================================

func TestStreamMultiAdd(t *testing.T) {
	db := New()
	for i := 0; i < 100; i++ {
		id := db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
		if id == "" {
			t.Fatalf("expected non-empty id at iteration %d", i)
		}
	}
	if db.XLen("s") != 100 {
		t.Fatalf("expected 100, got %d", db.XLen("s"))
	}
}

// ============================================================================
// ZSet edge cases
// ============================================================================

func TestZRemRangeByScoreEmpty(t *testing.T) {
	db := New()
	n := db.ZRemRangeByScore("nonexistent", 0, 100)
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestZRemRangeByScoreAll(t *testing.T) {
	db := New()
	for i := 0; i < 5; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	n := db.ZRemRangeByScore("z", 0, 100)
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	if db.ZCard("z") != 0 {
		t.Fatal("zset should be empty")
	}
}

func TestZRangeWithScoresNegative(t *testing.T) {
	db := New()
	for i := 0; i < 5; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}
	zs, scores := db.ZRangeWithScores("z", -2, -1)
	if zs.Len() != 2 || scores[0] != 3 || scores[1] != 4 {
		t.Fatalf("expected [m3,m4] [3,4], got %v scores=%v", zs, scores)
	}
	zs.Close()
}

func TestZRangeWithScoresNonexistent(t *testing.T) {
	db := New()
	zs, scores := db.ZRangeWithScores("nonexistent", 0, -1)
	if zs != nil || scores != nil {
		t.Fatal("expected nil, nil")
	}
}

// ============================================================================
// JSON additional tests
// ============================================================================

func TestJSONArrayIndex(t *testing.T) {
	db := New()
	db.JsonSet("doc", "$", map[string]interface{}{"arr": []interface{}{"a", "b", "c"}})

	val, err := db.JsonGet("doc", "arr")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	arr, ok := val.([]interface{})
	if !ok || len(arr) != 3 {
		t.Fatalf("expected array of 3, got %v", val)
	}
}

// ============================================================================
// HIncrBy negative values
// ============================================================================

func TestHIncrByNegative(t *testing.T) {
	db := New()
	db.HSet("h", "count", []byte("10"))
	val, err := db.HIncrBy("h", "count", -5)
	if err != nil || val != 5 {
		t.Fatalf("expected 5, got %d, err=%v", val, err)
	}
}

// ============================================================================
// List mixed LPush and RPush
// ============================================================================

func TestListMixedPush(t *testing.T) {
	db := New()
	db.LPush("l", []byte("l1"), []byte("l2"))
	db.RPush("l", []byte("r1"), []byte("r2"))

	result := db.LRange("l", 0, -1)
	if result.Len() != 4 {
		t.Fatalf("expected 4, got %d", result.Len())
	}
	result.Close()
}

// ============================================================================
// GeoDist missing member
// ============================================================================

func TestGeoDistMissing(t *testing.T) {
	db := New()
	db.GeoAdd("g", 13.361389, 38.115556, "A")
	d := db.GeoDist("g", "A", "nonexistent", "m")
	if d != -1 {
		t.Fatalf("expected -1, got %f", d)
	}
}

// ============================================================================
// PoolBuffer WriteRune
// ============================================================================

func TestPoolBufferWriteRuneMulti(t *testing.T) {
	pb := Buf("")
	pb.WriteRune('世')
	pb.WriteRune('界')
	if pb.String() != "世界" {
		t.Fatalf("expected '世界', got '%s'", pb.String())
	}
	pb.Close()
}

// ============================================================================
// PFAdd with empty data
// ============================================================================

func TestPFAddEmpty(t *testing.T) {
	db := New()
	db.PFAdd("hll")
	count := db.PFCount("hll")
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

// ============================================================================
// GetRange edge cases
// ============================================================================

func TestGetRangeEndBeforeStart(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	_, ok := db.GetRange("key", 5, 0)
	if ok {
		t.Fatal("expected not found")
	}
}

// ============================================================================
// TopK additional
// ============================================================================

func TestTopKAddMoreThanK(t *testing.T) {
	db := New()
	db.TopKReserve("topk", 2)
	db.TopKAdd("topk", "a", "b", "c", "a", "b")
	items := db.TopKList("topk")
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
}

// ============================================================================
// Probabilistic edge cases
// ============================================================================

func TestCFReserveSmall(t *testing.T) {
	db := New()
	db.CFReserve("cf", 1)
	db.CFAdd("cf", []byte("x"))
	if !db.CFExists("cf", []byte("x")) {
		t.Fatal("expected x to exist")
	}
}

func TestBFAddMultiple(t *testing.T) {
	db := New()
	for i := 0; i < 100; i++ {
		db.BFAdd("bf", []byte(fmt.Sprintf("item%d", i)))
	}
	if !db.BFExists("bf", []byte("item0")) {
		t.Fatal("item0 should exist")
	}
}

func TestCMSIncrByMultiple(t *testing.T) {
	db := New()
	db.CMSInitByDim("cms", 100, 5)
	for i := 0; i < 10; i++ {
		db.CMSIncrBy("cms", []byte("a"), 1)
	}
	counts := db.CMSQuery("cms", []byte("a"))
	if counts[0] < 10 {
		t.Fatalf("expected >= 10, got %d", counts[0])
	}
}

// ============================================================================
// Append with int key (encoding=ObjEncodingInt)
// ============================================================================

func TestAppendToInt(t *testing.T) {
	db := New()
	db.IncrBy("n", 10)
	n := db.Append("n", []byte("0"))
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

// ============================================================================
// SRem multi
// ============================================================================

func TestSRemMulti(t *testing.T) {
	db := New()
	db.SAdd("s", []byte("a"), []byte("b"), []byte("c"), []byte("d"))
	removed := db.SRem("s", []byte("a"), []byte("c"), []byte("x"))
	if removed != 2 {
		t.Fatalf("expected 2, got %d", removed)
	}
}

// ============================================================================
// TSAdd chunk 满时创建新 chunk
// ============================================================================

func TestTSAddChunkFull(t *testing.T) {
	db := New()
	for i := 0; i < 300; i++ {
		db.TSAdd("ts", int64(i*10), float64(i))
	}
	points := db.TSRange("ts", 0, 3000)
	if len(points) != 300 {
		t.Fatalf("expected 300 points, got %d", len(points))
	}
}

func TestTSAddManyChunks(t *testing.T) {
	db := New()
	for i := 0; i < 1000; i++ {
		db.TSAdd("ts", int64(i), float64(i%100))
	}
	points := db.TSRange("ts", 0, 1000)
	if len(points) != 1000 {
		t.Fatalf("expected 1000 points, got %d", len(points))
	}
}

func TestTSAddWithLabels(t *testing.T) {
	db := New()
	labels := map[string]string{"type": "sensor", "zone": "north"}
	n := db.TSAddWithLabels("ts", 100, 1.5, labels)
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	retrieved := db.TSGetLabels("ts")
	if retrieved == nil {
		t.Fatal("expected labels, got nil")
	}
	if retrieved["type"] != "sensor" || retrieved["zone"] != "north" {
		t.Fatalf("expected labels {type:sensor, zone:north}, got %v", retrieved)
	}
}

func TestTSAddWithLabelsExisting(t *testing.T) {
	db := New()
	db.TSAddWithLabels("ts", 100, 1.0, map[string]string{"type": "temp"})
	n := db.TSAddWithLabels("ts", 200, 2.0, map[string]string{"type": "humidity"})
	if n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}

	retrieved := db.TSGetLabels("ts")
	if retrieved["type"] != "temp" {
		t.Fatalf("expected temp, got %s", retrieved["type"])
	}
}

func TestTSAddWithLabelsMultiple(t *testing.T) {
	db := New()
	db.TSAddWithLabels("ts1", 100, 1.0, map[string]string{"sensor": "temp"})
	db.TSAddWithLabels("ts2", 100, 2.0, map[string]string{"sensor": "humidity"})

	labels1 := db.TSGetLabels("ts1")
	labels2 := db.TSGetLabels("ts2")

	if labels1["sensor"] != "temp" {
		t.Fatalf("expected temp, got %s", labels1["sensor"])
	}
	if labels2["sensor"] != "humidity" {
		t.Fatalf("expected humidity, got %s", labels2["sensor"])
	}
}

func TestTSGetLabelsNonexistent(t *testing.T) {
	db := New()
	labels := db.TSGetLabels("nonexistent")
	if labels != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestTSGetLabelsNoLabels(t *testing.T) {
	db := New()
	db.TSAdd("ts", 100, 1.0)
	labels := db.TSGetLabels("ts")
	if labels != nil && len(labels) > 0 {
		t.Fatalf("expected empty or nil labels, got %v", labels)
	}
}

// ============================================================================
// ZSlices 边界检查
// ============================================================================

func TestZSlicesLenEmpty(t *testing.T) {
	zs := NewZSlices()
	if zs.Len() != 0 {
		t.Fatalf("expected 0, got %d", zs.Len())
	}
	zs.Close()
}

func TestZSlicesGetNegative(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	zs := db.ZRange("z", 0, -1)
	if zs.Get(-1) != nil {
		t.Fatal("expected nil for negative index")
	}
	zs.Close()
}

func TestZSlicesGetOutOfBounds(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	zs := db.ZRange("z", 0, -1)
	if zs.Get(100) != nil {
		t.Fatal("expected nil for out of range")
	}
	zs.Close()
}

func TestZSlicesCloseNil(t *testing.T) {
	var zs *ZSlices
	if zs != nil {
		zs.Close()
	}
}

func TestZSlicesFinishEmpty(t *testing.T) {
	zs := NewZSlices()
	zs.Finish()
	if zs.Len() != 0 {
		t.Fatalf("expected 0, got %d", zs.Len())
	}
	zs.Close()
}

// ============================================================================
// Ziplist 大 prevLen 编码 (>254)
// ============================================================================

func TestZiplistLargePrevLenEncoding(t *testing.T) {
	db := New()
	largeValue := make([]byte, 300)
	for i := range largeValue {
		largeValue[i] = 'x'
	}
	db.HSet("h", "f1", largeValue)
	db.HSet("h", "f2", []byte("small"))

	v, ok := db.HGet("h", "f2")
	if !ok || v.String() != "small" {
		t.Fatalf("expected 'small', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// Ziplist 多字节长度编码 (64-16383)
// ============================================================================

func TestZiplistMediumLengthEncoding(t *testing.T) {
	db := New()
	mediumValue := make([]byte, 100)
	for i := range mediumValue {
		mediumValue[i] = 'y'
	}
	db.HSet("h", "f1", mediumValue)

	v, ok := db.HGet("h", "f1")
	if !ok || len(v.Bytes()) != 100 {
		t.Fatalf("expected len 100, got %d", len(v.Bytes()))
	}
	v.Close()
}

func TestZiplistLargeLengthEncoding(t *testing.T) {
	db := New()
	largeValue := make([]byte, 20000)
	for i := range largeValue {
		largeValue[i] = 'z'
	}
	db.HSet("h", "f1", largeValue)

	v, ok := db.HGet("h", "f1")
	if !ok || len(v.Bytes()) != 20000 {
		t.Fatalf("expected len 20000, got %d", len(v.Bytes()))
	}
	v.Close()
}

// ============================================================================
// Ziplist 整数编码读取
// ============================================================================

func TestZiplistIntEncoding(t *testing.T) {
	db := New()
	db.HSet("h", "count", []byte("123"))
	v, ok := db.HGet("h", "count")
	if !ok || v.String() != "123" {
		t.Fatalf("expected '123', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// ZRangeIter 遍历
// ============================================================================

func TestZRangeIterFull(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	count := 0
	db.ZRangeIter("z", 0, -1, func(member []byte) {
		count++
	})
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

func TestZRangeIterRange(t *testing.T) {
	db := New()
	for i := 0; i < 10; i++ {
		db.ZAdd("z", float64(i), []byte(fmt.Sprintf("m%d", i)))
	}

	count := 0
	db.ZRangeIter("z", 2, 5, func(member []byte) {
		count++
	})
	if count != 4 {
		t.Fatalf("expected 4, got %d", count)
	}
}

// ============================================================================
// IncrBy 溢出和错误处理
// ============================================================================

func TestIncrByFromString(t *testing.T) {
	db := New()
	db.Set("n", []byte("100"))
	val, err := db.IncrBy("n", 50)
	if err != nil || val != 150 {
		t.Fatalf("expected 150, got %d, err=%v", val, err)
	}
}

func TestIncrByInvalidString(t *testing.T) {
	db := New()
	db.Set("n", []byte("not_a_number"))
	_, err := db.IncrBy("n", 1)
	if err == nil {
		t.Fatal("expected error for invalid number")
	}
}

func TestIncrByFloatFromString(t *testing.T) {
	db := New()
	db.Set("n", []byte("10.5"))
	val, err := db.IncrByFloat("n", 5.5)
	if err != nil || val != 16.0 {
		t.Fatalf("expected 16.0, got %f, err=%v", val, err)
	}
}

// ============================================================================
// ZAddBuffer 边界
// ============================================================================

func TestZAddBufferNil(t *testing.T) {
	db := New()
	defer func() {
		if r := recover(); r == nil {
			if db.ZCard("z") != 0 {
				t.Fatal("expected 0 for nil buffer")
			}
		}
	}()
	db.ZAddBuffer("z", 1.0, nil)
}

func TestZAddBufferEmpty(t *testing.T) {
	db := New()
	pb := Buf("")
	db.ZAddBuffer("z", 1.0, pb)
	pb.Close()
	if db.ZCard("z") != 1 {
		t.Fatalf("expected 1, got %d", db.ZCard("z"))
	}
}

// ============================================================================
// ZRange 边界
// ============================================================================

func TestZRangeEmpty(t *testing.T) {
	db := New()
	zs := db.ZRange("nonexistent", 0, -1)
	if zs != nil && zs.Len() != 0 {
		t.Fatal("expected empty or nil")
	}
	if zs != nil {
		zs.Close()
	}
}

func TestZRangeSingleElement(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("only"))
	zs := db.ZRange("z", 0, -1)
	if zs.Len() != 1 {
		t.Fatalf("expected 1, got %d", zs.Len())
	}
	if string(zs.Get(0)) != "only" {
		t.Fatalf("expected 'only', got '%s'", string(zs.Get(0)))
	}
	zs.Close()
}

// ============================================================================
// ZRangeByScore 边界
// ============================================================================

func TestZRangeByScoreNoMatch(t *testing.T) {
	db := New()
	db.ZAdd("z", 100.0, []byte("a"))
	zs := db.ZRangeByScore("z", 0, 50)
	if zs == nil || zs.Len() != 0 {
		t.Fatalf("expected 0, got %d", zs.Len())
	}
	if zs != nil {
		zs.Close()
	}
}

func TestZRangeByScoreExactMatch(t *testing.T) {
	db := New()
	db.ZAdd("z", 50.0, []byte("a"))
	zs := db.ZRangeByScore("z", 50, 50)
	if zs == nil || zs.Len() != 1 {
		t.Fatalf("expected 1, got %d", zs.Len())
	}
	if zs != nil {
		zs.Close()
	}
}

// ============================================================================
// ZRemBuffer 边界
// ============================================================================

func TestZRemBufferNil(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	defer func() {
		if r := recover(); r == nil {
			if db.ZCard("z") != 1 {
				t.Fatal("expected 1 after nil buffer removal attempt")
			}
		}
	}()
	db.ZRemBuffer("z", nil)
}

func TestZRemBufferNonexistent(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	pb := Buf("nonexistent")
	removed := db.ZRemBuffer("z", pb)
	pb.Close()
	if removed {
		t.Fatal("expected false for nonexistent member")
	}
}

// ============================================================================
// ZRank / ZRevRank
// ============================================================================

func TestZRank(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	rank, ok := db.ZRank("z", []byte("a"))
	if !ok || rank != 0 {
		t.Fatalf("expected rank 0 for 'a', got %d, ok=%v", rank, ok)
	}

	rank, ok = db.ZRank("z", []byte("b"))
	if !ok || rank != 1 {
		t.Fatalf("expected rank 1 for 'b', got %d, ok=%v", rank, ok)
	}

	rank, ok = db.ZRank("z", []byte("c"))
	if !ok || rank != 2 {
		t.Fatalf("expected rank 2 for 'c', got %d, ok=%v", rank, ok)
	}

	_, ok = db.ZRank("z", []byte("nonexistent"))
	if ok {
		t.Fatal("expected not found for nonexistent member")
	}

	_, ok = db.ZRank("nonexistent", []byte("a"))
	if ok {
		t.Fatal("expected not found for nonexistent key")
	}
}

func TestZRevRank(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	rank, ok := db.ZRevRank("z", []byte("c"))
	if !ok || rank != 0 {
		t.Fatalf("expected rev rank 0 for 'c', got %d, ok=%v", rank, ok)
	}

	rank, ok = db.ZRevRank("z", []byte("a"))
	if !ok || rank != 2 {
		t.Fatalf("expected rev rank 2 for 'a', got %d, ok=%v", rank, ok)
	}
}

// ============================================================================
// ZIncrBy
// ============================================================================

func TestZIncrBy(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))

	newScore := db.ZIncrBy("z", []byte("a"), 2.0)
	if newScore != 3.0 {
		t.Fatalf("expected 3.0, got %f", newScore)
	}

	score, _ := db.ZScore("z", []byte("a"))
	if score != 3.0 {
		t.Fatalf("expected stored score 3.0, got %f", score)
	}
}

func TestZIncrByNewMember(t *testing.T) {
	db := New()
	newScore := db.ZIncrBy("z", []byte("new"), 5.0)
	if newScore != 5.0 {
		t.Fatalf("expected 5.0 for new member, got %f", newScore)
	}

	if db.ZCard("z") != 1 {
		t.Fatal("expected 1 member")
	}
}

func TestZIncrByNegativeDelta(t *testing.T) {
	db := New()
	db.ZAdd("z", 5.0, []byte("a"))

	newScore := db.ZIncrBy("z", []byte("a"), -3.0)
	if newScore != 2.0 {
		t.Fatalf("expected 2.0, got %f", newScore)
	}
}

// ============================================================================
// ZPopMin / ZPopMax
// ============================================================================

func TestZPopMin(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	zs := db.ZPopMin("z", 2)
	if zs.Len() != 2 {
		t.Fatalf("expected 2, got %d", zs.Len())
	}
	if string(zs.Get(0)) != "a" {
		t.Fatalf("expected first popped 'a', got '%s'", string(zs.Get(0)))
	}
	if string(zs.Get(1)) != "b" {
		t.Fatalf("expected second popped 'b', got '%s'", string(zs.Get(1)))
	}
	zs.Close()

	if db.ZCard("z") != 1 {
		t.Fatalf("expected 1 remaining, got %d", db.ZCard("z"))
	}
}

func TestZPopMax(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	zs := db.ZPopMax("z", 2)
	if zs.Len() != 2 {
		t.Fatalf("expected 2, got %d", zs.Len())
	}
	if string(zs.Get(0)) != "c" {
		t.Fatalf("expected first popped 'c', got '%s'", string(zs.Get(0)))
	}
	if string(zs.Get(1)) != "b" {
		t.Fatalf("expected second popped 'b', got '%s'", string(zs.Get(1)))
	}
	zs.Close()

	if db.ZCard("z") != 1 {
		t.Fatalf("expected 1 remaining, got %d", db.ZCard("z"))
	}
}

func TestZPopMinEmpty(t *testing.T) {
	db := New()
	zs := db.ZPopMin("nonexistent", 1)
	if zs != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestZPopMaxCountZero(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	zs := db.ZPopMax("z", 0)
	if zs.Len() != 0 {
		t.Fatalf("expected 0, got %d", zs.Len())
	}
	zs.Close()
	if db.ZCard("z") != 1 {
		t.Fatal("expected 1 remaining")
	}
}

// ============================================================================
// Stream parseStreamID 边界
// ============================================================================

func TestParseStreamIDZero(t *testing.T) {
	db := New()
	id := db.XAdd("s", "0", map[string]*PooledBuffer{"f": Buf("v")})
	if id == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestParseStreamIDZeroZero(t *testing.T) {
	db := New()
	id := db.XAdd("s", "0-0", map[string]*PooledBuffer{"f": Buf("v")})
	if id == "" {
		t.Fatal("expected non-empty id")
	}
}

// ============================================================================
// Dict 边界
// ============================================================================

func TestDictKeyEquals(t *testing.T) {
	db := New()
	db.Set("key1", []byte("val1"))
	db.Set("key1", []byte("val2"))

	v, ok := db.Get("key1")
	if !ok || v.String() != "val2" {
		t.Fatalf("expected 'val2', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// PoolBuffer WriteByte/ReadByte
// ============================================================================

func TestPoolBufferWriteByteReadByte(t *testing.T) {
	pb := Buf("")
	if err := pb.WriteByte('A'); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if err := pb.WriteByte('B'); err != nil {
		t.Fatalf("write error: %v", err)
	}
	b, err := pb.ReadByte()
	if err != nil || b != 'A' {
		t.Fatalf("expected 'A', got '%c', err=%v", b, err)
	}
	b, err = pb.ReadByte()
	if err != nil || b != 'B' {
		t.Fatalf("expected 'B', got '%c', err=%v", b, err)
	}
	pb.Close()
}

func TestPoolBufferReadByteEmpty(t *testing.T) {
	pb := Buf("")
	_, err := pb.ReadByte()
	if err == nil {
		t.Fatal("expected error for empty buffer")
	}
	pb.Close()
}

// ============================================================================
// GetRange 边界
// ============================================================================

func TestGetRangeFullString(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	v, ok := db.GetRange("key", 0, 4)
	if !ok || v.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s'", v.String())
	}
	v.Close()
}

func TestGetRangeSingleChar(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	v, ok := db.GetRange("key", 0, 0)
	if !ok || v.String() != "h" {
		t.Fatalf("expected 'h', got '%s'", v.String())
	}
	v.Close()
}

// ============================================================================
// SetRange 边界
// ============================================================================

func TestSetRangeExtend(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	n := db.SetRange("key", 10, []byte("world"))
	if n != 15 {
		t.Fatalf("expected 15, got %d", n)
	}
	v, _ := db.Get("key")
	expected := "hello" + strings.Repeat("\x00", 5) + "world"
	if v.String() != expected {
		t.Fatalf("expected '%s', got '%s'", expected, v.String())
	}
	v.Close()
}

// ============================================================================
// Strlen 边界
// ============================================================================

func TestStrlenNonexistent(t *testing.T) {
	db := New()
	n := db.Strlen("nonexistent")
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestStrlenEmpty(t *testing.T) {
	db := New()
	db.Set("key", []byte(""))
	n := db.Strlen("key")
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// ============================================================================
// Append 边界
// ============================================================================

func TestAppendToEmpty(t *testing.T) {
	db := New()
	db.Set("key", []byte(""))
	n := db.Append("key", []byte("hello"))
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestAppendBuffer(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	pb := Buf("world")
	n := db.AppendBuffer("key", pb)
	pb.Close()
	if n != 10 {
		t.Fatalf("expected 10, got %d", n)
	}
}

// ============================================================================
// String 新命令测试: SetEx, PsetEx, GetDel
// ============================================================================

func TestSetEx(t *testing.T) {
	db := New()
	db.SetEx("key", 10, []byte("hello"))
	val, ok := db.Get("key")
	if !ok || val.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestPsetEx(t *testing.T) {
	db := New()
	db.PsetEx("key", 10000, []byte("world"))
	val, ok := db.Get("key")
	if !ok || val.String() != "world" {
		t.Fatalf("expected 'world', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestGetDel(t *testing.T) {
	db := New()
	db.Set("key", []byte("hello"))
	val, ok := db.GetDel("key")
	if !ok || val.String() != "hello" {
		t.Fatalf("expected 'hello', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	val, ok = db.Get("key")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestGetDelNonExistent(t *testing.T) {
	db := New()
	val, ok := db.GetDel("nonexistent")
	if ok || val != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

// ============================================================================
// Bitmap 新命令测试: BitPos
// ============================================================================

func TestBitPos(t *testing.T) {
	db := New()
	db.SetBit("key", 0, 1)
	db.SetBit("key", 8, 1)

	pos := db.BitPos("key", 1, 0, -1)
	if pos != 0 {
		t.Fatalf("expected first 1 at pos 0, got %d", pos)
	}

	pos = db.BitPos("key", 1, 1, -1)
	if pos != 8 {
		t.Fatalf("expected first 1 at pos 8, got %d", pos)
	}

	pos = db.BitPos("key", 0, 0, 0)
	if pos != 1 {
		t.Fatalf("expected first 0 at pos 1 in byte 0, got %d", pos)
	}
}

func TestBitPosNonExistent(t *testing.T) {
	db := New()
	pos := db.BitPos("nonexistent", 0, 0, -1)
	if pos != 0 {
		t.Fatalf("expected 0 for nonexistent key with bit=0, got %d", pos)
	}

	pos = db.BitPos("nonexistent", 1, 0, -1)
	if pos != -1 {
		t.Fatalf("expected -1 for nonexistent key with bit=1, got %d", pos)
	}
}

func TestBitPosAllZeros(t *testing.T) {
	db := New()
	db.Set("key", []byte{0, 0, 0})

	pos := db.BitPos("key", 1, 0, -1)
	if pos != -1 {
		t.Fatalf("expected -1 for all zeros, got %d", pos)
	}
}

func TestBitPosAllOnes(t *testing.T) {
	db := New()
	db.Set("key", []byte{255, 255, 255})

	pos := db.BitPos("key", 1, 0, -1)
	if pos != 0 {
		t.Fatalf("expected first 1 at pos 0, got %d", pos)
	}
}

// ============================================================================
// Hash 新命令测试: HStrLen, HRandField
// ============================================================================

func TestHStrLen(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("hello"))
	db.HSet("h", "f2", []byte("world"))

	len := db.HStrLen("h", "f1")
	if len != 5 {
		t.Fatalf("expected len 5 for 'hello', got %d", len)
	}

	len = db.HStrLen("h", "f2")
	if len != 5 {
		t.Fatalf("expected len 5 for 'world', got %d", len)
	}

	len = db.HStrLen("h", "nonexistent")
	if len != 0 {
		t.Fatalf("expected len 0 for nonexistent field, got %d", len)
	}

	len = db.HStrLen("nonexistent", "f1")
	if len != 0 {
		t.Fatalf("expected len 0 for nonexistent key, got %d", len)
	}
}

func TestHRandField(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))
	db.HSet("h", "f3", []byte("v3"))

	fields := db.HRandField("h", 2)
	if fields == nil || len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %v", fields)
	}
	for _, f := range fields {
		f.Close()
	}
}

func TestHRandFieldNonExistent(t *testing.T) {
	db := New()
	fields := db.HRandField("nonexistent", 2)
	if fields != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestHRandFieldCountExceeds(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))

	fields := db.HRandField("h", 10)
	if fields == nil || len(fields) > 1 {
		t.Fatalf("expected at most 1 field, got %d", len(fields))
	}
	for _, f := range fields {
		f.Close()
	}
}

// ============================================================================
// List 新命令测试: RPopLPush, LInsert
// ============================================================================

func TestRPopLPush(t *testing.T) {
	db := New()
	db.RPush("src", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.RPopLPush("src", "dst")
	if !ok || val.String() != "c" {
		t.Fatalf("expected 'c', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	vals := db.LRange("src", 0, -1)
	if vals.Len() != 2 {
		t.Fatalf("expected 2 elements in src, got %d", vals.Len())
	}
	vals.Close()

	vals = db.LRange("dst", 0, -1)
	if vals.Len() != 1 {
		t.Fatalf("expected 1 element in dst, got %d", vals.Len())
	}
	vals.Close()
}

func TestRPopLPushSrcEmpty(t *testing.T) {
	db := New()
	db.RPush("dst", []byte("existing"))

	val, ok := db.RPopLPush("nonexistent", "dst")
	if ok || val != nil {
		t.Fatal("expected nil for empty source")
	}
}

func TestRPopLPushSameKey(t *testing.T) {
	db := New()
	db.RPush("list", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.RPopLPush("list", "list")
	if !ok || val.String() != "c" {
		t.Fatalf("expected 'c', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	vals := db.LRange("list", 0, -1)
	if vals.Len() != 3 {
		t.Fatalf("expected 3 elements, got %d", vals.Len())
	}
	vals.Close()
}

func TestLInsert(t *testing.T) {
	db := New()
	db.RPush("list", []byte("a"), []byte("c"))

	n := db.LInsert("list", "BEFORE", "c", []byte("b"))
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	vals := db.LRange("list", 0, -1)
	if vals.Len() != 3 {
		t.Fatalf("expected 3 elements, got %d", vals.Len())
	}
	vals.Close()
}

func TestLInsertAfter(t *testing.T) {
	db := New()
	db.RPush("list", []byte("a"), []byte("c"))

	n := db.LInsert("list", "AFTER", "a", []byte("b"))
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	vals := db.LRange("list", 0, -1)
	if vals.Len() != 3 {
		t.Fatalf("expected 3 elements, got %d", vals.Len())
	}
	vals.Close()
}

func TestLInsertNonExistent(t *testing.T) {
	db := New()
	n := db.LInsert("nonexistent", "BEFORE", "pivot", []byte("val"))
	if n != -1 {
		t.Fatalf("expected -1 for nonexistent key, got %d", n)
	}
}

func TestLInsertPivotNotFound(t *testing.T) {
	db := New()
	db.RPush("list", []byte("a"), []byte("c"))

	n := db.LInsert("list", "BEFORE", "nonexistent", []byte("b"))
	if n != -1 {
		t.Fatalf("expected -1 for pivot not found, got %d", n)
	}
}

// ============================================================================
// Set 新命令测试: SMove
// ============================================================================

func TestSMove(t *testing.T) {
	db := New()
	db.SAdd("src", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("dst", []byte("x"))

	ok := db.SMove("src", "dst", []byte("b"))
	if !ok {
		t.Fatal("expected SMove to succeed")
	}

	if !db.SIsMember("dst", []byte("b")) {
		t.Fatal("expected 'b' in dst")
	}

	if db.SIsMember("src", []byte("b")) {
		t.Fatal("expected 'b' removed from src")
	}
}

func TestSMoveToNonExistent(t *testing.T) {
	db := New()
	db.SAdd("src", []byte("a"), []byte("b"))

	ok := db.SMove("src", "dst", []byte("a"))
	if !ok {
		t.Fatal("expected SMove to succeed")
	}

	if !db.SIsMember("dst", []byte("a")) {
		t.Fatal("expected 'a' in dst")
	}
}

func TestSMoveNonExistent(t *testing.T) {
	db := New()
	ok := db.SMove("nonexistent", "dst", []byte("a"))
	if ok {
		t.Fatal("expected SMove to fail for nonexistent source")
	}
}

func TestSMoveMemberNotInSource(t *testing.T) {
	db := New()
	db.SAdd("src", []byte("a"))
	db.SAdd("dst", []byte("x"))

	ok := db.SMove("src", "dst", []byte("nonexistent"))
	if ok {
		t.Fatal("expected SMove to fail for member not in source")
	}
}

// ============================================================================
// ZSet 新命令测试: ZCount, ZLexCount, ZRangeByLex, BZMPop
// ============================================================================

func TestZCount(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))
	db.ZAdd("z", 4.0, []byte("d"))

	count := db.ZCount("z", 1.0, 3.0)
	if count != 3 {
		t.Fatalf("expected count 3, got %d", count)
	}

	count = db.ZCount("z", 2.5, 4.0)
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}

	count = db.ZCount("z", 10.0, 20.0)
	if count != 0 {
		t.Fatalf("expected count 0, got %d", count)
	}
}

func TestZCountNonExistent(t *testing.T) {
	db := New()
	count := db.ZCount("nonexistent", 1.0, 10.0)
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestZLexCount(t *testing.T) {
	db := New()
	db.ZAdd("z", 0.0, []byte("a"))
	db.ZAdd("z", 0.0, []byte("b"))
	db.ZAdd("z", 0.0, []byte("c"))
	db.ZAdd("z", 0.0, []byte("d"))

	count := db.ZLexCount("z", "-", "+")
	if count != 4 {
		t.Fatalf("expected count 4, got %d", count)
	}

	count = db.ZLexCount("z", "b", "d")
	if count != 3 {
		t.Fatalf("expected count 3, got %d", count)
	}

	count = db.ZLexCount("z", "(b", "d")
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}

func TestZLexCountNonExistent(t *testing.T) {
	db := New()
	count := db.ZLexCount("nonexistent", "-", "+")
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestZRangeByLex(t *testing.T) {
	db := New()
	db.ZAdd("z", 0.0, []byte("a"))
	db.ZAdd("z", 0.0, []byte("b"))
	db.ZAdd("z", 0.0, []byte("c"))
	db.ZAdd("z", 0.0, []byte("d"))

	result := db.ZRangeByLex("z", "b", "d")
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Len() != 3 {
		t.Fatalf("expected 3 elements, got %d", result.Len())
	}
	result.Close()
}

func TestZRangeByLexNonExistent(t *testing.T) {
	db := New()
	result := db.ZRangeByLex("nonexistent", "-", "+")
	if result != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestZRangeByLexExclusive(t *testing.T) {
	db := New()
	db.ZAdd("z", 0.0, []byte("a"))
	db.ZAdd("z", 0.0, []byte("b"))
	db.ZAdd("z", 0.0, []byte("c"))

	result := db.ZRangeByLex("z", "b", "+")
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Len() < 1 {
		t.Fatalf("expected at least 1 element, got %d", result.Len())
	}
	result.Close()
}

func TestBZMPop(t *testing.T) {
	db := New()
	db.ZAdd("z1", 1.0, []byte("a"))
	db.ZAdd("z1", 2.0, []byte("b"))
	db.ZAdd("z2", 1.0, []byte("c"))

	key, member, score, found := db.BZMPop(1, 2, []string{"z1", "z2"}, "MIN")
	if !found {
		t.Fatal("expected to find element")
	}
	if key != "z1" {
		t.Fatalf("expected key 'z1', got '%s'", key)
	}
	if member.String() != "a" {
		t.Fatalf("expected member 'a', got '%s'", member.String())
	}
	if score != 1.0 {
		t.Fatalf("expected score 1.0, got %f", score)
	}
	member.Close()
}

func TestBZMPopMax(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))
	db.ZAdd("z", 2.0, []byte("b"))
	db.ZAdd("z", 3.0, []byte("c"))

	_, member, score, found := db.BZMPop(1, 1, []string{"z"}, "MAX")
	if !found {
		t.Fatal("expected to find element")
	}
	if member.String() != "c" {
		t.Fatalf("expected member 'c', got '%s'", member.String())
	}
	if score != 3.0 {
		t.Fatalf("expected score 3.0, got %f", score)
	}
	member.Close()
}

func TestBZMPopNotFound(t *testing.T) {
	db := New()
	_, _, _, found := db.BZMPop(1, 1, []string{"nonexistent"}, "MIN")
	if found {
		t.Fatal("expected not found")
	}
}

// ============================================================================
// Stream 新命令测试: XInfo, XInfoGroups, XClaim, XAutoClaim, XPending,
//                   XGroupCreateConsumer, XGroupDelConsumer
// ============================================================================

func TestXInfo(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v2")})

	info := db.XInfo("s")
	if info == nil {
		t.Fatal("expected info, got nil")
	}
	if info["length"] != 2 {
		t.Fatalf("expected length 2, got %v", info["length"])
	}
	if info["groups"] != 0 {
		t.Fatalf("expected groups 0, got %v", info["groups"])
	}
}

func TestXInfoNonExistent(t *testing.T) {
	db := New()
	info := db.XInfo("nonexistent")
	if info != nil {
		t.Fatal("expected nil for nonexistent stream")
	}
}

func TestXInfoGroups(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g1", "0-0")
	db.XGroupCreate("s", "g2", "0-0")

	groups := db.XInfoGroups("s")
	if groups == nil {
		t.Fatal("expected groups, got nil")
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestXInfoGroupsNonExistent(t *testing.T) {
	db := New()
	groups := db.XInfoGroups("nonexistent")
	if groups != nil {
		t.Fatal("expected nil for nonexistent stream")
	}
}

func TestXClaim(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g", "0-0")

	entries := db.XClaim("s", "g", "c1", 0, []string{"1-0"})
	if entries == nil {
		t.Fatal("expected entries, got nil")
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestXClaimNonExistent(t *testing.T) {
	db := New()
	entries := db.XClaim("nonexistent", "g", "c", 0, []string{"1-0"})
	if entries != nil {
		t.Fatal("expected nil for nonexistent stream")
	}
}

func TestXClaimGroupNotFound(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})

	entries := db.XClaim("s", "nonexistent", "c", 0, []string{"1-0"})
	if entries != nil {
		t.Fatal("expected nil for nonexistent group")
	}
}

func TestXAutoClaim(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})
	db.XGroupCreate("s", "g", "0-0")

	nextID, entries := db.XAutoClaim("s", "g", "c1", "0-0", 10)
	if nextID == "" {
		t.Fatal("expected nextID, got empty")
	}
	if entries == nil {
		t.Fatal("expected entries, got nil")
	}
}

func TestXAutoClaimNonExistent(t *testing.T) {
	db := New()
	_, entries := db.XAutoClaim("nonexistent", "g", "c", "0-0", 10)
	if entries != nil {
		t.Fatal("expected nil for nonexistent stream")
	}
}

func TestXPending(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g", "0-0")

	pending := db.XPending("s", "g")
	if pending == nil {
		t.Fatal("expected pending info, got nil")
	}
}

func TestXPendingNonExistent(t *testing.T) {
	db := New()
	pending := db.XPending("nonexistent", "g")
	if pending != nil {
		t.Fatal("expected nil for nonexistent stream")
	}
}

func TestXGroupCreateConsumer(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g", "0-0")

	ok := db.XGroupCreateConsumer("s", "g", "c1")
	if !ok {
		t.Fatal("expected consumer creation success")
	}

	ok = db.XGroupCreateConsumer("s", "g", "c1")
	if !ok {
		t.Fatal("expected consumer to already exist (success)")
	}
}

func TestXGroupCreateConsumerNonExistent(t *testing.T) {
	db := New()
	ok := db.XGroupCreateConsumer("nonexistent", "g", "c")
	if ok {
		t.Fatal("expected failure for nonexistent stream")
	}
}

func TestXGroupCreateConsumerGroupNotFound(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})

	ok := db.XGroupCreateConsumer("s", "nonexistent", "c")
	if ok {
		t.Fatal("expected failure for nonexistent group")
	}
}

func TestXGroupDelConsumer(t *testing.T) {
	db := New()
	db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g", "0-0")
	db.XGroupCreateConsumer("s", "g", "c1")

	n := db.XGroupDelConsumer("s", "g", "c1")
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestXGroupDelConsumerNonExistent(t *testing.T) {
	db := New()
	n := db.XGroupDelConsumer("nonexistent", "g", "c")
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// ============================================================================
// String 新命令测试: MGet, MSet, SetNX, Decr
// ============================================================================

func TestMGet(t *testing.T) {
	db := New()
	db.Set("key1", []byte("val1"))
	db.Set("key2", []byte("val2"))

	results := db.MGet("key1", "key2", "nonexistent")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0] == nil || results[0].String() != "val1" {
		t.Fatalf("expected 'val1', got '%s'", results[0].String())
	}
	results[0].Close()

	if results[1] == nil || results[1].String() != "val2" {
		t.Fatalf("expected 'val2', got '%s'", results[1].String())
	}
	results[1].Close()

	if results[2] != nil {
		t.Fatalf("expected nil for nonexistent key, got '%s'", results[2].String())
	}
}

func TestMGetEmptyKeys(t *testing.T) {
	db := New()
	results := db.MGet()
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestMGetAllNonExistent(t *testing.T) {
	db := New()
	results := db.MGet("key1", "key2", "key3")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if r != nil {
			t.Fatalf("expected nil at index %d, got '%s'", i, r.String())
		}
	}
}

func TestMSet(t *testing.T) {
	db := New()
	db.MSet(map[string]*PooledBuffer{
		"key1": Buf("val1"),
		"key2": Buf("val2"),
		"key3": Buf("val3"),
	})

	val, ok := db.Get("key1")
	if !ok || val.String() != "val1" {
		t.Fatalf("expected 'val1', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	val, ok = db.Get("key2")
	if !ok || val.String() != "val2" {
		t.Fatalf("expected 'val2', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	val, ok = db.Get("key3")
	if !ok || val.String() != "val3" {
		t.Fatalf("expected 'val3', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestMSetOverwrite(t *testing.T) {
	db := New()
	db.Set("key1", []byte("original"))

	db.MSet(map[string]*PooledBuffer{
		"key1": Buf("updated"),
	})

	val, ok := db.Get("key1")
	if !ok || val.String() != "updated" {
		t.Fatalf("expected 'updated', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestMSetEmpty(t *testing.T) {
	db := New()
	db.MSet(map[string]*PooledBuffer{})
}

func TestSetNX(t *testing.T) {
	db := New()

	ok := db.SetNX("key", Buf("val"))
	if !ok {
		t.Fatal("expected SetNX to succeed for new key")
	}

	val, found := db.Get("key")
	if !found || val.String() != "val" {
		t.Fatalf("expected 'val', got '%s', found=%v", val.String(), found)
	}
	val.Close()
}

func TestSetNXExistingKey(t *testing.T) {
	db := New()
	db.Set("key", []byte("original"))

	ok := db.SetNX("key", Buf("new"))
	if ok {
		t.Fatal("expected SetNX to fail for existing key")
	}

	val, found := db.Get("key")
	if !found || val.String() != "original" {
		t.Fatalf("expected 'original', got '%s', found=%v", val.String(), found)
	}
	val.Close()
}

func TestDecr(t *testing.T) {
	db := New()

	val, err := db.Decr("counter")
	if err != nil || val != -1 {
		t.Fatalf("expected -1, got %d, err=%v", val, err)
	}

	val, err = db.Decr("counter")
	if err != nil || val != -2 {
		t.Fatalf("expected -2, got %d, err=%v", val, err)
	}
}

func TestDecrFromSetValue(t *testing.T) {
	db := New()
	db.Set("counter", []byte("10"))

	val, err := db.Decr("counter")
	if err != nil || val != 9 {
		t.Fatalf("expected 9, got %d, err=%v", val, err)
	}
}

func TestDecrZero(t *testing.T) {
	db := New()
	db.Set("counter", []byte("0"))

	val, err := db.Decr("counter")
	if err != nil || val != -1 {
		t.Fatalf("expected -1, got %d, err=%v", val, err)
	}
}

func TestDecrInvalidValue(t *testing.T) {
	db := New()
	db.Set("str", []byte("not-a-number"))

	_, err := db.Decr("str")
	if err == nil {
		t.Fatal("expected error for invalid value")
	}
}

// ============================================================================
// Hash 新命令测试: HKeys, HVals, HMSet, HMGet
// ============================================================================

func TestHMSet(t *testing.T) {
	db := New()

	n := db.HMSet("h", map[string]*PooledBuffer{
		"f1": Buf("v1"),
		"f2": Buf("v2"),
		"f3": Buf("v3"),
	})
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	val, ok := db.HGet("h", "f1")
	if !ok || val.String() != "v1" {
		t.Fatalf("expected 'v1', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	val, ok = db.HGet("h", "f2")
	if !ok || val.String() != "v2" {
		t.Fatalf("expected 'v2', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()

	val, ok = db.HGet("h", "f3")
	if !ok || val.String() != "v3" {
		t.Fatalf("expected 'v3', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestHMSetUpdate(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("original"))

	n := db.HMSet("h", map[string]*PooledBuffer{
		"f1": Buf("updated"),
		"f2": Buf("new"),
	})
	if n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}

	val, ok := db.HGet("h", "f1")
	if !ok || val.String() != "updated" {
		t.Fatalf("expected 'updated', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestHMSetEmpty(t *testing.T) {
	db := New()
	n := db.HMSet("h", map[string]*PooledBuffer{})
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestHMGet(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))

	results := db.HMGet("h", "f1", "f2", "nonexistent")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0] == nil || results[0].String() != "v1" {
		t.Fatalf("expected 'v1', got '%s'", results[0].String())
	}
	results[0].Close()

	if results[1] == nil || results[1].String() != "v2" {
		t.Fatalf("expected 'v2', got '%s'", results[1].String())
	}
	results[1].Close()

	if results[2] != nil {
		t.Fatalf("expected nil for nonexistent field, got '%s'", results[2].String())
	}
}

func TestHMGetEmptyFields(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))

	results := db.HMGet("h")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestHMGetNonExistentKey(t *testing.T) {
	db := New()
	results := db.HMGet("nonexistent", "f1", "f2")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r != nil {
			t.Fatalf("expected nil at index %d, got '%s'", i, r.String())
		}
	}
}

func TestHKeys(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))
	db.HSet("h", "f3", []byte("v3"))

	keys := db.HKeys("h")
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
}

func TestHKeysEmpty(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HDel("h", "f1")

	keys := db.HKeys("h")
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestHKeysNonExistent(t *testing.T) {
	db := New()
	keys := db.HKeys("nonexistent")
	if keys != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestHVals(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HSet("h", "f2", []byte("v2"))
	db.HSet("h", "f3", []byte("v3"))

	vals := db.HVals("h")
	if len(vals) != 3 {
		t.Fatalf("expected 3 values, got %d", len(vals))
	}
	for _, v := range vals {
		v.Close()
	}
}

func TestHValsEmpty(t *testing.T) {
	db := New()
	db.HSet("h", "f1", []byte("v1"))
	db.HDel("h", "f1")

	vals := db.HVals("h")
	if len(vals) != 0 {
		t.Fatalf("expected 0 values, got %d", len(vals))
	}
}

func TestHValsNonExistent(t *testing.T) {
	db := New()
	vals := db.HVals("nonexistent")
	if vals != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

// ============================================================================
// ZSet 补充测试: ZPopMinNonExistent, ZPopMinCountExceeds, ZPopMaxEmpty,
//                ZPopMaxNonExistent, ZRankNonExistent, ZRankKeyNotFound,
//                ZRevRankNonExistent, ZRevRankKeyNotFound, ZIncrByNegative
// ============================================================================

func TestZPopMinNonExistent(t *testing.T) {
	db := New()
	zs := db.ZPopMin("nonexistent", 1)
	if zs != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestZPopMinCountExceeds(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))

	zs := db.ZPopMin("z", 10)
	if zs == nil {
		t.Fatal("expected result, got nil")
	}
	if zs.Len() != 1 {
		t.Fatalf("expected 1 element, got %d", zs.Len())
	}
	zs.Close()
}

func TestZPopMaxEmpty(t *testing.T) {
	db := New()
	zs := db.ZPopMax("z", 1)
	if zs != nil {
		t.Fatal("expected nil for empty set")
	}
}

func TestZPopMaxNonExistent(t *testing.T) {
	db := New()
	zs := db.ZPopMax("nonexistent", 1)
	if zs != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestZRankNonExistent(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))

	rank, ok := db.ZRank("z", []byte("nonexistent"))
	if ok {
		t.Fatalf("expected not found, got rank %d", rank)
	}
}

func TestZRankKeyNotFound(t *testing.T) {
	db := New()
	rank, ok := db.ZRank("nonexistent", []byte("a"))
	if ok {
		t.Fatalf("expected not found, got rank %d", rank)
	}
}

func TestZRevRankNonExistent(t *testing.T) {
	db := New()
	db.ZAdd("z", 1.0, []byte("a"))

	rank, ok := db.ZRevRank("z", []byte("nonexistent"))
	if ok {
		t.Fatalf("expected not found, got rank %d", rank)
	}
}

func TestZRevRankKeyNotFound(t *testing.T) {
	db := New()
	rank, ok := db.ZRevRank("nonexistent", []byte("a"))
	if ok {
		t.Fatalf("expected not found, got rank %d", rank)
	}
}

func TestZIncrByNegative(t *testing.T) {
	db := New()
	db.ZAdd("z", 10.0, []byte("a"))

	score := db.ZIncrBy("z", []byte("a"), -3.0)
	if score != 7.0 {
		t.Fatalf("expected score 7.0, got %f", score)
	}
}

// ============================================================================
// Stream 基础命令测试: XAdd, XRead, XReadGroup, XGroupCreate, XDel, XTrim, XLen
// ============================================================================

func TestXAdd(t *testing.T) {
	db := New()

	id := db.XAdd("s", "*", map[string]*PooledBuffer{"f": Buf("v")})
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestXAddWithID(t *testing.T) {
	db := New()

	id := db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})
	if id != "1-0" {
		t.Fatalf("expected ID '1-0', got '%s'", id)
	}
}

func TestXRead(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})

	result := db.XRead(map[string]string{"s": "0-0"}, 10)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result["s"]) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result["s"]))
	}
}

func TestXReadNonExistent(t *testing.T) {
	db := New()
	result := db.XRead(map[string]string{"nonexistent": "0-0"}, 10)
	if result != nil && len(result) > 0 {
		t.Fatal("expected empty result")
	}
}

func TestXReadCountLimit(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})
	db.XAdd("s", "3-0", map[string]*PooledBuffer{"f": Buf("v3")})

	result := db.XRead(map[string]string{"s": "0-0"}, 2)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result["s"]) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result["s"]))
	}
}

func TestXGroupCreate(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})

	err := db.XGroupCreate("s", "g", "0-0")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestXGroupCreateNonExistent(t *testing.T) {
	db := New()
	err := db.XGroupCreate("nonexistent", "g", "0-0")
	if err == nil {
		t.Fatal("expected error for nonexistent stream")
	}
}

func TestXReadGroup(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})
	db.XGroupCreate("s", "g", "0-0")

	result := db.XReadGroup("g", "c1", map[string]string{"s": ">"}, 10)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result["s"]) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result["s"]))
	}
}

func TestXReadGroupNonExistent(t *testing.T) {
	db := New()
	result := db.XReadGroup("g", "c", map[string]string{"nonexistent": ">"}, 10)
	if result != nil && len(result) > 0 {
		t.Fatal("expected empty result")
	}
}

func TestXReadGroupPending(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})
	db.XGroupCreate("s", "g", "0-0")

	db.XReadGroup("g", "c1", map[string]string{"s": ">"}, 10)

	result := db.XReadGroup("g", "c1", map[string]string{"s": "0-0"}, 10)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result["s"]) != 1 {
		t.Fatalf("expected 1 pending entry, got %d", len(result["s"]))
	}
}

func TestXDel(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})

	n := db.XDel("s", []string{"1-0"})
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	if db.XLen("s") != 1 {
		t.Fatalf("expected 1 entry remaining, got %d", db.XLen("s"))
	}
}

func TestXDelNonExistent(t *testing.T) {
	db := New()
	n := db.XDel("nonexistent", []string{"1-0"})
	if n != 0 {
		t.Fatalf("expected 0 deleted, got %d", n)
	}
}

func TestXDelInvalidID(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})

	n := db.XDel("s", []string{"999-0"})
	if n != 0 {
		t.Fatalf("expected 0 deleted, got %d", n)
	}
}

func TestXTrim(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})
	db.XAdd("s", "3-0", map[string]*PooledBuffer{"f": Buf("v3")})

	n := db.XTrim("s", 2)
	if n != 1 {
		t.Fatalf("expected 1 trimmed, got %d", n)
	}

	if db.XLen("s") != 2 {
		t.Fatalf("expected 2 entries remaining, got %d", db.XLen("s"))
	}
}

func TestXTrimNonExistent(t *testing.T) {
	db := New()
	n := db.XTrim("nonexistent", 10)
	if n != 0 {
		t.Fatalf("expected 0 trimmed, got %d", n)
	}
}

func TestXTrimLargerThanSize(t *testing.T) {
	db := New()
	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v")})

	n := db.XTrim("s", 100)
	if n != 0 {
		t.Fatalf("expected 0 trimmed, got %d", n)
	}
}

func TestXLen(t *testing.T) {
	db := New()

	if db.XLen("s") != 0 {
		t.Fatalf("expected 0, got %d", db.XLen("s"))
	}

	db.XAdd("s", "1-0", map[string]*PooledBuffer{"f": Buf("v1")})
	db.XAdd("s", "2-0", map[string]*PooledBuffer{"f": Buf("v2")})

	if db.XLen("s") != 2 {
		t.Fatalf("expected 2, got %d", db.XLen("s"))
	}
}

func TestXLenNonExistent(t *testing.T) {
	db := New()
	if db.XLen("nonexistent") != 0 {
		t.Fatalf("expected 0 for nonexistent stream, got %d", db.XLen("nonexistent"))
	}
}

// ============================================================================
// 键过期命令测试: Expire, PExpire, ExpireAt, PExpireAt, TTL, PTTL, Persist
// ============================================================================

func TestExpire(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	ok := db.Expire("key", 100)
	if !ok {
		t.Fatal("expected Expire to succeed")
	}

	ttl := db.TTL("key")
	if ttl <= 0 || ttl > 100 {
		t.Fatalf("expected TTL > 0 and <= 100, got %d", ttl)
	}
}

func TestExpireNonExistent(t *testing.T) {
	db := New()
	ok := db.Expire("nonexistent", 100)
	if ok {
		t.Fatal("expected Expire to fail for nonexistent key")
	}
}

func TestPExpire(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	ok := db.PExpire("key", 50000)
	if !ok {
		t.Fatal("expected PExpire to succeed")
	}

	pttl := db.PTTL("key")
	if pttl <= 0 || pttl > 50000 {
		t.Fatalf("expected PTTL > 0 and <= 50000, got %d", pttl)
	}
}

func TestExpireAt(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	futureTime := time.Now().Unix() + 3600
	ok := db.ExpireAt("key", futureTime)
	if !ok {
		t.Fatal("expected ExpireAt to succeed")
	}

	ttl := db.TTL("key")
	if ttl <= 0 || ttl > 3600 {
		t.Fatalf("expected TTL > 0 and <= 3600, got %d", ttl)
	}
}

func TestExpireAtPast(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	pastTime := time.Now().Unix() - 1
	ok := db.ExpireAt("key", pastTime)
	if !ok {
		t.Fatal("expected ExpireAt to succeed (key should be deleted)")
	}

	if db.Exists("key") {
		t.Fatal("expected key to be deleted")
	}
}

func TestPExpireAt(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	futureTimeMs := time.Now().UnixMilli() + 60000
	ok := db.PExpireAt("key", futureTimeMs)
	if !ok {
		t.Fatal("expected PExpireAt to succeed")
	}

	pttl := db.PTTL("key")
	if pttl <= 0 || pttl > 60000 {
		t.Fatalf("expected PTTL > 0 and <= 60000, got %d", pttl)
	}
}

func TestTTL(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.Expire("key", 100)

	ttl := db.TTL("key")
	if ttl <= 0 || ttl > 100 {
		t.Fatalf("expected TTL > 0 and <= 100, got %d", ttl)
	}
}

func TestTTLNonExistent(t *testing.T) {
	db := New()
	ttl := db.TTL("nonexistent")
	if ttl != -1 {
		t.Fatalf("expected TTL -1 for nonexistent key, got %d", ttl)
	}
}

func TestTTLNoExpiry(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	ttl := db.TTL("key")
	if ttl != -2 {
		t.Fatalf("expected TTL -2 for key without expiry, got %d", ttl)
	}
}

func TestPTTL(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.PExpire("key", 50000)

	pttl := db.PTTL("key")
	if pttl <= 0 || pttl > 50000 {
		t.Fatalf("expected PTTL > 0 and <= 50000, got %d", pttl)
	}
}

func TestPersist(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.Expire("key", 100)

	ok := db.Persist("key")
	if !ok {
		t.Fatal("expected Persist to succeed")
	}

	ttl := db.TTL("key")
	if ttl != -2 {
		t.Fatalf("expected TTL -2 after Persist, got %d", ttl)
	}
}

func TestPersistNonExistent(t *testing.T) {
	db := New()
	ok := db.Persist("nonexistent")
	if ok {
		t.Fatal("expected Persist to fail for nonexistent key")
	}
}

func TestPersistNoExpiry(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	ok := db.Persist("key")
	if ok {
		t.Fatal("expected Persist to fail for key without expiry")
	}
}

func TestKeyExpiration(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))

	db.PExpire("key", 10)

	time.Sleep(20 * time.Millisecond)

	if db.Exists("key") {
		t.Fatal("expected key to be expired and deleted")
	}
}

func TestExpireAfterSet(t *testing.T) {
	db := New()
	db.Set("key", []byte("original"))
	db.Expire("key", 10000)

	val, ok := db.Get("key")
	if !ok || val.String() != "original" {
		t.Fatalf("expected 'original', got '%s', ok=%v", val.String(), ok)
	}
	val.Close()
}

func TestFlushAllClearsExpiry(t *testing.T) {
	db := New()
	db.Set("key1", []byte("value1"))
	db.Set("key2", []byte("value2"))
	db.Expire("key1", 10000)
	db.Expire("key2", 10000)

	db.FlushAll()

	if db.Exists("key1") || db.Exists("key2") {
		t.Fatal("expected all keys to be deleted after FlushAll")
	}
}

func TestGetExpiredKeyReturnsNil(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.Expire("key", 1)

	time.Sleep(1100 * time.Millisecond)

	val, ok := db.Get("key")
	if ok || val != nil {
		t.Fatalf("expected expired key to return nil, got ok=%v, val=%v", ok, val)
	}
}

func TestSetExSetsExpiration(t *testing.T) {
	db := New()
	db.SetEx("key", 10, []byte("value"))

	ttl := db.TTL("key")
	if ttl <= 0 || ttl > 10 {
		t.Fatalf("expected TTL > 0 and <= 10, got %d", ttl)
	}
}

func TestPsetExSetsExpiration(t *testing.T) {
	db := New()
	db.PsetEx("key", 5000, []byte("value"))

	pttl := db.PTTL("key")
	if pttl <= 0 || pttl > 5000 {
		t.Fatalf("expected PTTL > 0 and <= 5000, got %d", pttl)
	}
}

func TestSetNXOnExpiredKeySucceeds(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.Expire("key", 1)

	time.Sleep(1100 * time.Millisecond)

	pb := BufFromBytes([]byte("newvalue"))
	ok := db.SetNX("key", pb)
	pb.Close()
	if !ok {
		t.Fatal("expected SetNX on expired key to succeed")
	}

	val, _ := db.Get("key")
	if val.String() != "newvalue" {
		t.Fatalf("expected 'newvalue', got '%s'", val.String())
	}
	val.Close()
}

func TestMSetNXOnExpiredKeySucceeds(t *testing.T) {
	db := New()
	db.Set("key1", []byte("value1"))
	db.Set("key2", []byte("value2"))
	db.Expire("key1", 1)

	time.Sleep(1100 * time.Millisecond)

	pb1 := BufFromBytes([]byte("newvalue1"))
	pb2 := BufFromBytes([]byte("newvalue2"))
	inserted := db.MSetNX(map[string]*PooledBuffer{
		"key1": pb1,
		"key2": pb2,
	})
	pb1.Close()
	pb2.Close()
	if inserted != 1 {
		t.Fatalf("expected 1 key inserted, got %d", inserted)
	}

	val1, _ := db.Get("key1")
	if val1.String() != "newvalue1" {
		t.Fatalf("expected 'newvalue1', got '%s'", val1.String())
	}
	val1.Close()

	val2, _ := db.Get("key2")
	if val2.String() != "value2" {
		t.Fatalf("expected 'value2' (unchanged), got '%s'", val2.String())
	}
	val2.Close()
}

func TestGetDelOnExpiredKeyReturnsNil(t *testing.T) {
	db := New()
	db.Set("key", []byte("value"))
	db.Expire("key", 1)

	time.Sleep(1100 * time.Millisecond)

	val, ok := db.GetDel("key")
	if ok || val != nil {
		t.Fatalf("expected GetDel on expired key to return nil, got ok=%v, val=%v", ok, val)
	}
}
