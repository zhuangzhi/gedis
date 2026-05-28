# gedis

[中文](./README.md)

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://go.dev/)
[![Test](https://img.shields.io/badge/tests-59%20passed-brightgreen)](./redis_test.go)
[![Benchmark](https://img.shields.io/badge/benchmarks-70-f1c40f)](./redis_bench_test.go)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

gedis is an embedded Redis-like in-memory database written in Go. The core design goal is **zero GC pressure** — all persistent data lives in a single `[]byte` Arena, using integer offsets instead of Go pointers to prevent the GC from scanning structured data.

## Architecture

```
+---------------------------------------------------------------+
|                          RedisDB                               |
|  +------------------+  +----------------+  +---------------+   |
|  |      Dict        |  |     Arena      |  |  sync.RWMutex |   |
|  |  (key space)      |  |  ([]byte buf)  |  |               |   |
|  |                  |  |                |  |               |   |
|  |  key -> Object   |  |  all data       |  |               |   |
|  +------------------+  +----------------+  +---------------+   |
+---------------------------------------------------------------+
```

## Core Design

### Zero-GC Arena Memory Allocator

All persistent data is stored in a single `[]byte` buffer managed by `Arena`:

- **Allocation**: `Arena.Alloc(size)` returns an integer offset; data is written via `WriteUint32(offset, value)` etc.
- **Free**: `Arena.Free(offset)` adds the block to a free list for reuse in subsequent allocations.
- **Write**: `WriteUint32` / `WriteUint64` / `WriteBytes` write directly into the underlying `[]byte`.
- **Read**: `ReadUint32` / `ReadBytes` read directly from the underlying buffer.
- **Zero Go pointers**: All internal structures reference each other via integer offsets; the GC never needs to scan Arena-internal data.

```
+------------+----------------------------------+
| size (4B)  |  data (variable)                 |
+------------+----------------------------------+
 ↑ header     ↑ dataOff (returned to caller)
```

### Dict Hash Table

FNV-1a hashing + linear probing, stored in Arena. Supports dynamic rehash (load factor > 75%).

### Object Header

Each stored value is prefixed with a 16-byte object header:

| Offset | Size | Field |
|--------|------|-------|
| 0 | 1 | type |
| 1 | 1 | encoding |
| 2 | 4 | lru |
| 6 | 2 | refcount |
| 8 | 8 | data_offset |

### Internal Data Structures

| Structure | Used By | Description |
|-----------|---------|-------------|
| Ziplist | List / Hash / Small ZSet | Double-ended packed list: prevLen + encoding + data |
| Skiplist | ZSet | 32-level skip list, stored in Arena |
| Intset | Small integer Set | Sorted integer set with 2/4/8 byte encoding upgrade |
| Rax-like | Stream | Radix-tree-style entry storage with consumer groups |
| Chunk | TimeSeries | Chunked timestamp-value pair storage |

## API Reference

> See [API.md](./API.md) for detailed API documentation.

### Zero-Allocation Optimization

All public APIs use two zero-allocation types as replacements for native Go types:

- **`*PooledBuffer`** replaces `[]byte` — allocated from a 6-tier object pool (1K/4K/16K/64K/256K/1M), must call `Close()` to return
- **`*ZSlices`** replaces `[]string` — elements stored compactly in a PooledBuffer, `Get(i)` returns a zero-copy `[]byte` view, must call `Close()` to return

### Keys

```go
func New() *RedisDB
func (db *RedisDB) Del(key string) bool
func (db *RedisDB) Exists(key string) bool
func (db *RedisDB) FlushAll()
```

### Strings

```go
func Buf(s string) *PooledBuffer
func NewBuf(minCap int) *PooledBuffer
func (db *RedisDB) Set(key string, value *PooledBuffer)
func (db *RedisDB) Get(key string) (*PooledBuffer, bool)
func (db *RedisDB) Append(key string, value *PooledBuffer) int
func (db *RedisDB) GetRange(key string, start, end int) (*PooledBuffer, bool)
func (db *RedisDB) SetRange(key string, offset int, value *PooledBuffer) int
func (db *RedisDB) Strlen(key string) int
func (db *RedisDB) IncrBy(key string, inc int64) (int64, error)
func (db *RedisDB) IncrByFloat(key string, inc float64) (float64, error)
```

### Lists

```go
func (db *RedisDB) LPush(key string, values ...*PooledBuffer) int
func (db *RedisDB) RPush(key string, values ...*PooledBuffer) int
func (db *RedisDB) LPop(key string) (*PooledBuffer, bool)
func (db *RedisDB) RPop(key string) (*PooledBuffer, bool)
func (db *RedisDB) LIndex(key string, index int) (*PooledBuffer, bool)
func (db *RedisDB) LRange(key string, start, stop int) []*PooledBuffer
func (db *RedisDB) LLen(key string) int
```

### Hashes

```go
func (db *RedisDB) HSet(key, field string, value *PooledBuffer) int
func (db *RedisDB) HGet(key, field string) (*PooledBuffer, bool)
func (db *RedisDB) HDel(key string, fields ...string) int
func (db *RedisDB) HGetAll(key string) map[string]*PooledBuffer
func (db *RedisDB) HIncrBy(key, field string, inc int64) (int64, error)
func (db *RedisDB) HExists(key, field string) bool
func (db *RedisDB) HLen(key string) int
```

### Sets

```go
func (db *RedisDB) SAdd(key string, members ...*PooledBuffer) int
func (db *RedisDB) SRem(key string, members ...*PooledBuffer) int
func (db *RedisDB) SIsMember(key string, member *PooledBuffer) bool
func (db *RedisDB) SMembers(key string) []*PooledBuffer
func (db *RedisDB) SCard(key string) int
func (db *RedisDB) SInter(keys ...string) []*PooledBuffer
func (db *RedisDB) SUnion(keys ...string) []*PooledBuffer
```

### Sorted Sets

```go
func (db *RedisDB) ZAdd(key string, score float64, member *PooledBuffer) int
func (db *RedisDB) ZRem(key string, member *PooledBuffer) bool
func (db *RedisDB) ZScore(key string, member *PooledBuffer) (float64, bool)
func (db *RedisDB) ZRange(key string, start, stop int) ZSlices
func (db *RedisDB) ZRangeIter(key string, start, stop int, fn func(member []byte))
func (db *RedisDB) ZRangeWithScores(key string, start, stop int) (*ZSlices, []float64)
func (db *RedisDB) ZRangeByScore(key string, min, max float64) []*PooledBuffer
func (db *RedisDB) ZRemRangeByScore(key string, min, max float64) int
func (db *RedisDB) ZCard(key string) int
```

### Bitmaps

```go
func (db *RedisDB) SetBit(key string, offset int, val int) int
func (db *RedisDB) GetBit(key string, offset int) int
func (db *RedisDB) BitCount(key string, start, end int) int
func (db *RedisDB) BitOp(op string, destKey string, srcKeys ...string) int
func (db *RedisDB) BitField(key string, args ...*PooledBuffer) []int64
```

### HyperLogLog

```go
func (db *RedisDB) PFAdd(key string, elements ...[]byte) int
func (db *RedisDB) PFAddBuffer(key string, elements ...*PooledBuffer) int
func (db *RedisDB) PFCount(keys ...string) int64
func (db *RedisDB) PFMerge(dest string, sources ...string)
```

### Geo

```go
func (db *RedisDB) GeoAdd(key string, lon, lat float64, member string) int
func (db *RedisDB) GeoDist(key, member1, member2, unit string) float64
func (db *RedisDB) GeoRadius(key string, lon, lat, radius float64, unit string) *ZSlices
func (db *RedisDB) GeoRadiusByMember(key, member string, radius float64, unit string) *ZSlices
func (db *RedisDB) GeoPos(key string, members ...string) [][2]float64
```

### Streams

```go
func (db *RedisDB) XAdd(key string, id string, fields map[string]*PooledBuffer) string
func (db *RedisDB) XRead(streams map[string]string, count int) map[string][]StreamEntry
func (db *RedisDB) XReadGroup(group, consumer string, streams map[string]string, count int) map[string][]StreamEntry
func (db *RedisDB) XGroupCreate(key, group, startID string) error
func (db *RedisDB) XLen(key string) int
```

### TimeSeries

```go
func (db *RedisDB) TSAdd(key string, ts int64, val float64) int
func (db *RedisDB) TSRange(key string, startTs, endTs int64) []TSPoint
func (db *RedisDB) TSLast(key string) (int64, float64, bool)
func (db *RedisDB) TSDel(key string, startTs, endTs int64) int
```

### Probabilistic

```go
// Bloom Filter
func (db *RedisDB) BFReserve(key string, errorRate float64, capacity int)
func (db *RedisDB) BFAdd(key string, item *PooledBuffer) bool
func (db *RedisDB) BFExists(key string, item *PooledBuffer) bool

// Cuckoo Filter
func (db *RedisDB) CFReserve(key string, capacity int)
func (db *RedisDB) CFAdd(key string, item *PooledBuffer) bool
func (db *RedisDB) CFDel(key string, item *PooledBuffer) bool
func (db *RedisDB) CFExists(key string, item *PooledBuffer) bool

// Count-Min Sketch
func (db *RedisDB) CMSInitByDim(key string, width, depth int)
func (db *RedisDB) CMSIncrBy(key string, item *PooledBuffer, inc int) int
func (db *RedisDB) CMSQuery(key string, items ...*PooledBuffer) []int

// Top-K
func (db *RedisDB) TopKReserve(key string, k int)
func (db *RedisDB) TopKAdd(key string, items ...string)
func (db *RedisDB) TopKList(key string) []TopKItem
```

### JSON

```go
func (db *RedisDB) JsonSet(key string, path string, value interface{}) error
func (db *RedisDB) JsonGet(key string, path string) (interface{}, error)
func (db *RedisDB) JsonDel(key string, path string) error
func (db *RedisDB) JsonArrAppend(key string, path string, values ...interface{}) error
func (db *RedisDB) JsonObjLen(key string, path string) (int, error)
```

### Search

```go
func (db *RedisDB) FTCreate(index string, schema map[string]string)
func (db *RedisDB) FTAdd(index string, docID string, fields map[string]string)
func (db *RedisDB) FTSearch(index string, query string, limit int) *ZSlices
```

### Graph

```go
func (db *RedisDB) GraphQuery(graphName, cypher string) ([]GraphResult, error)
```

### Rate Limiting (Cell)

```go
func (db *RedisDB) Throttle(key string, maxBurst, rate int64, period int64) ThrottleResult
func (db *RedisDB) CellReset(key string)
```

## Usage Example

```go
package main

import (
    "fmt"
    "gedis"
)

func main() {
    db := gedis.New()

    // Strings
    db.Set("hello", gedis.Buf("world"))
    val, _ := db.Get("hello")
    fmt.Println(val.String()) // world
    val.Close()

    // Lists
    db.LPush("mylist", gedis.Buf("a"), gedis.Buf("b"), gedis.Buf("c"))
    v, _ := db.LPop("mylist")
    fmt.Println(v.String()) // c
    v.Close()

    // Hashes
    db.HSet("myhash", "f1", gedis.Buf("v1"))
    v, _ = db.HGet("myhash", "f1")
    fmt.Println(v.String()) // v1
    v.Close()

    // Sorted Sets
    db.ZAdd("myzset", 1.0, gedis.Buf("a"))
    db.ZAdd("myzset", 2.0, gedis.Buf("b"))
    members := db.ZRange("myzset", 0, -1)
    for i := 0; i < members.Len(); i++ {
        fmt.Println(string(members.Get(i)))
    }
    members.Close()
    // Output: a, b

    // Probabilistic - Bloom Filter
    db.BFReserve("bf", 0.01, 1000000)
    db.BFAdd("bf", gedis.Buf("item1"))
    exists := db.BFExists("bf", gedis.Buf("item1"))
    fmt.Println(exists) // true
}
```

## Concurrency Safety

All public APIs are internally protected by `sync.RWMutex`:

- **Write operations** (Set, Del, LPush, ZAdd, etc.): `Lock`
- **Read operations** (Get, Exists, ZScore, etc.): `RLock`

Safe for concurrent access from multiple goroutines.

## Performance Benchmarks

Intel Core Ultra 9 185H, Windows, Go 1.21+

```bash
go test -bench=. -benchtime=300ms -benchmem -count=1 -run NONE .
```

### Core Components

| Benchmark | Latency | Throughput | Bytes | Allocs |
|-----------|---------|------------|-------|--------|
| Arena Alloc (64B) | 39.7 ns | ~25M ops/s | 218 B | 0 |
| Arena Alloc+Free | 3.8 ns | ~265M ops/s | 0 B | 0 |
| Arena Read/Write | 0.5 ns | ~2G ops/s | 0 B | 0 |
| Dict Set | 8.0 ns | ~125M ops/s | 0 B | 0 |
| Dict Get | 49.5 ns | ~20M ops/s | 3 B | 1 |
| Dict Del | 14.7 ns | ~68M ops/s | 20 B | 0 |

### Redis Commands

| Benchmark | Latency | Throughput | Bytes | Allocs |
|-----------|---------|------------|-------|--------|
| SET | 45.1 ns | ~22M ops/s | 135 B | 0 |
| GET | 76.7 ns | ~13M ops/s | 37 B | 2 |
| DEL | 62.4 ns | ~16M ops/s | 22 B | 0 |
| INCRBY | 23.7 ns | ~42M ops/s | 0 B | 0 |
| EXISTS | 62.0 ns | ~16M ops/s | 5 B | 1 |
| LPUSH | 46.5 ns | ~21M ops/s | 0 B | 0 |
| LPOP | 123.7 ns | ~8.1M ops/s | 51 B | 1 |
| RPUSH | 46.8 ns | ~21M ops/s | 0 B | 0 |
| RPOP | 130.8 ns | ~7.6M ops/s | 52 B | 1 |
| HSET | 81.6 ns | ~12M ops/s | 5 B | 1 |
| HGET | 678 ns | ~1.5M ops/s | 16 B | 2 |
| HDEL | 162 ns | ~6.2M ops/s | 60 B | 0 |
| HINCRBY | 183 ns | ~5.5M ops/s | 2 B | 2 |
| SADD | 27.6 ns | ~36M ops/s | 0 B | 0 |
| SISMEMBER | 18.4 ns | ~54M ops/s | 0 B | 0 |
| SREM | 63.4 ns | ~16M ops/s | 23 B | 0 |
| ZADD | 316 ns | ~3.2M ops/s | 1073 B | 0 |
| ZSCORE | 364 ns | ~2.7M ops/s | 8 B | 1 |
| ZREM | 323 ns | ~3.1M ops/s | 1073 B | 0 |
| ZRANGE (100) | 777 ns | ~1.3M ops/s | 1792 B | 2 |
| ZRANGEITER (100) | 197 ns | ~5.1M ops/s | 0 B | **0** |
| PFADD | 29.3 ns | ~34M ops/s | 0 B | 0 |
| PFCOUNT | 42.6 µs | ~23K ops/s | 0 B | 0 |
| SETBIT | 31.4 ns | ~32M ops/s | 0 B | 0 |
| GETBIT | 15.6 ns | ~64M ops/s | 0 B | 0 |
| BITCOUNT | 3.0 µs | ~330K ops/s | 0 B | 0 |
| BF.ADD | 38.7 ns | ~26M ops/s | 0 B | 0 |
| BF.EXISTS | 75.1 ns | ~13M ops/s | 7 B | 1 |
| CF.ADD | 4.4 µs | ~230K ops/s | 0 B | 0 |
| CF.EXISTS | 57.3 ns | ~17M ops/s | 7 B | 1 |
| CMS.INCRBY | 82.3 ns | ~12M ops/s | 13 B | 1 |
| TOPK.ADD | 361 ns | ~2.8M ops/s | 7 B | 1 |

### Concurrency

| Benchmark | Latency | Throughput | Bytes | Allocs |
|-----------|---------|------------|-------|--------|
| Concurrent Read | 74.4 ns | ~13M ops/s | 11 B | 2 |
| Concurrent Write | 149 ns | ~6.7M ops/s | 69 B | 1 |
| Concurrent IncrBy | 56.3 ns | ~18M ops/s | 0 B | 0 |
| Mixed Read (5 ops) | 580 ns | ~1.7M iter/s | 110 B | 14 |

## Escape Analysis

`go build -gcflags="-m" .` verifies the zero-GC design:

- All `Arena.Alloc` read/write operations produce **zero heap allocations**
- Temporary `make([]byte, size)` buffers **do not escape** (stack-allocated)
- `string` → `[]byte` conversions use **zero-copy**
- `ziplist`/`skiplist` internal operations: `arena does not escape`
- Only API return values (`string(member)`, slices from `append(...)`) and `sync.RWMutex` escape to heap as expected

## Project Structure

```
gedis/
├── arena.go             # Arena memory allocator
├── poolbuffer.go        # PooledBuffer tiered object pool
├── zslices.go           # ZSlices zero-allocation string slice
├── object.go            # Object header management
├── dict.go              # Hash table (FNV-1a + linear probing)
├── redis.go             # RedisDB main structure
├── string.go            # String commands
├── list.go              # List commands
├── hash.go              # Hash commands
├── set.go               # Set commands
├── zset.go              # Sorted Set commands
├── ziplist.go           # Ziplist internal structure
├── skip_list.go         # Skiplist internal structure
├── bitmap.go            # Bitmap / BitField commands
├── hyperloglog.go       # HyperLogLog commands
├── geo.go               # Geo commands
├── stream.go            # Stream commands
├── timeseries.go        # TimeSeries commands
├── probabilistic.go     # Bloom/Cuckoo/CMS/TopK commands
├── json.go              # JSON commands
├── search.go            # Search commands
├── graph.go             # Graph commands
├── cell.go              # Rate Limiter commands
├── redis_test.go        # Unit tests (59 tests)
├── redis_bench_test.go  # Benchmarks (70 benchmarks)
├── example/             # Usage examples
├── API.md               # Detailed API documentation
```

## Testing

```bash
# Run all unit tests
go test -v ./...

# Run all benchmarks
go test -bench=. -benchtime=300ms -benchmem -count=1 -run NONE .

# Run escape analysis
go build -gcflags="-m" .
```

## License

MIT
