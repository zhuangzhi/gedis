package gedis

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
)

// ============================================================================
// Unit Tests
// ============================================================================

func TestNewPoolDefaultSizes(t *testing.T) {
	pool := NewPool()
	if len(pool.sizes) != 6 {
		t.Fatalf("expected 6 levels, got %d", len(pool.sizes))
	}
	if pool.sizes[0] != 1024 || pool.sizes[5] != 1048576 {
		t.Fatalf("unexpected default sizes: %v", pool.sizes)
	}
}

func TestNewLeveledPool(t *testing.T) {
	sizes := []int{64, 256, 1024}
	pool := NewLeveledPool(sizes)
	if len(pool.sizes) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(pool.sizes))
	}
}

func TestGetExactMatch(t *testing.T) {
	pool := NewLeveledPool([]int{64, 256, 1024})
	pb := pool.Get(256)
	if pb.level != 1 {
		t.Fatalf("expected level 1, got %d", pb.level)
	}
	if pb.Cap() < 256 {
		t.Fatalf("expected cap >= 256, got %d", pb.Cap())
	}
	pb.Close()
}

func TestGetFitsInHigherLevel(t *testing.T) {
	pool := NewLeveledPool([]int{64, 256, 1024})
	pb := pool.Get(100)
	if pb.level != 1 {
		t.Fatalf("expected level 1 (256 fits 100), got %d", pb.level)
	}
	pb.Close()
}

func TestGetOversize(t *testing.T) {
	pool := NewLeveledPool([]int{64, 256, 1024})
	pb := pool.Get(9999)
	if pb.pool != nil {
		t.Fatal("oversized should not be pooled")
	}
	if pb.level != -1 {
		t.Fatalf("expected level -1, got %d", pb.level)
	}
	if pb.Cap() < 9999 {
		t.Fatalf("expected cap >= 9999, got %d", pb.Cap())
	}
}

func TestGetReuse(t *testing.T) {
	pool := NewLeveledPool([]int{64, 256, 1024})

	pb1 := pool.Get(100)
	addr1 := fmt.Sprintf("%p", pb1.buf)
	pb1.Close()

	pb2 := pool.Get(100)
	addr2 := fmt.Sprintf("%p", pb2.buf)

	if addr1 != addr2 {
		t.Fatalf("expected buffer reuse, got different pointers: %s vs %s", addr1, addr2)
	}
	pb2.Close()
}

func TestWriteAndBytes(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(16)
	defer pb.Close()

	data := []byte("hello world")
	n, err := pb.Write(data)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if !bytes.Equal(pb.Bytes(), data) {
		t.Fatalf("expected %q, got %q", data, pb.Bytes())
	}
}

func TestWriteString(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(16)
	defer pb.Close()

	n, err := pb.WriteString("hello")
	if err != nil {
		t.Fatalf("writeString error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	if pb.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", pb.String())
	}
}

func TestWriteByte(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(16)
	defer pb.Close()

	if err := pb.WriteByte('A'); err != nil {
		t.Fatalf("writeByte error: %v", err)
	}
	if err := pb.WriteByte('B'); err != nil {
		t.Fatalf("writeByte error: %v", err)
	}
	if string(pb.Bytes()) != "AB" {
		t.Fatalf("expected 'AB', got %q", pb.Bytes())
	}
}

func TestWriteRune(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(16)
	defer pb.Close()

	n, err := pb.WriteRune('中')
	if err != nil {
		t.Fatalf("writeRune error: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 bytes for '中', got %d", n)
	}
	if pb.String() != "中" {
		t.Fatalf("expected '中', got %q", pb.String())
	}
}

func TestAutomaticGrow(t *testing.T) {
	pool := NewLeveledPool([]int{16, 64, 256})

	pb := pool.Get(8)
	defer pb.Close()

	longStr := bytes.Repeat([]byte("x"), 200)
	n, err := pb.Write(longStr)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != 200 {
		t.Fatalf("expected 200, got %d", n)
	}
	if pb.Len() != 200 {
		t.Fatalf("expected len 200, got %d", pb.Len())
	}

	if pb.String() != string(longStr) {
		t.Fatal("buffer content mismatch after grow")
	}
}

