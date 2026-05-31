// MIT License
//
// Copyright (c) 2026 Gedis Authors
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package gedis

import (
	"testing"
)

func TestArrayCreate(t *testing.T) {
	db := New()
	defer db.FlushAll()

	ok := db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))
	if !ok {
		t.Fatal("ArrayCreate failed")
	}

	length := db.ArrayLen("myarray")
	if length != 3 {
		t.Errorf("Expected length 3, got %d", length)
	}
}

func TestArrayGetSet(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.ArrayGet("myarray", 1)
	if !ok {
		t.Fatal("ArrayGet failed")
	}
	if val.String() != "b" {
		t.Errorf("Expected 'b', got '%s'", val.String())
	}
	val.Close()

	ok = db.ArraySet("myarray", 1, []byte("x"))
	if !ok {
		t.Fatal("ArraySet failed")
	}

	val, ok = db.ArrayGet("myarray", 1)
	if !ok {
		t.Fatal("ArrayGet after set failed")
	}
	if val.String() != "x" {
		t.Errorf("Expected 'x', got '%s'", val.String())
	}
	val.Close()
}

func TestArrayAppend(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"))

	idx := db.ArrayAppend("myarray", []byte("c"))
	if idx != 2 {
		t.Errorf("Expected index 2, got %d", idx)
	}

	length := db.ArrayLen("myarray")
	if length != 3 {
		t.Errorf("Expected length 3, got %d", length)
	}
}

func TestArrayPop(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.ArrayPop("myarray")
	if !ok {
		t.Fatal("ArrayPop failed")
	}
	if val.String() != "c" {
		t.Errorf("Expected 'c', got '%s'", val.String())
	}
	val.Close()

	length := db.ArrayLen("myarray")
	if length != 2 {
		t.Errorf("Expected length 2, got %d", length)
	}
}

func TestArrayShift(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	val, ok := db.ArrayShift("myarray")
	if !ok {
		t.Fatal("ArrayShift failed")
	}
	if val.String() != "a" {
		t.Errorf("Expected 'a', got '%s'", val.String())
	}
	val.Close()

	length := db.ArrayLen("myarray")
	if length != 2 {
		t.Errorf("Expected length 2, got %d", length)
	}
}

func TestArrayInsert(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("c"))

	ok := db.ArrayInsert("myarray", 1, []byte("b"))
	if !ok {
		t.Fatal("ArrayInsert failed")
	}

	val, ok := db.ArrayGet("myarray", 1)
	if !ok {
		t.Fatal("ArrayGet failed")
	}
	if val.String() != "b" {
		t.Errorf("Expected 'b', got '%s'", val.String())
	}
	val.Close()
}

func TestArrayDel(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	ok := db.ArrayDel("myarray", 1)
	if !ok {
		t.Fatal("ArrayDel failed")
	}

	length := db.ArrayLen("myarray")
	if length != 2 {
		t.Errorf("Expected length 2, got %d", length)
	}

	val, ok := db.ArrayGet("myarray", 0)
	if !ok {
		t.Fatal("ArrayGet failed")
	}
	if val.String() != "a" {
		t.Errorf("Expected 'a', got '%s'", val.String())
	}
	val.Close()
}

func TestArrayRange(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"), []byte("d"))

	values, ok := db.ArrayRange("myarray", 1, 2)
	if !ok {
		t.Fatal("ArrayRange failed")
	}
	if len(values) != 2 {
		t.Fatalf("Expected 2 values, got %d", len(values))
	}
	if values[0].String() != "b" {
		t.Errorf("Expected 'b', got '%s'", values[0].String())
	}
	if values[1].String() != "c" {
		t.Errorf("Expected 'c', got '%s'", values[1].String())
	}
	for _, v := range values {
		v.Close()
	}
}

func TestArrayContains(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	contains := db.ArrayContains("myarray", []byte("b"))
	if !contains {
		t.Error("Expected contains 'b' to be true")
	}

	contains = db.ArrayContains("myarray", []byte("x"))
	if contains {
		t.Error("Expected contains 'x' to be false")
	}
}

func TestArrayIndexOf(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	idx := db.ArrayIndexOf("myarray", []byte("b"))
	if idx != 1 {
		t.Errorf("Expected index 1, got %d", idx)
	}

	idx = db.ArrayIndexOf("myarray", []byte("x"))
	if idx != -1 {
		t.Errorf("Expected index -1, got %d", idx)
	}
}

func TestArraySort(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("c"), []byte("a"), []byte("b"))

	ok := db.ArraySort("myarray", false)
	if !ok {
		t.Fatal("ArraySort failed")
	}

	val, ok := db.ArrayGet("myarray", 0)
	if !ok {
		t.Fatal("ArrayGet failed")
	}
	if val.String() != "a" {
		t.Errorf("Expected 'a', got '%s'", val.String())
	}
	val.Close()
}

func TestArrayReverse(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	ok := db.ArrayReverse("myarray")
	if !ok {
		t.Fatal("ArrayReverse failed")
	}

	val, ok := db.ArrayGet("myarray", 0)
	if !ok {
		t.Fatal("ArrayGet failed")
	}
	if val.String() != "c" {
		t.Errorf("Expected 'c', got '%s'", val.String())
	}
	val.Close()
}

func TestArrayClear(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"))

	ok := db.ArrayClear("myarray")
	if !ok {
		t.Fatal("ArrayClear failed")
	}

	length := db.ArrayLen("myarray")
	if length != 0 {
		t.Errorf("Expected length 0, got %d", length)
	}
}

func TestArrayTrim(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.ArrayCreate("myarray", []byte("a"), []byte("b"), []byte("c"), []byte("d"))

	ok := db.ArrayTrim("myarray", 1, 2)
	if !ok {
		t.Fatal("ArrayTrim failed")
	}

	length := db.ArrayLen("myarray")
	if length != 2 {
		t.Errorf("Expected length 2, got %d", length)
	}
}
