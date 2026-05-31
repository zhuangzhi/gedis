package gedis

import "testing"

func TestZSlicesNew(t *testing.T) {
	zs := NewZSlices()
	if zs == nil {
		t.Fatal("expected non-nil ZSlices")
	}
	if zs.Len() != 0 {
		t.Errorf("expected len 0, got %d", zs.Len())
	}
	zs.Close()
}

func TestZSlicesAddAndLen(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("hello"))
	zs.Add([]byte("world"))

	if zs.Len() != 0 {
		t.Errorf("expected len 0 before Finish, got %d", zs.Len())
	}

	zs.Finish()

	if zs.Len() != 2 {
		t.Errorf("expected len 2 after Finish, got %d", zs.Len())
	}

	data := zs.Get(0)
	if string(data) != "hello" {
		t.Errorf("expected hello, got %s", string(data))
	}

	data = zs.Get(1)
	if string(data) != "world" {
		t.Errorf("expected world, got %s", string(data))
	}

	zs.Close()
}

func TestZSlicesAddString(t *testing.T) {
	zs := NewZSlices()
	zs.AddString("first")
	zs.AddString("second")

	zs.Finish()

	if zs.Len() != 2 {
		t.Errorf("expected len 2, got %d", zs.Len())
	}

	zs.Close()
}

func TestZSlicesFinish(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("item1"))
	zs.Add([]byte("item2"))
	zs.Add([]byte("item3"))

	zs.Finish()

	if zs.Len() != 3 {
		t.Errorf("expected len 3 after Finish, got %d", zs.Len())
	}

	zs.Close()
}

func TestZSlicesClose(t *testing.T) {
	zs := NewZSlices()
	zs.Add([]byte("data"))

	err := zs.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	err = zs.Close()
	if err != nil {
		t.Errorf("second Close failed: %v", err)
	}
}

func TestZSlicesEmpty(t *testing.T) {
	zs := NewZSlices()
	if zs.Len() != 0 {
		t.Errorf("expected len 0, got %d", zs.Len())
	}
	if zs.Get(0) != nil {
		t.Error("expected nil for empty slices")
	}
	zs.Finish()
	if zs.Len() != 0 {
		t.Errorf("expected len 0 after Finish, got %d", zs.Len())
	}
	zs.Close()
}