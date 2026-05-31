package gedis

import (
	"testing"
)

func TestBitmapGetRawData(t *testing.T) {
	db := New()

	db.SetBit("bitmap_raw", 100, 1)
	db.SetBit("bitmap_raw", 200, 1)
	db.SetBit("bitmap_raw", 300, 1)

	val := db.GetBit("bitmap_raw", 100)
	if val != 1 {
		t.Errorf("expected 1, got %d", val)
	}
	val = db.GetBit("bitmap_raw", 150)
	if val != 0 {
		t.Errorf("expected 0, got %d", val)
	}

	count := db.BitCount("bitmap_raw", 0, -1)
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestBitmapSetBitEdge(t *testing.T) {
	db := New()

	db.SetBit("bm_edge", 0, 1)
	db.SetBit("bm_edge", 7, 1)
	db.SetBit("bm_edge", 15, 1)

	if db.GetBit("bm_edge", 0) != 1 {
		t.Error("expected bit 0 to be 1")
	}
	if db.GetBit("bm_edge", 7) != 1 {
		t.Error("expected bit 7 to be 1")
	}
	if db.GetBit("bm_edge", 3) != 0 {
		t.Error("expected bit 3 to be 0")
	}
}

func TestBitFieldLowCov(t *testing.T) {
	db := New()

	v := db.BitField("bitfield_test", Buf("INCRBY"), Buf("i8"), Buf("0"), Buf("42"))
	if len(v) != 1 || v[0] != 42 {
		t.Errorf("expected [42], got %v", v)
	}

	v = db.BitField("bitfield_test", Buf("GET"), Buf("i8"), Buf("0"))
	if len(v) != 1 || v[0] != 42 {
		t.Errorf("expected [42], got %v", v)
	}
}

func TestBitOpLowCov(t *testing.T) {
	db := New()

	db.Set("bitop1", []byte{0xFF, 0xF0})
	db.Set("bitop2", []byte{0x0F, 0xFF})

	n := db.BitOp("AND", "bitop_dest", "bitop1", "bitop2")
	if n != 2 {
		t.Errorf("expected len 2, got %d", n)
	}
}

func TestBitPosLowCov(t *testing.T) {
	db := New()

	db.Set("bitpos_test", []byte{0x00, 0x02})

	pos := db.BitPos("bitpos_test", 1, 0, 2)
	if pos != 14 {
		t.Errorf("expected pos 14, got %d", pos)
	}
}

func TestBitCountLowCov(t *testing.T) {
	db := New()

	db.Set("bitcount_test", []byte{0xFF, 0x0F})

	n := db.BitCount("bitcount_test", 0, 1)
	if n != 12 {
		t.Errorf("expected 12 bits, got %d", n)
	}
}

func TestGetBitEdge(t *testing.T) {
	db := New()

	bit := db.GetBit("nonexistent", 0)
	if bit != 0 {
		t.Error("expected 0 for nonexistent key")
	}
}
