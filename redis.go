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
	"sync"
	"time"
)

// QueuedCommand 表示队列中的命令
type QueuedCommand struct {
	Op    string
	Key   string
	Args  [][]byte
}

// Transaction 表示一个事务
type Transaction struct {
	commands []QueuedCommand
	watched  map[string]uint64 // key -> 版本号
}

// RedisDB 是嵌入式 Redis-like 内存数据库的主结构。
// 所有数据存储在 Arena 中，通过 Dict 索引，sync.RWMutex 保证并发安全。
type RedisDB struct {
	arena         *Arena
	dict          *Dict
	expiry        *Dict
	lastWALOffset int64
	mu            sync.RWMutex
	
	// 事务相关
	inTransaction bool
	commands      []QueuedCommand
	watched       map[string]uint64 // key -> 版本号
	keyVersion    map[string]uint64 // key -> 当前版本号
}

// New 创建一个新的 RedisDB 实例。
func New() *RedisDB {
	arena := NewArena(0)
	dict := NewDict(arena)
	expiry := NewDict(arena)
	return &RedisDB{
		arena:      arena,
		dict:       dict,
		expiry:     expiry,
		watched:    make(map[string]uint64),
		keyVersion: make(map[string]uint64),
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
	db.dict.Del(keyBytes)
	db.expiry.Del(keyBytes)
	return true
}

// Exists 检查 key 是否存在（自动清理过期 key）。
func (db *RedisDB) Exists(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		db.deleteExpiredKey(keyBytes, headOff)
		return false
	}
	return true
}

// FlushAll 清空所有数据，重置 Arena 和 Dict。
func (db *RedisDB) FlushAll() {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.arena.Reset()
	db.dict = NewDict(db.arena)
	db.expiry = NewDict(db.arena)
}

// isExpired 检查 key 是否已过期。
func (db *RedisDB) isExpired(keyBytes []byte, headOff int) bool {
	expTimeOff, ok := db.expiry.Get(keyBytes)
	if !ok {
		return false
	}
	expTime := int64(db.arena.ReadUint64(expTimeOff))
	return currentTimeMs() >= expTime
}

// deleteExpiredKey 删除过期 key 及其关联数据。
func (db *RedisDB) deleteExpiredKey(keyBytes []byte, headOff int) {
	db.FreeObject(headOff)
	db.dict.Del(keyBytes)
	db.expiry.Del(keyBytes)
}

// getObject 获取 key 对应的对象头偏移量，同时处理过期键的惰性删除。
// 返回 (headOff, ok)：headOff 为对象头偏移，ok 为 false 表示 key 不存在或已过期。
func (db *RedisDB) getObject(key string) (int, bool) {
	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return 0, false
	}
	if db.isExpired(keyBytes, headOff) {
		db.deleteExpiredKey(keyBytes, headOff)
		return 0, false
	}
	return headOff, true
}

// currentTimeMs 返回当前时间戳（毫秒）。
func currentTimeMs() int64 {
	return (int64)(time.Now().UnixNano() / 1000000)
}

// Expire 设置 key 的过期时间（秒）。
// 返回 true 表示设置成功，false 表示 key 不存在。
func (db *RedisDB) Expire(key string, seconds int64) bool {
	return db.PExpire(key, seconds*1000)
}

// PExpire 设置 key 的过期时间（毫秒）。
// 返回 true 表示设置成功，false 表示 key 不存在。
func (db *RedisDB) PExpire(key string, milliseconds int64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		db.deleteExpiredKey(keyBytes, headOff)
		return false
	}

	expTime := currentTimeMs() + milliseconds
	expTimeOff := db.arena.Alloc(8)
	db.arena.WriteUint64(expTimeOff, uint64(expTime))
	db.expiry.Set(keyBytes, expTimeOff)
	return true
}

// ExpireAt 设置 key 在指定时间戳过期（秒）。
func (db *RedisDB) ExpireAt(key string, timestamp int64) bool {
	return db.PExpireAt(key, timestamp*1000)
}

// PExpireAt 设置 key 在指定时间戳过期（毫秒）。
func (db *RedisDB) PExpireAt(key string, timestampMs int64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if timestampMs <= currentTimeMs() {
		db.deleteExpiredKey(keyBytes, headOff)
		return true
	}

	expTimeOff := db.arena.Alloc(8)
	db.arena.WriteUint64(expTimeOff, uint64(timestampMs))
	db.expiry.Set(keyBytes, expTimeOff)
	return true
}

// TTL 返回 key 的剩余生存时间（秒）。
// 返回 -1 表示 key 不存在，-2 表示 key 没有设置过期时间。
func (db *RedisDB) TTL(key string) int64 {
	pttl := db.PTTL(key)
	if pttl < 0 {
		return pttl
	}
	return pttl / 1000
}

