package gedis

import (
	"testing"
)

func TestBFReserveAndAdd(t *testing.T) {
	db := New()

	db.BFReserve("bf_test", 0.01, 1000)

	db.BFAdd("bf_test", []byte("item1"))
	db.BFAdd("bf_test", []byte("item2"))

	if !db.BFExists("bf_test", []byte("item1")) {
		t.Error("expected item1 to exist in bloom filter")
	}
	if !db.BFExists("bf_test", []byte("item2")) {
		t.Error("expected item2 to exist in bloom filter")
	}
	if db.BFExists("bf_test", []byte("nonexistent")) {
		t.Error("expected nonexistent item to not exist")
	}
}

func TestCFReserveAndAdd(t *testing.T) {
	db := New()

	db.CFReserve("cf_test", 1000)

	db.CFAdd("cf_test", []byte("item1"))
	db.CFAdd("cf_test", []byte("item2"))

	if !db.CFExists("cf_test", []byte("item1")) {
		t.Error("expected item1 to exist in cuckoo filter")
	}
	if !db.CFExists("cf_test", []byte("item2")) {
		t.Error("expected item2 to exist in cuckoo filter")
	}
	if db.CFExists("cf_test", []byte("nonexistent")) {
		t.Error("expected nonexistent item to not exist")
	}

	db.CFDel("cf_test", []byte("item1"))
	if db.CFExists("cf_test", []byte("item1")) {
		t.Error("expected item1 to be deleted")
	}
}

func TestCMSIncrBy(t *testing.T) {
	db := New()

	db.CMSIncrBy("cms_test", []byte("a"), 1)
	db.CMSIncrBy("cms_test", []byte("a"), 2)
	db.CMSIncrBy("cms_test", []byte("b"), 3)

	counts := db.CMSQuery("cms_test", []byte("a"), []byte("b"))
	if counts[0] != 3 {
		t.Errorf("expected count 3 for 'a', got %d", counts[0])
	}
	if counts[1] != 3 {
		t.Errorf("expected count 3 for 'b', got %d", counts[1])
	}
}

func TestTopKAdd(t *testing.T) {
	db := New()

	db.TopKReserve("topk_test", 3)

	db.TopKAdd("topk_test", "a", "a", "a", "b", "b", "c", "d")

	result := db.TopKList("topk_test")
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestTopKReserveLowCov(t *testing.T) {
	db := New()

	db.TopKReserve("topk_test", 3)

	db.TopKAdd("topk_test", "a", "b", "c", "d", "e", "a", "a")

	list := db.TopKList("topk_test")
	if len(list) == 0 {
		t.Error("expected top-k items")
	}
}

func TestBFBufferVariants(t *testing.T) {
	db := New()

	db.BFReserve("bf_buf", 0.01, 100)

	pb := Buf("buf_item")
	db.BFAddBuffer("bf_buf", pb)
	pb.Close()

	if !db.BFExistsBuffer("bf_buf", Buf("buf_item")) {
		t.Error("expected buf_item to exist")
	}
	if db.BFExistsBuffer("bf_buf", Buf("nonexistent")) {
		t.Error("expected nonexistent to not exist")
	}
}

func TestCFBufferVariants(t *testing.T) {
	db := New()

	db.CFReserve("cf_buf", 100)

	pb := Buf("cf_item")
	db.CFAddBuffer("cf_buf", pb)
	pb.Close()

	if !db.CFExistsBuffer("cf_buf", Buf("cf_item")) {
		t.Error("expected cf_item to exist")
	}

	db.CFDelBuffer("cf_buf", Buf("cf_item"))
	if db.CFExistsBuffer("cf_buf", Buf("cf_item")) {
		t.Error("expected cf_item to be deleted")
	}
}

func TestCMSQueryBuffer(t *testing.T) {
	db := New()

	db.CMSInitByDim("cms_buf", 10, 5)

	db.CMSIncrByBuffer("cms_buf", Buf("item1"), 1)
	db.CMSIncrByBuffer("cms_buf", Buf("item2"), 3)

	results := db.CMSQueryBuffer("cms_buf", Buf("item1"), Buf("item2"))
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
