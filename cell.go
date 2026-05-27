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

// 速率限制（Cell/Rate Limiting）实现，基于令牌桶（Token Bucket）算法。
package gedis

import "time"

// ThrottleResult 速率限制检查结果。
type ThrottleResult struct {
	Allowed   bool
	Remaining int64
	RetryAfter int64
	ResetAt   int64
}

const (
	cellMetaSize = 40
)

func cellCapacity(dataOff int) int      { return dataOff }
func cellRate(dataOff int) int          { return dataOff + 8 }
func cellTokens(dataOff int) int        { return dataOff + 16 }
func cellLastRefill(dataOff int) int    { return dataOff + 24 }
func cellPeriod(dataOff int) int        { return dataOff + 32 }

func (db *RedisDB) Throttle(key string, maxBurst, rate int64, period int64) ThrottleResult {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	now := time.Now().UnixMilli()

	headOff, ok := db.dict.Get(keyBytes)

	var dataOff int
	var capacity, tokens int64
	var lastRefill int64
	var storedRate, storedPeriod int64

	if ok {
		dataOff = db.ObjectDataOffset(headOff)
		capacity = int64(db.arena.ReadUint64(cellCapacity(dataOff)))
		storedRate = int64(db.arena.ReadUint64(cellRate(dataOff)))
		tokens = int64(db.arena.ReadUint64(cellTokens(dataOff)))
		lastRefill = int64(db.arena.ReadUint64(cellLastRefill(dataOff)))
		storedPeriod = int64(db.arena.ReadUint64(cellPeriod(dataOff)))
	} else {
		capacity = maxBurst
		storedRate = rate
		storedPeriod = period
		tokens = capacity
		lastRefill = now

		dataOff = db.arena.Alloc(cellMetaSize)
		db.arena.WriteUint64(cellCapacity(dataOff), uint64(capacity))
		db.arena.WriteUint64(cellRate(dataOff), uint64(storedRate))
		db.arena.WriteUint64(cellTokens(dataOff), uint64(tokens))
		db.arena.WriteUint64(cellLastRefill(dataOff), uint64(lastRefill))
		db.arena.WriteUint64(cellPeriod(dataOff), uint64(storedPeriod))

		headOff = db.NewObject(ObjCell, ObjEncodingRaw, dataOff)
		db.dict.Set(keyBytes, headOff)
	}

	elapsed := now - lastRefill
	if elapsed > 0 {
		refillTokens := elapsed * storedRate / storedPeriod
		if refillTokens > 0 {
			tokens += refillTokens
			if tokens > capacity {
				tokens = capacity
			}
			lastRefill = now
		}
	}

	allowed := tokens > 0
	if allowed {
		tokens--
	}

	db.arena.WriteUint64(cellTokens(dataOff), uint64(tokens))
	db.arena.WriteUint64(cellLastRefill(dataOff), uint64(lastRefill))

	retryAfter := int64(0)
	if !allowed && storedRate > 0 {
		retryAfter = storedPeriod / storedRate
	}

	resetAt := now
	if tokens < capacity && storedRate > 0 {
		deficit := capacity - tokens
		resetAt = now + deficit*storedPeriod/storedRate
	}

	return ThrottleResult{
		Allowed:    allowed,
		Remaining:  tokens,
		RetryAfter: retryAfter,
		ResetAt:    resetAt,
	}
}

func (db *RedisDB) CellReset(key string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return
	}

	dataOff := db.ObjectDataOffset(headOff)
	capacity := int64(db.arena.ReadUint64(cellCapacity(dataOff)))

	db.arena.WriteUint64(cellTokens(dataOff), uint64(capacity))
	db.arena.WriteUint64(cellLastRefill(dataOff), uint64(time.Now().UnixMilli()))
}
