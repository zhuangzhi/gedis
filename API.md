# Gedis API 使用文档

> gedis 是一个嵌入式 Redis-like 内存数据库，Go 语言实现。所有数据操作均支持并发安全访问。

## 目录

- [快速开始](#快速开始)
- [优化设计：PooledBuffer 与 ZSlices](#优化设计pooledbuffer-与-zslices)
- [通用键操作](#通用键操作)
- [字符串](#字符串)
- [列表](#列表)
- [哈希](#哈希)
- [集合](#集合)
- [有序集合](#有序集合)
- [位图](#位图)
- [HyperLogLog](#hyperloglog)
- [地理位置](#地理位置)
- [流](#流)
- [时间序列](#时间序列)
- [概率数据结构](#概率数据结构)
  - [布隆过滤器](#布隆过滤器)
  - [布谷鸟过滤器](#布谷鸟过滤器)
  - [Count-Min Sketch](#count-min-sketch)
  - [Top-K](#top-k)
- [JSON](#json)
- [全文搜索](#全文搜索)
- [图查询](#图查询)
- [速率限制](#速率限制)
- [数据结构类型](#数据结构类型)

---

## 快速开始

```go
package main

import (
    "fmt"
    "gedis"
)

func main() {
    db := gedis.New()

    db.Set("name", gedis.Buf("Alice"))
    db.Set("age", gedis.Buf("30"))
    db.SAdd("users", gedis.Buf("Alice"), gedis.Buf("Bob"), gedis.Buf("Charlie"))
    db.ZAdd("scores", 95.5, gedis.Buf("Alice"))
    db.ZAdd("scores", 87.0, gedis.Buf("Bob"))

    value, ok := db.Get("name")
    if ok {
        fmt.Println("name:", value.String())
        value.Close()
    }

    members := db.SMembers("users")
    for _, m := range members {
        fmt.Println("  user:", m.String())
        m.Close()
    }

    results := db.ZRange("scores", 0, -1)
    for i := 0; i < results.Len(); i++ {
        fmt.Printf("  #%d: %s\n", i+1, string(results.Get(i)))
    }
    results.Close()
}
```

---

## 优化设计：PooledBuffer 与 ZSlices

gedis 所有公开 API 均经过零分配优化，使用 `*PooledBuffer` 替代 `[]byte`、`*ZSlices` 替代 `[]string` / `[][]byte`。

### 为什么不用原生 Go 类型？

| 原生类型 | GC 问题 | 典型影响 |
|----------|---------|----------|
| `[]byte` | 每次 `make([]byte, n)` 产生堆分配 | ZAdd：12288B/op |
| `string → []byte` | `[]byte(s)` = make+copy 堆分配 | PFAdd：12288B/op |
| `[][]byte` | N 个元素 = N 次独立 make | ZRange 100元素：101 allocs |
| `[]string` | 同 `[][]byte`，每个 string 底层也逃逸 | HGetAll：~100 allocs |
| `map[string][]byte` | 每个 key/value 独立堆分配 | HGetAll：大量 allocs |

> 在高吞吐场景下，每秒数以亿计的小对象分配会触发频繁 GC STW，严重影响延迟和吞吐。

### PooledBuffer（替代 `[]byte`）

缓冲区从全局分级对象池（6 档：1K / 4K / 16K / 64K / 256K / 1M）分配，使用后须 `Close()` 归还。

```
Buf(s) / NewBuf(cap) → 池获取 → 写入数据 → 传给 API → pb.Close() → 归还池
                                                              ↑
                                                         必须调用！
```

| 契约 | 说明 |
|------|------|
| **入参** | 传给 API 后**立即** `Close()`，API 内部已拷贝/持有数据 |
| **返回值** | 调用方读取 `.Bytes()` / `.String()` 后**必须** `Close()` |
| **不 Close 的后果** | 缓冲区泄漏，失去池复用效果，GC 压力回升 |

**作为 API 入参**：

```go
pb := gedis.Buf("hello")
db.Set("key", pb)
pb.Close()  // ← 必须！传入后立即归还
```

**作为 API 返回值**：

```go
val, ok := db.Get("key")
if ok {
    fmt.Println(val.String())  // 转为 string 使用
    val.Close()                // ← 必须！归还池
}
```

### ZSlices（替代 `[]string` / `[][]byte`）

所有元素紧凑存储在单个 PooledBuffer 中，格式为 `[count(4B)][len₁(4B)][data₁][len₂(4B)][data₂]...`。`Get(i)` 零拷贝返回内部 `[]byte` 子切片。

| 特性 | 说明 |
|------|------|
| **原理** | 单次池分配，紧凑存储所有元素 |
| **Get(i)** | 零拷贝 —— 直接返回内部缓冲区子切片 |
| **对比** | ZRange 100 元素：原生 `[][]byte` = 101 次分配 → `ZSlices` = 1 次池分配 |
| **契约** | 使用完**必须** `Close()` 归还底层 PooledBuffer |

**使用模式**：

```go
zs := db.ZRange("leaderboard", 0, -1)
for i := 0; i < zs.Len(); i++ {
    fmt.Println(string(zs.Get(i)))
}
zs.Close()  // ← 必须！
```

> **危险**：`Get(i)` 返回的 `[]byte` 生命周期绑定到 ZSlices。`Close()` 后会失效，不可持有到 Close 之后。

### ZRangeIter（零分配回调遍历）

不返回任何对象，通过回调函数逐个传递元素。ZRange 100 元素 = **0 次分配**。

```go
db.ZRangeIter("leaderboard", 0, -1, func(member []byte) {
    // member 仅在回调内有效，不能保存到外部变量！
    fmt.Println(string(member))
})
// 无需 Close() —— 没有返回需要归还的对象
```

> **危险**：回调内保存 `member` 到外部变量，回调结束后 `[]byte` 可能被 Arena 复用覆盖。不需要 Close —— 没用池。

### Arena.GetSlice（零拷贝内部视图）

直接返回 Arena 底层 `[]byte` 的子切片，不分配、不拷贝。仅在 Arena 内部使用，**不在外部 API 暴露**。

```go
// 内部模式
slice := arena.GetSlice(offset, size)
doSomething(slice)
// 契约：下一个 arena.Alloc 可能触发 grow，slice 即失效
```

### 类型对比总览

| 场景 | 原生 Go 类型 | 优化类型 | 分配 (100元素) | 释放方式 |
|------|-------------|----------|---------------|----------|
| 字符串值读/写 | `[]byte` | `*PooledBuffer` | 0 (池复用) | `Close()` |
| ZSet 范围查询 | `[][]byte` | `*ZSlices` | 1 (池分配) | `Close()` |
| ZSet 遍历 | `[][]byte` | `ZRangeIter` 回调 | **0** | 无需释放 |
| HLL/PFCount | `make([]byte, 12288)` | `Arena.GetSlice` | **0** | 不持有 |

---

## 通用键操作

### `New() *RedisDB`

创建一个新的数据库实例。

```go
db := gedis.New()
```

### `Del(key string) bool`

删除指定键及其关联的数据。

```go
db.Set("name", gedis.Buf("Alice"))
deleted := db.Del("name")  // true
deleted = db.Del("name")   // false
```

### `Exists(key string) bool`

检查指定键是否存在。

```go
db.Set("name", gedis.Buf("Alice"))
exists := db.Exists("name")   // true
exists = db.Exists("unknown") // false
```

### `FlushAll()`

清空数据库中的所有数据。

```go
db.FlushAll()
```

---

## 字符串

所有数据通过 `*PooledBuffer` 传递，这是封装了 `bytes.Buffer` + 内存池的可复用缓冲区，可避免 GC 压力。

### `Buf(s string) *PooledBuffer`

从默认池获取一个缓冲区并写入字符串。

```go
pb := gedis.Buf("hello world")
// 使用后通过 Close() 归还池中
pb.Close()
```

### `NewBuf(minCap int) *PooledBuffer`

获取一个最小容量为 minCap 的空缓冲区。

```go
pb := gedis.NewBuf(1024)
pb.WriteString("hello")
```

### `Set(key string, value *PooledBuffer)`

设置键的字符串值。

```go
db.Set("greeting", gedis.Buf("hello world"))
db.Set("counter", gedis.Buf("42"))
```

### `Get(key string) (*PooledBuffer, bool)`

获取键的字符串值。调用方负责在读取后调用 `Close()` 归还缓冲区。

```go
val, ok := db.Get("greeting")
if ok {
    fmt.Println(val.String())
    val.Close()
}
```

### `Append(key string, value *PooledBuffer) int`

在已有字符串值末尾追加数据。

```go
db.Set("msg", gedis.Buf("hello"))
newLen := db.Append("msg", gedis.Buf(" world")) // 11
```

### `GetRange(key string, start, end int) (*PooledBuffer, bool)`

获取字符串值的子串，支持负数索引。

```go
db.Set("msg", gedis.Buf("hello world"))
sub, _ := db.GetRange("msg", 0, 4)  // "hello"
sub.Close()
sub, _ = db.GetRange("msg", -5, -1) // "world"
sub.Close()
```

### `SetRange(key string, offset int, value *PooledBuffer) int`

在指定偏移位置覆写字符串值。

```go
db.Set("msg", gedis.Buf("hello world"))
newLen := db.SetRange("msg", 6, gedis.Buf("gedis")) // "hello gedis"
```

### `Strlen(key string) int`

获取字符串值的长度。

```go
db.Set("msg", gedis.Buf("hello"))
length := db.Strlen("msg") // 5
```

### `IncrBy(key string, inc int64) (int64, error)`

将字符串值解释为整数并增加指定值。

```go
db.Set("counter", gedis.Buf("10"))
newVal, _ := db.IncrBy("counter", 5) // 15
newVal, _ = db.IncrBy("visits", 1)   // 1 (不存在的key自动初始化)
```

### `IncrByFloat(key string, inc float64) (float64, error)`

将字符串值解释为浮点数并增加指定值。

```go
db.Set("score", gedis.Buf("10.5"))
newVal, _ := db.IncrByFloat("score", 0.5) // 11.0
```

---

## 列表

### `LPush(key string, values ...*PooledBuffer) int`

将一个或多个值从列表左侧插入。

```go
db.LPush("queue", gedis.Buf("c"), gedis.Buf("b"), gedis.Buf("a"))
// 列表: ["a", "b", "c"]
```

### `RPush(key string, values ...*PooledBuffer) int`

将一个或多个值从列表右侧插入。

```go
db.RPush("queue", gedis.Buf("a"), gedis.Buf("b"), gedis.Buf("c"))
```

### `LPop(key string) (*PooledBuffer, bool)`

从列表左侧弹出并返回一个元素。

```go
db.LPush("queue", gedis.Buf("a"), gedis.Buf("b"))
val, ok := db.LPop("queue")
if ok {
    fmt.Println(val.String())
    val.Close()
}
```

### `RPop(key string) (*PooledBuffer, bool)`

从列表右侧弹出并返回一个元素。

```go
db.RPush("queue", gedis.Buf("a"), gedis.Buf("b"))
val, ok := db.RPop("queue")
if ok {
    fmt.Println(val.String())
    val.Close()
}
```

### `LIndex(key string, index int) (*PooledBuffer, bool)`

获取列表中指定索引位置的元素。

```go
db.RPush("list", gedis.Buf("a"), gedis.Buf("b"), gedis.Buf("c"))
val, _ := db.LIndex("list", 1)  // "b"
if val != nil { val.Close() }
```

### `LRange(key string, start, stop int) []*PooledBuffer`

获取列表指定范围内的元素。

```go
db.RPush("list", gedis.Buf("a"), gedis.Buf("b"), gedis.Buf("c"), gedis.Buf("d"))
items := db.LRange("list", 1, 2)
for _, item := range items {
    fmt.Println(item.String())
    item.Close()
}
```

### `LLen(key string) int`

获取列表中的元素数量。

```go
length := db.LLen("queue")
```

---

## 哈希

### `HSet(key, field string, value *PooledBuffer) int`

设置哈希表中指定字段的值。

```go
db.HSet("user:1", "name", gedis.Buf("Alice"))
db.HSet("user:1", "age", gedis.Buf("30"))
db.HSet("user:1", "email", gedis.Buf("alice@example.com"))
```

### `HGet(key, field string) (*PooledBuffer, bool)`

获取哈希表中指定字段的值。

```go
val, ok := db.HGet("user:1", "name")
if ok {
    fmt.Println(val.String())
    val.Close()
}
```

### `HDel(key string, fields ...string) int`

删除哈希表中一个或多个字段。

```go
db.HSet("user:1", "tmp1", gedis.Buf("v1"))
db.HSet("user:1", "tmp2", gedis.Buf("v2"))
deleted := db.HDel("user:1", "tmp1", "tmp2") // 2
```

### `HGetAll(key string) map[string]*PooledBuffer`

获取哈希表中所有字段和值。

```go
db.HSet("user:1", "name", gedis.Buf("Alice"))
db.HSet("user:1", "age", gedis.Buf("30"))

all := db.HGetAll("user:1")
for field, val := range all {
    fmt.Printf("%s: %s\n", field, val.String())
    val.Close()
}
```

### `HIncrBy(key, field string, inc int64) (int64, error)`

将哈希表中指定字段的值增加指定数值。

```go
db.HSet("user:1", "visits", gedis.Buf("5"))
newVal, _ := db.HIncrBy("user:1", "visits", 3) // 8
```

### `HExists(key, field string) bool`

检查哈希表中是否存在指定字段。

```go
exists := db.HExists("user:1", "name") // true
```

### `HLen(key string) int`

获取哈希表中字段的数量。

```go
count := db.HLen("user:1")
```

---

## 集合

### `SAdd(key string, members ...*PooledBuffer) int`

向集合中添加一个或多个成员。

```go
db.SAdd("tags", gedis.Buf("go"), gedis.Buf("redis"), gedis.Buf("database"))
db.SAdd("tags", gedis.Buf("go")) // 0 (已存在)
```

### `SRem(key string, members ...*PooledBuffer) int`

从集合中删除一个或多个成员。

```go
removed := db.SRem("tags", gedis.Buf("database")) // 1
```

### `SIsMember(key string, member *PooledBuffer) bool`

检查指定成员是否在集合中。

```go
isMember := db.SIsMember("tags", gedis.Buf("go")) // true
```

### `SMembers(key string) []*PooledBuffer`

获取集合中所有成员。

```go
members := db.SMembers("tags")
for _, m := range members {
    fmt.Println(m.String())
    m.Close()
}
```

### `SCard(key string) int`

获取集合中的成员数量。

```go
count := db.SCard("tags")
```

### `SInter(keys ...string) []*PooledBuffer`

获取多个集合的交集。

```go
db.SAdd("set1", gedis.Buf("a"), gedis.Buf("b"), gedis.Buf("c"))
db.SAdd("set2", gedis.Buf("b"), gedis.Buf("c"), gedis.Buf("d"))
result := db.SInter("set1", "set2") // ["b", "c"]
for _, m := range result {
    fmt.Println(m.String())
    m.Close()
}
```

### `SUnion(keys ...string) []*PooledBuffer`

获取多个集合的并集。

```go
db.SAdd("set1", gedis.Buf("a"), gedis.Buf("b"))
db.SAdd("set2", gedis.Buf("b"), gedis.Buf("c"))
result := db.SUnion("set1", "set2") // ["a", "b", "c"]
for _, m := range result {
    m.Close()
}
```

---

## 有序集合

### `ZAdd(key string, score float64, member *PooledBuffer) int`

向有序集合中添加成员及其分数。

```go
db.ZAdd("leaderboard", 1000, gedis.Buf("Alice"))
db.ZAdd("leaderboard", 850, gedis.Buf("Bob"))
db.ZAdd("leaderboard", 950, gedis.Buf("Charlie"))
```

### `ZRem(key string, member *PooledBuffer) bool`

从有序集合中删除指定成员。

```go
deleted := db.ZRem("leaderboard", gedis.Buf("Bob")) // true
```

### `ZScore(key string, member *PooledBuffer) (float64, bool)`

获取有序集合中指定成员的分数。

```go
score, ok := db.ZScore("leaderboard", gedis.Buf("Alice"))
if ok {
    fmt.Printf("Score: %.0f\n", score) // 1000
}
```

### `ZRange(key string, start, stop int) ZSlices`

获取有序集合中指定排名范围的成员，返回零分配 ZSlices 视图。

```go
members := db.ZRange("leaderboard", 0, -1)
for i := 0; i < members.Len(); i++ {
    fmt.Println(string(members.Get(i)))
}
members.Close()
```

### `ZRangeIter(key string, start, stop int, fn func(member []byte))`

对有序集合成员进行零分配回调遍历。回调中的 `member []byte` 指向 Arena 内存视图。

```go
db.ZRangeIter("leaderboard", 0, -1, func(member []byte) {
    fmt.Println(string(member))
})
```

### `ZRangeWithScores(key string, start, stop int) (*ZSlices, []float64)`

获取有序集合指定排名范围的成员及其分数。

```go
names, scores := db.ZRangeWithScores("leaderboard", 0, -1)
for i := 0; i < names.Len(); i++ {
    fmt.Printf("%s: %.0f\n", string(names.Get(i)), scores[i])
}
names.Close()
```

### `ZRangeByScore(key string, min, max float64) []*PooledBuffer`

获取分数在 `[min, max]` 范围内的成员。

```go
members := db.ZRangeByScore("leaderboard", 900, 1100)
for _, m := range members {
    fmt.Println(m.String())
    m.Close()
}
```

### `ZRemRangeByScore(key string, min, max float64) int`

删除分数在 `[min, max]` 范围内的成员。

```go
deleted := db.ZRemRangeByScore("leaderboard", 800, 900)
```

### `ZCard(key string) int`

获取有序集合中的成员数量。

```go
count := db.ZCard("leaderboard")
```

---

## 位图

### `SetBit(key string, offset int, val int) int`

设置或清除指定偏移位置的位。

```go
db.SetBit("online", 0, 1)
db.SetBit("online", 3, 1)
old := db.SetBit("online", 3, 0) // 返回旧值 1
```

### `GetBit(key string, offset int) int`

获取指定偏移位置的位值。

```go
val := db.GetBit("online", 0) // 1
```

### `BitCount(key string, start, end int) int`

统计指定范围内设置为 1 的位数。

```go
count := db.BitCount("data", 0, -1)
```

### `BitOp(op string, destKey string, srcKeys ...string) int`

对多个位图执行位运算。支持 AND/OR/XOR/NOT。

```go
db.BitOp("AND", "result", "a", "b")
```

### `BitField(key string, args ...*PooledBuffer) []int64`

在位图上执行 GET/SET/INCRBY 子命令序列。

```go
results := db.BitField("mykey",
    gedis.Buf("SET"), gedis.Buf("i8"), gedis.Buf("0"), gedis.Buf("42"),
    gedis.Buf("GET"), gedis.Buf("i8"), gedis.Buf("0"),
)
```

---

## HyperLogLog

### `PFAdd(key string, elements ...[]byte) int`

向 HyperLogLog 中添加元素，使用原生 `[]byte`。

```go
db.PFAdd("visitors",
    []byte("user1"), []byte("user2"), []byte("user3"),
    []byte("user1"), // 重复，不会更新
)
// 返回: 3
```

### `PFAddBuffer(key string, elements ...*PooledBuffer) int`

向 HyperLogLog 中添加元素，使用 `*PooledBuffer` 避免堆分配。

```go
db.PFAddBuffer("visitors",
    gedis.Buf("user1"), gedis.Buf("user2"), gedis.Buf("user3"),
)
// 返回: 3
```

### `PFCount(keys ...string) int64`

估计 HyperLogLog 的基数。

```go
count := db.PFCount("visitors")
count = db.PFCount("visitors1", "visitors2") // 合并估算
```

### `PFMerge(dest string, sources ...string)`

将多个 HyperLogLog 合并到目标 key 中。

```go
db.PFMerge("total", "visitors1", "visitors2")
```

---

## 地理位置

### `GeoAdd(key string, lon, lat float64, member string) int`

添加地理位置坐标。

```go
db.GeoAdd("cities", 116.397, 39.908, "Beijing")
db.GeoAdd("cities", 121.473, 31.230, "Shanghai")
```

### `GeoDist(key, member1, member2, unit string) float64`

计算两个位置之间的距离。支持 m/km/mi/ft。

```go
dist := db.GeoDist("cities", "Beijing", "Shanghai", "km")
```

### `GeoRadius(key string, lon, lat, radius float64, unit string) *ZSlices`

获取指定半径范围内的成员。

```go
members := db.GeoRadius("cities", 116.397, 39.908, 500, "km")
for i := 0; i < members.Len(); i++ {
    fmt.Println(string(members.Get(i)))
}
members.Close()
```

### `GeoRadiusByMember(key, member string, radius float64, unit string) *ZSlices`

获取以某成员为中心半径范围内的成员。

```go
nearby := db.GeoRadiusByMember("cities", "Beijing", 1000, "km")
for i := 0; i < nearby.Len(); i++ {
    fmt.Println(string(nearby.Get(i)))
}
nearby.Close()
```

### `GeoPos(key string, members ...string) [][2]float64`

获取指定成员的经纬度坐标。

```go
positions := db.GeoPos("cities", "Beijing", "Shanghai")
```

---

## 流

### `XAdd(key string, id string, fields map[string]*PooledBuffer) string`

向流中添加条目。ID 使用 `"*"` 自动生成。

```go
id := db.XAdd("mystream", "*", map[string]*PooledBuffer{
    "name":  gedis.Buf("Alice"),
    "score": gedis.Buf("100"),
})
```

### `XRead(streams map[string]string, count int) map[string][]StreamEntry`

从流中读取条目。

```go
entries := db.XRead(map[string]string{"mystream": "0"}, 10)
for streamKey, streamEntries := range entries {
    for _, entry := range streamEntries {
        fmt.Printf("ID: %s\n", entry.ID)
        for k, v := range entry.Fields {
            fmt.Printf("  %s: %s\n", k, v.String())
        }
    }
}
```

### `XGroupCreate(key, group, startID string) error`

创建消费者组。

```go
err := db.XGroupCreate("mystream", "mygroup", "0")
```

### `XReadGroup(group, consumer string, streams map[string]string, count int) map[string][]StreamEntry`

以消费者组身份读取条目。

```go
entries := db.XReadGroup("mygroup", "consumer1",
    map[string]string{"mystream": ">"}, 10)
```

### `XLen(key string) int`

获取流中的条目数量。

```go
count := db.XLen("mystream")
```

---

## 时间序列

### `TSAdd(key string, ts int64, val float64) int`

添加采样点。

```go
db.TSAdd("cpu:usage", 1000, 45.2)
db.TSAdd("cpu:usage", 1005, 52.1)
```

### `TSRange(key string, startTs, endTs int64) []TSPoint`

查询时间范围内的采样点。

```go
points := db.TSRange("cpu:usage", 1000, 1010)
for _, p := range points {
    fmt.Printf("ts=%d, val=%.1f\n", p.Timestamp, p.Value)
}
```

### `TSLast(key string) (int64, float64, bool)`

获取最后一个采样点。

```go
ts, val, ok := db.TSLast("cpu:usage")
```

### `TSDel(key string, startTs, endTs int64) int`

删除时间范围内的采样点。

```go
deleted := db.TSDel("cpu:usage", 1000, 1005)
```

---

## 概率数据结构

### 布隆过滤器

#### `BFReserve(key string, errorRate float64, capacity int)`

预留布隆过滤器空间。

```go
db.BFReserve("bf", 0.01, 100000)
```

#### `BFAdd(key string, item *PooledBuffer) bool`

添加元素到布隆过滤器。

```go
isNew := db.BFAdd("bf", gedis.Buf("apple"))   // true
isNew = db.BFAdd("bf", gedis.Buf("apple"))    // false
```

#### `BFExists(key string, item *PooledBuffer) bool`

检查元素是否可能存在。

```go
exists := db.BFExists("bf", gedis.Buf("apple"))  // true
exists = db.BFExists("bf", gedis.Buf("grape"))   // false (一定不存在)
```

### 布谷鸟过滤器

#### `CFReserve(key string, capacity int)`

预留布谷鸟过滤器空间。

```go
db.CFReserve("cf", 1024)
```

#### `CFAdd(key string, item *PooledBuffer) bool`

添加元素。

```go
ok := db.CFAdd("cf", gedis.Buf("go"))    // true
ok = db.CFAdd("cf", gedis.Buf("go"))     // false (已存在)
```

#### `CFDel(key string, item *PooledBuffer) bool`

删除元素。

```go
deleted := db.CFDel("cf", gedis.Buf("go"))   // true
```

#### `CFExists(key string, item *PooledBuffer) bool`

检查元素是否存在。

```go
exists := db.CFExists("cf", gedis.Buf("rust")) // true
```

### Count-Min Sketch

#### `CMSInitByDim(key string, width, depth int)`

初始化 CMS。

```go
db.CMSInitByDim("cms", 100, 5)
```

#### `CMSIncrBy(key string, item *PooledBuffer, inc int) int`

增加计数。

```go
count := db.CMSIncrBy("cms", gedis.Buf("item_a"), 3) // 3
count = db.CMSIncrBy("cms", gedis.Buf("item_a"), 2)  // 5
```

#### `CMSQuery(key string, items ...*PooledBuffer) []int`

查询计数。

```go
counts := db.CMSQuery("cms", gedis.Buf("item_a"), gedis.Buf("item_b"))
```

### Top-K

#### `TopKReserve(key string, k int)`

预留 Top-K 空间。

```go
db.TopKReserve("topk", 3)
```

#### `TopKAdd(key string, items ...string)`

添加元素。

```go
db.TopKAdd("topk", "a", "b", "a", "c", "b", "a")
```

#### `TopKList(key string) []string`

获取当前 Top-K 元素列表。

```go
items := db.TopKList("topk")
```

---

## JSON

### `JsonSet(key, path string, value interface{}) error`

设置 JSON 值。

```go
db.JsonSet("doc", "$", map[string]interface{}{
    "name": "John",
    "age":  30.0,
})
```

### `JsonGet(key, path string) (interface{}, error)`

获取 JSON 值。

```go
val, _ := db.JsonGet("doc", "name") // "John"
val, _ = db.JsonGet("doc", "$")     // 整个文档
```

### `JsonDel(key, path string) error`

删除 JSON 路径。

```go
db.JsonDel("doc", "age")
```

### `JsonStrLen(key, path string) (int, error)`

获取 JSON 字段的字符串长度。

### `JsonArrLen(key, path string) (int, error)`

获取 JSON 数组的长度。

---

## 全文搜索

### `FtCreate(index string, schema map[string]FtFieldType) error`

创建全文索引。

```go
db.FtCreate("idx:books", map[string]FtFieldType{
    "title": FtText,
    "author": FtTag,
    "price": FtNumeric,
})
```

### `FtAdd(index string, docID string, fields map[string]interface{}) error`

添加文档。

```go
db.FtAdd("idx:books", "book1", map[string]interface{}{
    "title":  "Go Programming",
    "author": "Alice",
    "price":  29.99,
})
```

### `FtSearch(index, query string) *ZSlices`

搜索文档（返回匹配的文档 ID 列表）。

```go
results := db.FtSearch("idx:books", "programming")
for i := 0; i < results.Len(); i++ {
    fmt.Println(string(results.Get(i)))
}
results.Close()
```

---

## 图查询

### `GraphAddNode(label string, nodeID string, properties map[string]interface{}) error`

添加图节点。

```go
db.GraphAddNode("Person", "Alice", map[string]interface{}{
    "age": 30,
})
```

### `GraphAddEdge(label string, fromNodeID, toNodeID string, properties map[string]interface{}) error`

添加图边。

```go
db.GraphAddEdge("KNOWS", "Alice", "Bob", nil)
```

### `GraphGetNode(label, nodeID string) (map[string]interface{}, error)`

获取图节点。

```go
node, _ := db.GraphGetNode("Person", "Alice")
```

### `GraphGetNeighbors(nodeID string) ([]string, error)`

获取节点的邻居。

```go
neighbors, _ := db.GraphGetNeighbors("Alice")
```

---

## 速率限制

### `Throttle(key string, capacity, refillRate int, refillIntervalMs int64) ThrottleResult`

滑动窗口速率限制器。返回是否允许通过、剩余容量、下次重试时间等信息。

```go
result := db.Throttle("api:login", 10, 1, 1000)
if result.Allowed {
    fmt.Printf("Allowed, remaining: %d\n", result.Remaining)
} else {
    fmt.Printf("Blocked, retry after %d ms\n", result.RetryAfterMs)
}
```

---

## 数据结构类型

Gedis 暴露了底层数据结构类型，供高级用户直接使用：

| 类型 | 说明 |
|------|------|
| `Arena` | 底层内存分配器，支持 Alloc/Free |
| `Dict` | 哈希字典，支持 Set/Get/Del/Rehash |
| `Ziplist` | 紧凑双向列表，内部使用 |
| `Skiplist` | 跳表，内部使用 |
| `Intset` | 整数集合，内部使用 |
| `PooledBuffer` | 池化可复用缓冲区，公共 API 传递层 |
| `ZSlices` | 零分配视图（ZRange 返回） |

### PooledBuffer 使用模式

```go
// 从字符串创建缓冲区
pb := gedis.Buf("hello world")

// 获取底层字节
data := pb.Bytes()

// 通过方法复制后 底层切片可安全使用
buf := make([]byte, pb.Len())
copy(buf, pb.Bytes())

// 使用完毕后归还池中
pb.Close()

// 从默认池获取指定最小容量的空缓冲区
pb2 := gedis.NewBuf(4096)
pb2.WriteString("large data")
pb2.Close()
```
