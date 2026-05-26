# gedis

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://go.dev/)
[![Test](https://img.shields.io/badge/tests-42%20passed-brightgreen)](./redis_test.go)
[![Benchmark](https://img.shields.io/badge/benchmarks-42-f1c40f)](./redis_bench_test.go)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

gedis 是一个嵌入式 Redis-like 内存数据库，Go 语言实现。核心设计目标是 **零 GC 压力** —— 所有持久化数据存储在单一 `[]byte` Arena 中，使用整数偏移量替代 Go 指针，避免 GC 扫描结构化数据。

## 架构

```
+---------------------------------------------------------------+
|                          RedisDB                               |
|  +------------------+  +----------------+  +---------------+   |
|  |      Dict        |  |     Arena      |  |  sync.RWMutex |   |
|  |  (key space)      |  |  ([]byte buf)  |  |               |   |
|  |                  |  |                |  |               |   |
|  |  key -> Object   |  |  所有数据存储   |  |               |   |
|  +------------------+  +----------------+  +---------------+   |
+---------------------------------------------------------------+
```

## 核心设计

### Zero-GC Arena 内存管理

所有持久化数据存储在 `Arena` 的单块 `[]byte` 缓冲区中：

- **分配**：`Arena.Alloc(size)` 返回整数偏移量，数据通过 `WriteUint32(offset, value)` 等方法写入
- **释放**：`Arena.Free(offset)` 将块加入 free list，后续分配会优先复用
- **写入**：`WriteUint32/WriteUint64/WriteBytes` 直接将数据写入底层 `[]byte`
- **读取**：`ReadUint32/ReadBytes` 从底层缓冲区直接读取
- **零 Go 指针**：内部结构之间全部使用整数偏移量引用，GC 无需扫描 Arena 内部数据

```
+------------+----------------------------------+
| size (4B)  |  data (variable)                 |
+------------+----------------------------------+
 ↑ header     ↑ dataOff (返回给调用方)
```

### Dict 哈希表

FNV-1a 哈希 + 线性探测，在 Arena 中存储。支持动态 rehash（负载因子 > 75%）。

### Object 头

每个存储值前有一个 16 字节对象头：

| 偏移 | 大小 | 字段 |
|------|------|------|
| 0 | 1 | type |
| 1 | 1 | encoding |
| 2 | 4 | lru |
| 6 | 2 | refcount |
| 8 | 8 | data_offset |

### 内部数据结构

| 结构 | 用途 | 说明 |
|------|------|------|
| Ziplist | List / Hash / 小 ZSet | 双端紧凑列表，prevLen + encoding + data |
| Skiplist | ZSet | 32 级跳跃表，存储于 Arena |
| Intset | 小整数 Set | 有序整数集合，支持 2/4/8 字节编码升级 |
| Rax-like | Stream | 基数树风格条目存储，含消费者组 |
| Chunk | TimeSeries | 分块时间-值对存储 |

## API 参考

### Keys

```go
func New() *RedisDB
func (db *RedisDB) Del(key string) bool
func (db *RedisDB) Exists(key string) bool
func (db *RedisDB) FlushAll()
```

### Strings

```go
func (db *RedisDB) Set(key string, value []byte)
func (db *RedisDB) Get(key string) ([]byte, bool)
func (db *RedisDB) Append(key string, value []byte) int
func (db *RedisDB) GetRange(key string, start, end int) []byte
func (db *RedisDB) SetRange(key string, offset int, value []byte) int
func (db *RedisDB) Strlen(key string) int
func (db *RedisDB) IncrBy(key string, inc int64) (int64, error)
func (db *RedisDB) IncrByFloat(key string, inc float64) (float64, error)
```

### Lists

```go
func (db *RedisDB) LPush(key string, values ...[]byte) int
func (db *RedisDB) RPush(key string, values ...[]byte) int
func (db *RedisDB) LPop(key string) ([]byte, bool)
func (db *RedisDB) RPop(key string) ([]byte, bool)
func (db *RedisDB) LIndex(key string, index int) ([]byte, bool)
func (db *RedisDB) LRange(key string, start, stop int) [][]byte
func (db *RedisDB) LLen(key string) int
```

### Hashes

```go
func (db *RedisDB) HSet(key, field string, value []byte) int
func (db *RedisDB) HGet(key, field string) ([]byte, bool)
func (db *RedisDB) HDel(key string, fields ...string) int
func (db *RedisDB) HGetAll(key string) map[string][]byte
func (db *RedisDB) HIncrBy(key, field string, inc int64) (int64, error)
func (db *RedisDB) HExists(key, field string) bool
func (db *RedisDB) HLen(key string) int
```

### Sets

```go
func (db *RedisDB) SAdd(key string, members ...[]byte) int
func (db *RedisDB) SRem(key string, members ...[]byte) int
func (db *RedisDB) SIsMember(key string, member []byte) bool
func (db *RedisDB) SMembers(key string) [][]byte
func (db *RedisDB) SCard(key string) int
func (db *RedisDB) SInter(keys ...string) [][]byte
func (db *RedisDB) SUnion(keys ...string) [][]byte
```

### Sorted Sets

```go
func (db *RedisDB) ZAdd(key string, score float64, member []byte) int
func (db *RedisDB) ZRem(key string, member []byte) bool
func (db *RedisDB) ZScore(key string, member []byte) (float64, bool)
func (db *RedisDB) ZRange(key string, start, stop int) [][]byte
func (db *RedisDB) ZRangeWithScores(key string, start, stop int) ([]string, []float64)
func (db *RedisDB) ZRangeByScore(key string, min, max float64) [][]byte
func (db *RedisDB) ZRemRangeByScore(key string, min, max float64) int
func (db *RedisDB) ZCard(key string) int
```

### Bitmaps

```go
func (db *RedisDB) SetBit(key string, offset int, val int) int
func (db *RedisDB) GetBit(key string, offset int) int
func (db *RedisDB) BitCount(key string, start, end int) int
func (db *RedisDB) BitOp(op string, destKey string, srcKeys ...string) int
func (db *RedisDB) BitField(key string, args ...[]byte) []int64
```

### HyperLogLog

```go
func (db *RedisDB) PFAdd(key string, elements ...[]byte) int
func (db *RedisDB) PFCount(keys ...string) int64
func (db *RedisDB) PFMerge(dest string, sources ...string)
```

### Geo

```go
func (db *RedisDB) GeoAdd(key string, lon, lat float64, member string) int
func (db *RedisDB) GeoDist(key, member1, member2, unit string) float64
func (db *RedisDB) GeoRadius(key string, lon, lat, radius float64, unit string) []string
func (db *RedisDB) GeoRadiusByMember(key, member string, radius float64, unit string) []string
func (db *RedisDB) GeoPos(key string, members ...string) [][2]float64
```

### Streams

```go
func (db *RedisDB) XAdd(key string, id string, fields map[string][]byte) string
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
func (db *RedisDB) BFAdd(key string, item []byte) bool
func (db *RedisDB) BFExists(key string, item []byte) bool

// Cuckoo Filter
func (db *RedisDB) CFReserve(key string, capacity int)
func (db *RedisDB) CFAdd(key string, item []byte) bool
func (db *RedisDB) CFDel(key string, item []byte) bool
func (db *RedisDB) CFExists(key string, item []byte) bool

// Count-Min Sketch
func (db *RedisDB) CMSInitByDim(key string, width, depth int)
func (db *RedisDB) CMSIncrBy(key string, item []byte, inc int) int
func (db *RedisDB) CMSQuery(key string, items ...[]byte) []int

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
func (db *RedisDB) FTSearch(index string, query string, limit int) []string
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

## 使用示例

```go
package main

import (
    "fmt"
    "gedis"
)

func main() {
    db := gedis.New()

    // Strings
    db.Set("hello", []byte("world"))
    val, _ := db.Get("hello")
    fmt.Println(string(val)) // world

    // Lists
    db.LPush("mylist", []byte("a"), []byte("b"), []byte("c"))
    v, _ := db.LPop("mylist")
    fmt.Println(string(v)) // c

    // Hashes
    db.HSet("myhash", "f1", []byte("v1"))
    v, _ = db.HGet("myhash", "f1")
    fmt.Println(string(v)) // v1

    // Sorted Sets
    db.ZAdd("myzset", 1.0, []byte("a"))
    db.ZAdd("myzset", 2.0, []byte("b"))
    members := db.ZRange("myzset", 0, -1)
    for _, m := range members {
        fmt.Println(string(m))
    }
    // Output: a, b

    // Probabilistic - Bloom Filter
    db.BFReserve("bf", 0.01, 1000000)
    db.BFAdd("bf", []byte("item1"))
    exists := db.BFExists("bf", []byte("item1"))
    fmt.Println(exists) // true
}
```

## 并发安全

所有 public API 均在内部使用 `sync.RWMutex` 保护：

- **写操作**（Set、Del、LPush、ZAdd 等）：`Lock`
- **读操作**（Get、Exists、ZScore 等）：`RLock`

可以安全地从多个 goroutine 并发访问。

## 性能基准

```bash
go test -benchmem -bench=. -benchtime=1s -run NONE .
```

| 操作 | 吞吐量 | 每次分配 |
|------|--------|----------|
| Arena Alloc (64B) | ~10M ops/s | 0 allocs/op |
| Arena Read/Write | ~460M ops/s | 0 allocs/op |
| Dict Set | ~1.6M ops/s | 0 allocs/op |
| Dict Get (10K keys) | ~3.0M ops/s | 1 allocs/op |
| Redis IncrBy | ~9.2M ops/s | 0 allocs/op |
| Redis Set | ~1.0M ops/s | 1 allocs/op |
| Redis Get (10K keys) | ~2.1M ops/s | 2 allocs/op |

## 逃逸分析

`go build -gcflags="-m" .` 验证 Zero-GC 设计：

- `Arena.Alloc` 所有读写操作 **零堆分配**
- `make([]byte, size)` 临时缓冲区 **does not escape** （栈分配）
- `string` → `[]byte` 转换 **zero-copy conversion**
- `ziplist`/`skiplist` 内部操作 `arena does not escape`
- 仅 API 返回值（`string(member)`、`append(...)` 构建返回切片）和 `sync.RWMutex` 发生预期逃逸

## 项目结构

```
gedis/
├── arena.go          # Arena 内存分配器
├── object.go         # 对象头管理
├── dict.go           # 哈希表 (FNV-1a + 线性探测)
├── redis.go          # RedisDB 主结构
├── string.go         # String 命令
├── list.go           # List 命令
├── hash.go           # Hash 命令
├── set.go            # Set 命令
├── zset.go           # Sorted Set 命令
├── ziplist.go        # Ziplist 内部结构
├── skip_list.go      # Skiplist 内部结构
├── bitmap.go         # Bitmap / BitField 命令
├── hyperloglog.go    # HyperLogLog 命令
├── geo.go            # Geo 命令
├── stream.go         # Stream 命令
├── timeseries.go     # TimeSeries 命令
├── probabilistic.go  # Bloom/Cuckoo/CMS/TopK 命令
├── json.go           # JSON 命令
├── search.go         # Search 命令
├── graph.go          # Graph 命令
├── cell.go           # Rate Limiter 命令
├── redis_test.go     # 单元测试 (42 tests)
└── redis_bench_test.go # 性能基准测试 (42 benchmarks)
```

## 测试

```bash
# 运行所有单元测试
go test -v ./...

# 运行性能基准测试
go test -benchmem -bench=. -benchtime=1s -run NONE .

# 运行逃逸分析
go build -gcflags="-m" .
```

## License

MIT
