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

import "sync"

// RedisDB 是嵌入式 Redis-like 内存数据库的主结构。
// 所有数据存储在 Arena 中，通过 Dict 索引，sync.RWMutex 保证并发安全。
type RedisDB struct {
	arena *Arena
	dict  *Dict
	mu    sync.RWMutex
}

// New 创建一个新的 RedisDB 实例。
func New() *RedisDB {
	arena := NewArena(0)
	dict := NewDict(arena)
	return &RedisDB{
		arena: arena,
		dict:  dict,
	}
}

// Del 删除指定 key 及其关联的所有数据。
func (db *RedisDB) Del(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	db.FreeObject(headOff)
	return db.dict.Del(keyBytes)
}

// Exists 检查 key 是否存在。
func (db *RedisDB) Exists(key string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, ok := db.dict.Get([]byte(key))
	return ok
}

// FlushAll 清空所有数据，重置 Arena 和 Dict。
func (db *RedisDB) FlushAll() {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.arena.Reset()
	db.dict = NewDict(db.arena)
}
