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

func TestDumpRestoreBasic(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("dump_key", []byte("hello world"))

	data, ok := db.Dump("dump_key")
	if !ok {
		t.Fatal("Dump failed")
	}
	if len(data) == 0 {
		t.Fatal("Dump returned empty data")
	}

	ok = db.Restore("restore_key", 0, data, false)
	if !ok {
		t.Fatal("Restore failed")
	}

	val, ok := db.Get("restore_key")
	if !ok {
		t.Fatal("Get restored key failed")
	}
	if val.String() != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", val.String())
	}
	val.Close()
}

func TestDumpRestoreWithTTL(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("dump_key", []byte("with ttl"))

	data, ok := db.Dump("dump_key")
	if !ok {
		t.Fatal("Dump failed")
	}

	ok = db.Restore("restore_key", 1000, data, false)
	if !ok {
		t.Fatal("Restore with TTL failed")
	}

	val, ok := db.Get("restore_key")
	if !ok {
		t.Fatal("Get restored key failed")
	}
	if val.String() != "with ttl" {
		t.Errorf("Expected 'with ttl', got '%s'", val.String())
	}
	val.Close()
}

func TestDumpNonexistent(t *testing.T) {
	db := New()
	defer db.FlushAll()

	data, ok := db.Dump("nonexistent")
	if ok || data != nil {
		t.Fatal("Dump should fail for nonexistent key")
	}
}

func TestRestoreReplace(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("existing_key", []byte("original"))
	db.Set("source_key", []byte("new value"))

	data, ok := db.Dump("source_key")
	if !ok {
		t.Fatal("Dump failed")
	}

	ok = db.Restore("existing_key", 0, data, false)
	if ok {
		t.Fatal("Restore without replace should fail")
	}

	ok = db.Restore("existing_key", 0, data, true)
	if !ok {
		t.Fatal("Restore with replace should succeed")
	}

	val, ok := db.Get("existing_key")
	if !ok {
		t.Fatal("Get key failed")
	}
	if val.String() != "new value" {
		t.Errorf("Expected 'new value', got '%s'", val.String())
	}
	val.Close()
}

func TestRenameBasic(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("old_key", []byte("value"))

	ok := db.Rename("old_key", "new_key")
	if !ok {
		t.Fatal("Rename failed")
	}

	_, ok = db.Get("old_key")
	if ok {
		t.Fatal("Old key should not exist")
	}

	val, ok := db.Get("new_key")
	if !ok {
		t.Fatal("New key should exist")
	}
	if val.String() != "value" {
		t.Errorf("Expected 'value', got '%s'", val.String())
	}
	val.Close()
}

func TestRenameNx(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("old_key", []byte("value"))
	db.Set("existing_key", []byte("existing"))

	ok := db.RenameNx("old_key", "existing_key")
	if ok {
		t.Fatal("RenameNx should fail when new key exists")
	}

	ok = db.RenameNx("old_key", "new_key")
	if !ok {
		t.Fatal("RenameNx should succeed when new key doesn't exist")
	}

	val, ok := db.Get("new_key")
	if !ok {
		t.Fatal("New key should exist")
	}
	if val.String() != "value" {
		t.Errorf("Expected 'value', got '%s'", val.String())
	}
	val.Close()
}

func TestRenameNonexistent(t *testing.T) {
	db := New()
	defer db.FlushAll()

	ok := db.Rename("nonexistent", "new_key")
	if ok {
		t.Fatal("Rename should fail for nonexistent key")
	}
}

func TestDumpRestoreBase64(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Set("base64_key", []byte("test base64"))

	data, ok := db.DumpBase64("base64_key")
	if !ok {
		t.Fatal("DumpBase64 failed")
	}
	if len(data) == 0 {
		t.Fatal("DumpBase64 returned empty string")
	}

	ok = db.RestoreBase64("restored_b64", 0, data, false)
	if !ok {
		t.Fatal("RestoreBase64 failed")
	}

	val, ok := db.Get("restored_b64")
	if !ok {
		t.Fatal("Get restored key failed")
	}
	if val.String() != "test base64" {
		t.Errorf("Expected 'test base64', got '%s'", val.String())
	}
	val.Close()
}