func TestGrowBeyondMaxLevel(t *testing.T) {
	pool := NewLeveledPool([]int{16, 64})

	pb := pool.Get(8)
	defer pb.Close()

	large := bytes.Repeat([]byte("y"), 10000)
	pb.Write(large)

	if pb.pool != nil {
		t.Fatal("pool should be nil after growing beyond max level")
	}
	if pb.level != -1 {
		t.Fatalf("expected level -1, got %d", pb.level)
	}
}

func TestAutoGrowPreservesData(t *testing.T) {
	pool := NewLeveledPool([]int{8, 32, 128})

	pb := pool.Get(8)

	pb.WriteString("hello ")
	pb.WriteString("world")
	result := pb.String()

	pb.Close()

	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
}

func TestLenAndCap(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	if pb.Len() != 0 {
		t.Fatalf("expected len 0, got %d", pb.Len())
	}
	if pb.Cap() < 64 {
		t.Fatalf("expected cap >= 64, got %d", pb.Cap())
	}

	pb.WriteString("abc")
	if pb.Len() != 3 {
		t.Fatalf("expected len 3, got %d", pb.Len())
	}
}

func TestReset(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("hello world")
	pb.Reset()
	if pb.Len() != 0 {
		t.Fatalf("expected len 0 after reset, got %d", pb.Len())
	}
}

func TestTruncate(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("hello world")
	pb.Truncate(5)
	if pb.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", pb.String())
	}
}

func TestRead(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("hello")

	buf := make([]byte, 3)
	n, err := pb.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if n != 3 || string(buf[:n]) != "hel" {
		t.Fatalf("expected 'hel', got %q", buf[:n])
	}
}

func TestNext(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("hello world")
	b := pb.Next(5)
	if string(b) != "hello" {
		t.Fatalf("expected 'hello', got %q", b)
	}
}

func TestReadByte(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("ab")
	b, err := pb.ReadByte()
	if err != nil {
		t.Fatalf("readByte error: %v", err)
	}
	if b != 'a' {
		t.Fatalf("expected 'a', got %c", b)
	}
}

func TestReadBytes(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("a b c")
	data, err := pb.ReadBytes(' ')
	if err != nil {
		t.Fatalf("readBytes error: %v", err)
	}
	if string(data) != "a " {
		t.Fatalf("expected 'a ', got %q", data)
	}
}

func TestReadString(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("hello\nworld")
	s, err := pb.ReadString('\n')
	if err != nil {
		t.Fatalf("readString error: %v", err)
	}
	if s != "hello\n" {
		t.Fatalf("expected 'hello\\n', got %q", s)
	}
}

func TestCloseNilPool(t *testing.T) {
	pb := &PooledBuffer{
		buf: bytes.NewBuffer(make([]byte, 0, 64)),
	}
	if err := pb.Close(); err != nil {
		t.Fatalf("close should succeed: %v", err)
	}
}

func TestConcurrentGetWriteClose(t *testing.T) {
	pool := NewPool()
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pb := pool.Get(256)
			defer pb.Close()

			msg := fmt.Sprintf("goroutine-%d ", idx)
			pb.WriteString(msg)
			pb.WriteString("hello")

			if pb.Len() < len(msg) {
				t.Errorf("unexpected short write in goroutine %d", idx)
			}
		}(i)
	}
	wg.Wait()
}

func TestGrow(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.Grow(10000)
	if pb.Cap() < 10000 {
		t.Fatalf("expected cap >= 10000, got %d", pb.Cap())
	}
}

func TestIoWriterInterface(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	var w io.Writer = pb
	n, err := w.Write([]byte("test"))
	if err != nil {
		t.Fatalf("io.Writer error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4, got %d", n)
	}
}

