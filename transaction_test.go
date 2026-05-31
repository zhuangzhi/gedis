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

import "testing"

func TestTransactionBasic(t *testing.T) {
	db := New()
	defer db.FlushAll()

	if db.InTransaction() {
		t.Fatal("Should not be in transaction initially")
	}

	db.Multi()
	if !db.InTransaction() {
		t.Fatal("Should be in transaction after MULTI")
	}

	db.QueueCommand("SET", "tx_key", [][]byte{[]byte("value")})
	db.QueueCommand("SET", "tx_key2", [][]byte{[]byte("value2")})

	if db.QueuedCommandCount() != 2 {
		t.Fatalf("Expected 2 queued commands, got %d", db.QueuedCommandCount())
	}

	db.Exec()
}

func TestTransactionDiscard(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Multi()
	db.QueueCommand("SET", "discard_key", [][]byte{[]byte("value")})

	if db.QueuedCommandCount() != 1 {
		t.Fatalf("Expected 1 queued command, got %d", db.QueuedCommandCount())
	}

	db.Discard()

	if db.InTransaction() {
		t.Fatal("Should not be in transaction after DISCARD")
	}
	if db.QueuedCommandCount() != 0 {
		t.Fatal("Queued commands should be cleared after DISCARD")
	}
}

func TestTransactionWatch(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Watch("watch_key")
	db.Unwatch()
}

func TestTransactionMultiKey(t *testing.T) {
	db := New()
	defer db.FlushAll()

	db.Multi()
	db.Multi()

	if db.QueuedCommandCount() != 0 {
		t.Fatal("Queued command count should remain 0 after multiple MULTI calls")
	}
}
