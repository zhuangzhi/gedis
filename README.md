# gedis

[English](./README_EN.md)

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://go.dev/)
[![Test](https://img.shields.io/badge/tests-67%20passed-brightgreen)](./redis_test.go)
[![Benchmark](https://img.shields.io/badge/benchmarks-70-f1c40f)](./redis_bench_test.go)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

gedis 是一个嵌入式 Redis-like 内存数据库，Go 语言实现。核心设计目标是 **零 GC 压力** —— 所有持久化数据存储在单一 `[]byte` Arena 中，使用整数偏移量替代 Go 指针，避免 GC 扫描结构化数据。

**新增功能：**
- 🔒 数据持久化：WAL + RDB 快照方案，支持 LZ4 压缩
- ⏱️ TimeSeries 扩展：完整的时间序列功能，支持标签、聚合、批量查询、下采样规则

**文档：**
- [持久化方案设计](./PERSISTENCE_DESIGN.md)
- [任务列表](./tasklist.md)
- [API 文档](./API.md)

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

## 类型设计原理

### 原生 Go 类型的 GC 问题

| 原生类型 | 问题 | 说明 |
|----------|------|------|
| `[]byte` | 每次创建产生堆分配 | `make([]byte, n)` 逃逸到堆，GC 需要扫描回收 |
| `string` | `string → []byte` 产生分配 | `[]byte(s)` = `make+copy`，每次转换都分配新内存 |
| `[][]byte` | O(n) 个独立 `[]byte` 分配 | ZRange 100 个元素 = 100+ 次 `make([]byte, ...)` |
| `[]string` | O(n) 个独立 `string` 分配 | 同上，且每个 `string` 底层也是逃逸分配 |
| `map[string][]byte` | 逐个返回大量独立对象 | HGetAll 返回 map，每个 key/value 独立分配 |

> 例如：ZRange 返回 100 个元素时，原生 `[][]byte` 需 101 次堆分配，GC 扫描压力巨大。

### 优化后的类型与契约

#### `*PooledBuffer` → 替代 `[]byte`

```
对象池 → Get(cap) → 写入数据 → 传给API → pb.Close() → 归还池
                                              ↑
                                      必须调用，否则泄漏
```

| 特性 | 说明 |
|------|------|
| **来源** | 6 档分级 `sync.Pool`（1K / 4K / 16K / 64K / 256K / 1M） |
| **分配** | `Buf(s)` 从池获取，写入 s；`NewBuf(cap)` 获取空缓冲区 |
| **GC 压力** | 极低 —— 缓冲区从池复用，归还后不触发新的堆分配 |
| **入参契约** | 传给 API 后**立即**调用 `Close()`，API 内部已拷贝/持有数据 |
| **返回值契约** | 调用方读取 `.Bytes()` / `.String()` 后**必须**调用 `Close()` |

```go
// 入参模式：传给 API 后立即 Close()
db.Set("key", Buf("value"))
buf.Close()  // ← 必须！

// 返回值模式：读完就 Close()
v, _ := db.Get("key")
fmt.Println(v.String())
v.Close()    // ← 必须！
```

> **关键区别**：`Buf("hello")` 从池获取缓冲区，GC 压力 ≈ 0；而 `[]byte("hello")` 从堆分配，GC 必须跟踪回收。

#### `*ZSlices` → 替代 `[]string` / `[][]byte`

```
[count(4B)] [len₁(4B)][data₁] [len₂(4B)][data₂] ...
   ↑ 头部        ↑ 元素 N，紧凑存储，单次池分配
```

| 特性 | 说明 |
|------|------|
| **原理** | 所有元素紧凑存储在单个 `PooledBuffer` 中，一次池分配 |
| **Get(i)** | 零拷贝 —— 直接返回内部 `[]byte` 子切片，不分配新内存 |
| **Len()** | 解析头部 count 字段，O(1) |
| **对比** | ZRange 100 元素：原生 `[][]byte` = **101 次分配** → `ZSlices` = **1 次池分配** |
| **契约** | 使用完**必须**调用 `Close()` 归还底层缓冲区 |

```go
zs := db.ZRange("key", 0, -1)
for i := 0; i < zs.Len(); i++ {
    // Get(i) 返回 []byte，零拷贝，不分配新内存
    fmt.Println(string(zs.Get(i)))
}
zs.Close()  // ← 必须！归还缓冲区分级池
```

> **注意**：`Get(i)` 返回的 `[]byte` 生命周期绑定到 `ZSlices`。**不要在 `Close()` 后继续使用** `Get()` 返回的切片。

#### `ZRangeIter` 回调 → 替代 `[]string`（零分配）

| 特性 | 说明 |
|------|------|
| **原理** | 不返回任何数据，通过回调函数 `fn(member []byte)` 逐个传递元素 |
| **分配** | **0 次堆分配**（ZRange 100 元素） |
| **对比** | `ZSlices` = 2 次池分配 → `ZRangeIter` = **0** 次 |
| **契约** | `fn` 接收的 `[]byte` **仅在回调执行期间有效**，不得持有到回调外 |

```go
// 零分配遍历 ZSet 成员
db.ZRangeIter("key", 0, -1, func(member []byte) {
    // member 仅在回调内有效，不能保存到外部变量！
    fmt.Println(string(member))
})
// 无需 Close() —— 没有返回需要归还的对象
```

> **危险**：回调内保存 `member` 到外部变量，回调结束后 `[]byte` 可能被 Arena 复用覆盖。

#### `Arena.GetSlice(off, size)` → 零拷贝 `[]byte` 视图

| 特性 | 说明 |
|------|------|
| **原理** | 返回底层 `[]byte` 缓冲区的子切片，不分配、不拷贝 |
| **对比** | `Arena.ReadBytes` = `make+copy`（分配副本） → `GetSlice` = **零分配** |
| **契约** | 返回的 `[]byte` 在 Arena 扩容/Free 后**立即失效**，不得长期持有 |

```go
// 内部使用模式（不暴露给外部 API）
slice := arena.GetSlice(offset, size)
// 立即使用，不保存到外部变量
doSomething(slice)
// slice 失效点：下一个 arena.Alloc 可能触发 grow
```

### 类型对比汇总

| 场景 | 原生 Go 类型 | 优化类型 | 分配 (100元素) | 契约 |
|------|-------------|----------|---------------|------|
| 字符串值读写 | `[]byte` | `*PooledBuffer` | 0 (池复用) | `Close()` |
| ZSet 范围查询 | `[][]byte` / `[]string` | `*ZSlices` | 2 (池分配) | `Close()` |
| ZSet 遍历 | `[][]byte` / `[]string` | `ZRangeIter` 回调 | **0** | fn 内不持有 `[]byte` |
| HLL 寄存器 | `make([]byte, 12288)` | `Arena.GetSlice` | **0** | 不持有到 Arena grow |
| Bitmap 操作 | `make([]byte, ...)` | `Arena.GetSlice` | **0** | 不持有到 Arena grow |

## API 参考

> 详细的 API 使用文档见 [API.md](./API.md)

### 零分配优化

gedis 所有公开 API 使用两套零分配类型替代原生 Go 类型：

- **`*PooledBuffer`** 替代 `[]byte` — 从 6 档分级对象池 (1K/4K/16K/64K/256K/1M) 分配，使用后须 `Close()` 归还
- **`*ZSlices`** 替代 `[]string` — 元素紧凑存于 PooledBuffer，`Get(i)` 零拷贝返回 `[]byte`，使用后须 `Close()`

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
// 基础功能
func (db *RedisDB) TSAdd(key string, ts int64, val float64) int
func (db *RedisDB) TSAddWithLabels(key string, ts int64, val float64, labels map[string]string) int
func (db *RedisDB) TSGetLabels(key string) map[string]string
func (db *RedisDB) TSRange(key string, startTs, endTs int64) []TSPoint
func (db *RedisDB) TSRevRange(key string, startTs, endTs int64) []TSPoint
func (db *RedisDB) TSLast(key string) (int64, float64, bool)
func (db *RedisDB) TSDel(key string, startTs, endTs int64) int

// 聚合功能
func (db *RedisDB) TSAggregate(points []TSPoint, agg TSAggregation) float64
func (db *RedisDB) TSRangeWithAgg(key string, startTs, endTs int64, agg TSAggregation, bucketSize int64) []TSPoint
func (db *RedisDB) TSRevRangeWithAgg(key string, startTs, endTs int64, agg TSAggregation, bucketSize int64) []TSPoint

// 批量查询
func (db *RedisDB) TSMGET(keys []string) []TSMGETResult
func (db *RedisDB) TSMRANGE(startTs, endTs int64, labels map[string]string, agg TSAggregation, bucketSize int64, rev bool) []TSMRANGEResult
func (db *RedisDB) TSQUERYINDEX(labels map[string]string) []TSMGETResult

// 下采样/压缩规则
func (db *RedisDB) TSCreateRule(key, destKey string, bucketSize int64, agg TSAggregation) bool
func (db *RedisDB) TSDeleteRule(key, destKey string) bool
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
    fmt.Println(val.String()) // world
    val.Close()

    // Lists
    db.LPush("mylist", []byte("a"), []byte("b"), []byte("c"))
    v, _ := db.LPop("mylist")
    fmt.Println(v.String()) // c
    v.Close()

    // Hashes
    db.HSet("myhash", "f1", []byte("v1"))
    v, _ = db.HGet("myhash", "f1")
    fmt.Println(v.String()) // v1
    v.Close()

    // Sorted Sets
    db.ZAdd("myzset", 1.0, []byte("a"))
    db.ZAdd("myzset", 2.0, []byte("b"))
    members := db.ZRange("myzset", 0, -1)
    for i := 0; i < members.Len(); i++ {
        fmt.Println(string(members.Get(i)))
    }
    members.Close()
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

Intel Core Ultra 9 185H, Windows, Go 1.21+

```bash
go test -bench=. -benchtime=300ms -benchmem -count=1 -run NONE .
```

### 核心组件

| Benchmark | 耗时 | 吞吐量 | 字节 | 分配 |
|-----------|------|--------|------|------|
| Arena Alloc (64B) | 39.7 ns | ~25M ops/s | 218 B | 0 |
| Arena Alloc+Free | 3.8 ns | ~265M ops/s | 0 B | 0 |
| Arena Read/Write | 0.5 ns | ~2G ops/s | 0 B | 0 |
| Dict Set | 8.0 ns | ~125M ops/s | 0 B | 0 |
| Dict Get | 49.5 ns | ~20M ops/s | 3 B | 1 |
| Dict Del | 14.7 ns | ~68M ops/s | 20 B | 0 |

### Redis 命令

| Benchmark | 耗时 | 吞吐量 | 字节 | 分配 |
|-----------|------|--------|------|------|
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

### 并发

| Benchmark | 耗时 | 吞吐量 | 字节 | 分配 |
|-----------|------|--------|------|------|
| Concurrent Read | 74.4 ns | ~13M ops/s | 11 B | 2 |
| Concurrent Write | 149 ns | ~6.7M ops/s | 69 B | 1 |
| Concurrent IncrBy | 56.3 ns | ~18M ops/s | 0 B | 0 |
| Mixed Read (5 ops) | 580 ns | ~1.7M iter/s | 110 B | 14 |

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
├── arena.go             # Arena 内存分配器
├── poolbuffer.go        # PooledBuffer 分级对象池
├── zslices.go           # ZSlices 零分配字符串切片
├── object.go            # 对象头管理
├── dict.go              # 哈希表 (FNV-1a + 线性探测)
├── redis.go             # RedisDB 主结构
├── string.go            # String 命令
├── list.go              # List 命令
├── hash.go              # Hash 命令
├── set.go               # Set 命令
├── zset.go              # Sorted Set 命令
├── ziplist.go           # Ziplist 内部结构
├── skip_list.go         # Skiplist 内部结构
├── bitmap.go            # Bitmap / BitField 命令
├── hyperloglog.go       # HyperLogLog 命令
├── geo.go               # Geo 命令
├── stream.go            # Stream 命令
├── timeseries.go        # TimeSeries 命令
├── probabilistic.go     # Bloom/Cuckoo/CMS/TopK 命令
├── json.go              # JSON 命令
├── search.go            # Search 命令
├── graph.go             # Graph 命令
├── cell.go              # 速率限制
├── wal.go               # Write-Ahead Log (WAL) 持久化
├── persistence.go       # 持久化管理器 (RDB 快照 + 恢复)
├── redis_test.go        # 单元测试 (67 tests)
├── wal_test.go          # WAL 单元测试
├── redis_bench_test.go  # 性能基准测试 (70 benchmarks)
├── example/             # 使用示例
├── API.md               # API 详细使用文档
├── PERSISTENCE_DESIGN.md # 持久化方案设计文档
├── tasklist.md          # 任务列表
```

## 测试

```bash
# 运行所有单元测试
go test -v ./...

# 运行性能基准测试
go test -bench=. -benchtime=300ms -benchmem -count=1 -run NONE .

# 运行逃逸分析
go build -gcflags="-m" .
```

## License

MIT

## 路线图

### ✅ 已实现模块

String, List, Hash, Set, Sorted Set, Bitmap, HyperLogLog, Geo, Stream, TimeSeries (Labels), Probabilistic (BF/CF/CMS/TopK), JSON, Search, Graph, Rate Limiting (Cell/Throttle)

### ✅ 高优先级命令（已完成）

| 类别 | 命令 | 说明 |
|------|------|------|
| String | `MGET` / `MSET` | 批量获取/设置多个 key |
| String | `DECR` / `DECRBY` | 递减操作 |
| String | `SETNX` | key 不存在时设置（分布式锁常用） |
| Hash | `HMSET` / `HMGET` | 批量设置/获取 hash 字段 |
| Hash | `HKEYS` / `HVALS` | 获取所有字段名/值 |
| Hash | `HSETNX` | field 不存在时设置 |
| List | `LTRIM` | 裁剪列表保留指定范围 |
| List | `LLEN` | 获取列表长度 |
| Set | `SDIFF` / `SDIFFSTORE` | 集合差集运算 |
| Sorted Set | `ZPOPMIN` / `ZPOPMAX` | 弹出最小/最大分数成员 |
| Sorted Set | `ZRANK` / `ZREVRANK` | 获取成员排名 |
| Sorted Set | `ZINCRBY` | 递增分数 |

### ✅ 中优先级命令（已完成）

| 类别 | 命令 | 说明 |
|------|------|------|
| String | `SETEX` / `PSETEX` | 设置值并指定过期时间 |
| String | `GETDEL` | 删除并返回值 |
| String | `BITPOS` | 查找第一个 0/1 bit 位置 |
| Hash | `HINCRBYFLOAT` | hash 字段浮点递增 |
| Hash | `HSTRLEN` | 获取字段值长度 |
| Hash | `HRANDFIELD` | 随机获取 hash 字段 |
| List | `LMOVE` / `BLMOVE` | 原子移动列表元素 |
| List | `RPOPLPUSH` | 弹出并推送到另一列表 |
| List | `LINSERT` | 在指定位置插入 |
| Set | `SMOVE` | 移动成员到另一集合 |
| Sorted Set | `ZCOUNT` | 统计分数范围内成员数 |
| Sorted Set | `ZLEXCOUNT` | 字典序范围计数 |
| Sorted Set | `ZRANGEBYLEX` | 字典序范围查询 |
| Sorted Set | `BZMPOP` | 阻塞弹出版本 |

### ✅ Stream 扩展（已完成）

| 命令 | 说明 |
|------|------|
| `XDEL` | 删除流中指定 ID 的条目 |
| `XTRIM` | 裁剪流保留指定数量的条目 |
| `XINFO` | 获取流、组、消费者的详细信息 |
| `XCLAIM` | 认领其他消费者的 pending 消息 |
| `XAUTOCLAIM` | 自动认领旧消息 |
| `XPENDING` | 查看 pending 消息状态 |
| `XGROUP CREATECONSUMER` | 创建消费者 |
| `XGROUP DELCONSUMER` | 删除消费者 |

### ✅ TimeSeries 扩展（已完成）

| 命令 | 说明 |
|------|------|
| `TS.CREATEDS` | 创建下采样/压缩规则 |
| `TS.GET` | 获取最新数据点 |
| `TS.MGET` | 批量获取多个时间序列 |
| `TS.RANGE` / `TS.REVRANGE` | 范围查询 |
| `TS.MRANGE` / `TS.MREVRANGE` | 批量范围查询带标签过滤 |
| `TS.QUERYINDEX` | 按标签查询时间序列 |
| `TS.AGGREGATIONS` | 内置聚合函数 (avg, sum, min, max, count, first, last, std.p, var.p, range) |
| `TS.DELETERULE` | 删除压缩规则 |
| `TS.AddWithLabels` | 添加带标签的时间序列数据 |
| `TS.GetLabels` | 获取时间序列的标签 |

### ✅ 数据持久化（已完成）

| 功能 | 说明 |
|------|------|
| **WAL** | Write-Ahead Log，支持 LZ4 压缩、组提交、每秒 fsync |
| **RDB 快照** | 内存数据二进制快照，支持增量持久化 |
| **恢复流程** | 自动加载最新 RDB + 重放 WAL，保证数据一致性 |
| **性能** | 1GB 数据压缩写入 ~1.5s，读取 ~1.4s |

### 📋 待实现功能

#### 低优先级（不常用/复杂）

- **Pub/Sub**: PUBLISH, SUBSCRIBE, PSUBSCRIBE 等
- **Transactions**: MULTI, EXEC, DISCARD, WATCH
- **Scripting**: EVAL, EVALSHA, FUNCTION 等
- **Server/Config**: SAVE, BGSAVE, INFO, CONFIG 等
- **ACL**: 用户权限管理
- **Cluster**: 集群相关命令
- **Module APIs**: BloomFilter 增强, TDigest, AI 等