func TestIoReaderInterface(t *testing.T) {
	pool := NewPool()
	pb := pool.Get(64)
	defer pb.Close()

	pb.WriteString("data")

	var r io.Reader = pb
	buf := make([]byte, 2)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("io.Reader error: %v", err)
	}
	if n != 2 || string(buf[:n]) != "da" {
		t.Fatalf("expected 'da', got %q", buf[:n])
	}
}

func TestRuneLen(t *testing.T) {
	tests := []struct {
		r rune
		n int
	}{
		{'a', 1},
		{'é', 2},
		{'中', 3},
		{'𠀀', 4},
	}
	for _, tt := range tests {
		if got := runeLen(tt.r); got != tt.n {
			t.Errorf("runeLen(%q)=%d, want %d", tt.r, got, tt.n)
		}
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkPoolGetReturn(b *testing.B) {
	pool := NewPool()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb := pool.Get(512)
		pb.Close()
	}
}

func BenchmarkPoolGetWriteReturn(b *testing.B) {
	pool := NewPool()
	data := []byte("hello world")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb := pool.Get(64)
		pb.Write(data)
		pb.Close()
	}
}

func BenchmarkPoolWriteString(b *testing.B) {
	pool := NewPool()
	pb := pool.Get(1024)
	defer pb.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb.Reset()
		pb.WriteString("benchmark test string")
	}
}

func BenchmarkPoolWrite(b *testing.B) {
	pool := NewPool()
	pb := pool.Get(1024)
	defer pb.Close()
	data := []byte("benchmark test bytes")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb.Reset()
		pb.Write(data)
	}
}

func BenchmarkPoolWriteByte(b *testing.B) {
	pool := NewPool()
	pb := pool.Get(1024)
	defer pb.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb.Reset()
		pb.WriteByte('x')
	}
}

func BenchmarkPoolWriteRune(b *testing.B) {
	pool := NewPool()
	pb := pool.Get(1024)
	defer pb.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pb.Reset()
		pb.WriteRune('中')
	}
}

func BenchmarkPoolGrowAndMigrate(b *testing.B) {
	b.Run("withinLevel", func(b *testing.B) {
		pool := NewLeveledPool([]int{64, 256, 1024})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pb := pool.Get(32)
			pb.Write(bytes.Repeat([]byte("x"), 200))
			pb.Close()
		}
	})
	b.Run("beyondMaxLevel", func(b *testing.B) {
		pool := NewLeveledPool([]int{64, 256})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pb := pool.Get(32)
			pb.Write(bytes.Repeat([]byte("x"), 10000))
			pb.Close()
		}
	})
}

func BenchmarkPoolConcurrentWriters(b *testing.B) {
	pool := NewPool()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		data := []byte("concurrent write test")
		for pb.Next() {
			buf := pool.Get(128)
			buf.Write(data)
			buf.Close()
		}
	})
}

func BenchmarkBytesBufferBaseline(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		buf.WriteString("hello world")
		_ = buf.Bytes()
	}
}

func BenchmarkBytesBufferBaselineNoPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, 64))
		buf.WriteString("hello")
		_ = buf.String()
	}
}

// ============================================================================
// Example
// ============================================================================

func ExampleNewPool() {
	pool := NewPool()
	pb := pool.Get(512)
	pb.WriteString("Hello, Gedis!")
	fmt.Println(pb.String())
	pb.Close()
	// Output:
	// Hello, Gedis!
}

func ExampleNewLeveledPool() {
	pool := NewLeveledPool([]int{32, 128, 512})
	pb := pool.Get(64)
	pb.WriteString("custom pool")
	fmt.Println(pb.String())
	pb.Close()
	// Output:
	// custom pool
}

func ExamplePooledBuffer_grow() {
	pool := NewLeveledPool([]int{16, 64, 256})

	pb := pool.Get(8)
	pb.WriteString("hello ")
	pb.Write(bytes.Repeat([]byte("x"), 80))
	pb.WriteString(" world")

	fmt.Println(pb.Len() > 80 && bytes.HasPrefix(pb.Bytes(), []byte("hello ")))
	pb.Close()
	// Output:
	// true
}