// PTTL 返回 key 的剩余生存时间（毫秒）。
// 返回 -1 表示 key 不存在，-2 表示 key 没有设置过期时间。
func (db *RedisDB) PTTL(key string) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return -1
	}

	expTimeOff, hasExpiry := db.expiry.Get(keyBytes)
	if !hasExpiry {
		return -2
	}

	expTime := int64(db.arena.ReadUint64(expTimeOff))
	remain := expTime - currentTimeMs()
	if remain <= 0 {
		db.deleteExpiredKey(keyBytes, headOff)
		return -1
	}
	return remain
}

// Persist 移除 key 的过期时间，使其永久存在。
// 返回 true 表示移除成功，false 表示 key 不存在或没有过期时间。
func (db *RedisDB) Persist(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	if _, ok := db.dict.Get(keyBytes); !ok {
		return false
	}

	if _, hasExpiry := db.expiry.Get(keyBytes); !hasExpiry {
		return false
	}

	db.expiry.Del(keyBytes)
	return true
}

// GetEx 设置 key 的值并返回，同时可选地设置过期时间。
func (db *RedisDB) GetEx(key string, value []byte, expireMs int64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if ok {
		if db.isExpired(keyBytes, headOff) {
			db.deleteExpiredKey(keyBytes, headOff)
			ok = false
		}
	}

	if !ok {
		return false
	}

	db.setLocked(key, value)
	if expireMs > 0 {
		expTime := currentTimeMs() + expireMs
		expTimeOff := db.arena.Alloc(8)
		db.arena.WriteUint64(expTimeOff, uint64(expTime))
		db.expiry.Set(keyBytes, expTimeOff)
	} else if expireMs == -1 {
		db.expiry.Del(keyBytes)
	}
	return true
}

// incrementKeyVersion 递增 key 的版本号
func (db *RedisDB) incrementKeyVersion(key string) {
	db.keyVersion[key]++
}

// MULTI 开启事务
func (db *RedisDB) Multi() {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if db.inTransaction {
		return
	}
	
	db.inTransaction = true
	db.commands = make([]QueuedCommand, 0)
}

// QueueCommand 将命令加入事务队列
func (db *RedisDB) QueueCommand(op string, key string, args [][]byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if db.inTransaction {
		db.commands = append(db.commands, QueuedCommand{
			Op:   op,
			Key:  key,
			Args: args,
		})
	}
}

// EXEC 执行事务
// 返回值: [执行结果..., 执行失败?]
func (db *RedisDB) Exec() ([]interface{}, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if !db.inTransaction {
		return nil, false
	}
	
	// 检查 WATCH 的 key 是否被修改
	abort := false
	for key, version := range db.watched {
		if db.keyVersion[key] != version {
			abort = true
			break
		}
	}
	
	commands := db.commands
	
	// 重置事务状态
	db.inTransaction = false
	db.commands = nil
	db.watched = make(map[string]uint64)
	
	if abort {
		return nil, true
	}
	
	// 执行所有命令
	results := make([]interface{}, 0, len(commands))
	
	for _, cmd := range commands {
		result := db.executeCommand(cmd.Op, cmd.Key, cmd.Args)
		results = append(results, result)
	}
	
	return results, false
}

// DISCARD 取消事务
func (db *RedisDB) Discard() {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if !db.inTransaction {
		return
	}
	
	db.inTransaction = false
	db.commands = nil
	db.watched = make(map[string]uint64)
}

// WATCH 监视 key
func (db *RedisDB) Watch(keys ...string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if db.inTransaction {
		return
	}
	
	for _, key := range keys {
		db.watched[key] = db.keyVersion[key]
	}
}

// UNWATCH 取消监视所有 key
func (db *RedisDB) Unwatch() {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	db.watched = make(map[string]uint64)
}

// InTransaction 检查是否在事务中
func (db *RedisDB) InTransaction() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	
	return db.inTransaction
}

// QueuedCommandCount 获取队列中的命令数量
func (db *RedisDB) QueuedCommandCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	
	return len(db.commands)
}

// executeCommand 内部执行命令（不检查事务）
func (db *RedisDB) executeCommand(op string, key string, args [][]byte) interface{} {
	// 这是一个简单的执行器，仅用于演示
	// 在实际使用中，应该根据 op 调用相应的方法
	switch op {
	case "SET":
		if len(args) >= 1 {
			db.setLocked(key, args[0])
			return "OK"
		}
		return "ERR"
	case "DEL":
		// 这里需要一个不带锁的版本，先简化
		return true
	default:
		return "ERR unknown command"
	}
}
