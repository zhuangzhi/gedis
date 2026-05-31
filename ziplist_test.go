package gedis

import "testing"

func TestParseInteger(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantVal  int64
		wantOk   bool
	}{
		{"positive", []byte("123"), 123, true},
		{"negative", []byte("-456"), -456, true},
		{"with plus", []byte("+789"), 789, true},
		{"zero", []byte("0"), 0, true},
		{"empty", []byte(""), 0, false},
		{"only minus", []byte("-"), 0, false},
		{"only plus", []byte("+"), 0, false},
		{"invalid char", []byte("12a3"), 0, false},
		{"leading zeros", []byte("007"), 7, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := parseInteger(tt.data)
			if ok != tt.wantOk {
				t.Errorf("parseInteger(%v) ok = %v, want %v", tt.data, ok, tt.wantOk)
			}
			if ok && val != tt.wantVal {
				t.Errorf("parseInteger(%v) = %v, want %v", tt.data, val, tt.wantVal)
			}
		})
	}
}

func TestZiplistFindBasic(t *testing.T) {
	arena := NewArena(1024)
	zlOff := ziplistNew(arena)

	zlOff = ziplistInsert(arena, zlOff, []byte("apple"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("banana"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("cherry"), false)

	if ziplistFind(arena, zlOff, []byte("banana")) != 1 {
		t.Error("expected to find banana at index 1")
	}

	if ziplistFind(arena, zlOff, []byte("notexist")) != -1 {
		t.Error("expected -1 for not exist")
	}

	if ziplistFind(arena, 0, []byte("test")) != -1 {
		t.Error("expected -1 for zero offset")
	}
}

func TestZiplistLen(t *testing.T) {
	arena := NewArena(1024)

	if ziplistLen(arena, 0) != 0 {
		t.Error("expected 0 for zero offset")
	}

	zlOff := ziplistNew(arena)
	if ziplistLen(arena, zlOff) != 0 {
		t.Error("expected 0 for empty ziplist")
	}

	zlOff = ziplistInsert(arena, zlOff, []byte("first"), false)
	if ziplistLen(arena, zlOff) != 1 {
		t.Errorf("expected 1, got %d", ziplistLen(arena, zlOff))
	}

	zlOff = ziplistInsert(arena, zlOff, []byte("second"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("third"), false)
	if ziplistLen(arena, zlOff) != 3 {
		t.Errorf("expected 3, got %d", ziplistLen(arena, zlOff))
	}
}

func TestZiplistGet(t *testing.T) {
	arena := NewArena(1024)
	zlOff := ziplistNew(arena)

	zlOff = ziplistInsert(arena, zlOff, []byte("apple"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("banana"), false)

	if ziplistGet(arena, 0, 0) != nil {
		t.Error("expected nil for zero offset")
	}

	if ziplistGet(arena, zlOff, -1) != nil {
		t.Error("expected nil for negative index")
	}

	if ziplistGet(arena, zlOff, 100) != nil {
		t.Error("expected nil for out of bounds index")
	}

	data := ziplistGet(arena, zlOff, 0)
	if string(data) != "apple" {
		t.Errorf("expected apple, got %s", string(data))
	}

	data = ziplistGet(arena, zlOff, 1)
	if string(data) != "banana" {
		t.Errorf("expected banana, got %s", string(data))
	}
}

func TestZiplistDelete(t *testing.T) {
	arena := NewArena(1024)
	zlOff := ziplistNew(arena)

	zlOff = ziplistInsert(arena, zlOff, []byte("first"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("second"), false)
	zlOff = ziplistInsert(arena, zlOff, []byte("third"), false)

	pos := ziplistFind(arena, zlOff, []byte("second"))
	if pos == -1 {
		t.Fatal("expected to find second")
	}

	newOff := ziplistDelete(arena, zlOff, pos)
	if ziplistLen(arena, newOff) != 2 {
		t.Errorf("expected 2 after delete, got %d", ziplistLen(arena, newOff))
	}
}

func TestBytesEqual(t *testing.T) {
	if !bytesEqual([]byte("hello"), []byte("hello")) {
		t.Error("expected equal")
	}

	if bytesEqual([]byte("hello"), []byte("world")) {
		t.Error("expected not equal")
	}

	if bytesEqual([]byte("hello"), []byte("hell")) {
		t.Error("expected not equal - different length")
	}

	if !bytesEqual([]byte(""), []byte("")) {
		t.Error("expected equal for empty")
	}
}

func TestZiplistEntryTotalSize(t *testing.T) {
	arena := NewArena(1024)
	zlOff := ziplistNew(arena)

	zlOff = ziplistInsert(arena, zlOff, []byte("x"), false)

	pos := ziplistHeaderSize
	size := ziplistEntryTotalSize(arena, pos)
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}
}