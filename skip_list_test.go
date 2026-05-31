package gedis

import "testing"

func TestZslCreate(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	if zsl.headerOff == 0 {
		t.Error("expected non-zero headerOff")
	}
	if zsl.length != 0 {
		t.Errorf("expected length 0, got %d", zsl.length)
	}
	if zsl.level != 1 {
		t.Errorf("expected level 1, got %d", zsl.level)
	}
}

func TestZslInsert(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	member1 := arena.AllocBytes([]byte("member1"))
	nodeOff1 := zslInsert(arena, &zsl, member1, 1.0)
	if nodeOff1 == 0 {
		t.Error("expected non-zero node offset")
	}
	if zsl.length != 1 {
		t.Errorf("expected length 1, got %d", zsl.length)
	}

	member2 := arena.AllocBytes([]byte("member2"))
	nodeOff2 := zslInsert(arena, &zsl, member2, 2.0)
	if nodeOff2 == 0 {
		t.Error("expected non-zero node offset for second insert")
	}
	if zsl.length != 2 {
		t.Errorf("expected length 2, got %d", zsl.length)
	}
}

func TestZslInsertSameMember(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	member := arena.AllocBytes([]byte("same"))
	nodeOff1 := zslInsert(arena, &zsl, member, 1.0)
	nodeOff2 := zslInsert(arena, &zsl, member, 2.0)

	if nodeOff1 == 0 || nodeOff2 == 0 {
		t.Error("expected non-zero node offsets")
	}
}

func TestZslDelete(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	member := arena.AllocBytes([]byte("delete_me"))
	zslInsert(arena, &zsl, member, 1.0)

	if zsl.length != 1 {
		t.Fatalf("expected length 1, got %d", zsl.length)
	}

	deleted := zslDelete(arena, &zsl, member, 1.0)
	if !deleted {
		t.Error("expected delete to succeed")
	}
	if zsl.length != 0 {
		t.Errorf("expected length 0 after delete, got %d", zsl.length)
	}
}

func TestZslDeleteNotFound(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	member := arena.AllocBytes([]byte("exists"))
	zslInsert(arena, &zsl, member, 1.0)

	notExists := arena.AllocBytes([]byte("not_exists"))
	deleted := zslDelete(arena, &zsl, notExists, 1.0)
	if deleted {
		t.Error("expected delete to fail for non-existent member")
	}
}

func TestZslGetRank(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	members := []string{"a", "b", "c", "d"}
	scores := []float64{1.0, 2.0, 3.0, 4.0}

	for i, m := range members {
		member := arena.AllocBytes([]byte(m))
		zslInsert(arena, &zsl, member, scores[i])
	}

	for i, m := range members {
		member := arena.AllocBytes([]byte(m))
		rank := zslGetRank(arena, &zsl, member, scores[i])
		if rank != i+1 {
			t.Errorf("expected rank %d for %s, got %d", i+1, m, rank)
		}
	}
}

func TestZslGetElementByRank(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	members := []string{"first", "second", "third"}
	scores := []float64{1.0, 2.0, 3.0}

	for i, m := range members {
		member := arena.AllocBytes([]byte(m))
		zslInsert(arena, &zsl, member, scores[i])
	}

	for i := 1; i <= 3; i++ {
		nodeOff := zslGetElementByRank(arena, &zsl, i)
		if nodeOff == 0 {
			t.Errorf("expected non-zero offset for rank %d", i)
		}
	}
}

func TestZslDeleteNode(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	member := arena.AllocBytes([]byte("target"))
	nodeOff := zslInsert(arena, &zsl, member, 1.0)

	update := make([]int, zsl.level+1)
	zslDeleteNode(arena, &zsl, nodeOff, update)

	if zsl.length != 0 {
		t.Errorf("expected length 0, got %d", zsl.length)
	}
}

func TestZslMultipleLevels(t *testing.T) {
	arena := NewArena(16384)
	zsl := zslCreate(arena)

	for i := 0; i < 100; i++ {
		member := arena.AllocBytes([]byte(string(rune('a' + i%26))))
		zslInsert(arena, &zsl, member, float64(i))
	}

	if zsl.length != 100 {
		t.Errorf("expected length 100, got %d", zsl.length)
	}

	if zsl.level < 1 {
		t.Errorf("expected level at least 1, got %d", zsl.level)
	}
}

func TestZslInsertDescending(t *testing.T) {
	arena := NewArena(4096)
	zsl := zslCreate(arena)

	for i := 10; i >= 1; i-- {
		member := arena.AllocBytes([]byte(string(rune('0' + i))))
		zslInsert(arena, &zsl, member, float64(i))
	}

	if zsl.length != 10 {
		t.Errorf("expected length 10, got %d", zsl.length)
	}
}