# Changelog

## v0.1.0 - 2026-05-31

### 首次发布

This is the first release of gedis, a zero-GC Redis-like in-memory database for Go.

### 核心特性

- 🔋 **Zero-GC Arena 内存管理**: 所有数据存储在单一 `[]byte` 中，无 Go 指针，零 GC 扫描
- 🏷️ **丰富的 Redis 数据结构**: String, List, Hash, Set, Sorted Set, Bitmap, HyperLogLog, Geo
- 📦 **高级数据结构**: Stream, TimeSeries (with Labels/Aggregations), JSON, Search, Graph
- 🎲 **概率数据结构**: Bloom Filter, Cuckoo Filter, Count-Min Sketch, Top-K
- ⏱️ **限流功能**: Cell 令牌桶算法支持
- 💾 **数据持久化**: WAL (Write-Ahead Log) + RDB 快照，支持 LZ4 压缩
- 🚀 **高性能设计**:
  - `*PooledBuffer` 分级对象池替代 `[]byte`
  - `*ZSlices` 紧凑存储替代 `[]string` / `[][]byte`
  - 迭代回调 API 实现零分配遍历

### API 功能

#### 键操作
- `New()`, `Del()`, `Exists()`, `FlushAll()`

#### 字符串
- `Set()`, `Get()`, `Append()`, `Strlen()`, `Incr()`, `IncrBy()`, `Decr()`, `DecrBy()`
- `MSet()`, `MGet()`, `SetNX()`, `SetEX()`, `PSETEX()`
- `GetRange()`, `SetRange()`, `GetDel()`

#### 列表
- `LPush()`, `LPushX()`, `RPush()`, `RPushX()`, `LPop()`, `RPop()`
- `LIndex()`, `LLen()`, `LRange()`, `LSet()`, `LTrim()`, `LInsert()`
- `LMove()`, `BLMove()`, `RPushLPush()`

#### 哈希
- `HSet()`, `HGet()`, `HDel()`, `HExists()`, `HLen()`
- `HGetAll()`, `HMSet()`, `HMGet()`, `HKeys()`, `HVals()`
- `HIncrBy()`, `HIncrByFloat()`, `HSetNX()`, `HStrLen()`, `HRandField()`

#### 集合
- `SAdd()`, `SRem()`, `SIsMember()`, `SInter()`, `SUnion()`, `SDiff()`
- `SInterStore()`, `SUnionStore()`, `SDiffStore()`
- `SMembers()`, `SMove()`, `SCard()`, `SRandMember()`

#### 有序集合
- `ZAdd()`, `ZRem()`, `ZScore()`, `ZCard()`, `ZCount()`, `ZRank()`, `ZRevRank()`
- `ZRange()`, `ZRangeByScore()`, `ZRevRange()`, `ZRangeWithScores()`, `ZRangeIter()`
- `ZRemRangeByRank()`, `ZRemRangeByScore()`, `ZIncrBy()`
- `ZRangeByLex()`, `ZLexCount()`
- `ZPopMin()`, `ZPopMax()`, `BZMPOP()`

#### Bitmap
- `SetBit()`, `GetBit()`, `BitCount()`, `BitPos()`, `BitOp()`, `BitField()`

#### HyperLogLog
- `PFAdd()`, `PFCount()`, `PFMerge()`

#### Geo
- `GeoAdd()`, `GeoDist()`, `GeoRadius()`, `GeoRadiusByMember()`, `GeoPos()`

#### Stream
- `XAdd()`, `XLen()`, `XRead()`, `XReadGroup()`, `XDel()`, `XTrim()`
- `XGroupCreate()`, `XGroupCreateConsumer()`, `XGroupDelConsumer()`, `XGroupDestroy()`
- `XAck()`, `XInfo()`, `XClaim()`, `XPending()`, `XAutoClaim()`

#### TimeSeries
- `TSAdd()`, `TSAddWithLabels()`, `TSGetLabels()`, `TSRange()`, `TSRevRange()`
- `TSLast()`, `TSDel()`, `TSRangeWithAgg()`, `TSRevRangeWithAgg()`
- `TSMGET()`, `TSMRANGE()`, `TSQUERYINDEX()`
- `TSCreateRule()`, `TSDeleteRule()`

#### Probabilistic
- **Bloom**: `BFReserve()`, `BFAdd()`, `BFExists()`, `BFAddMulti()`, `BFExistsMulti()`
- **Cuckoo**: `CFReserve()`, `CFAdd()`, `CFExists()`, `CFDel()`
- **CMS**: `CMSInitByDim()`, `CMSIncrBy()`, `CMSQuery()`
- **Top-K**: `TopKReserve()`, `TopKAdd()`, `TopKList()`, `TopKQuery()`

#### JSON
- `JsonSet()`, `JsonGet()`, `JsonDel()`, `JsonArrAppend()`, `JsonObjLen()`

#### Search
- `FTCreate()`, `FTAdd()`, `FTSearch()`

#### Graph
- `GraphQuery()`

#### 限流
- `Throttle()`, `CellReset()`

### 性能基准 (Intel Core Ultra 9 185H)

| 命令 | 耗时 (ns/op) | 吞吐量 (ops/s) | 分配 (B) |
|------|-------------|---------------|----------|
| SET | 45.1 | ~22M | 135 |
| GET | 76.7 | ~13M | 37 |
| DEL | 62.4 | ~16M | 22 |
| LPUSH | 46.5 | ~21M | 0 |
| RPOP | 130.8 | ~7.6M | 52 |
| HSET | 81.6 | ~12M | 5 |
| SADD | 27.6 | ~36M | 0 |
| ZRANGEITER (100) | 197 | ~5.1M | **0** |
| BITOP | 3.0 µs | ~330K | 0 |

### 依赖
- `github.com/pierrec/lz4/v4`: LZ4 压缩库

### 文档
- [README.md](./README.md) (中英双语)
- [API.md](./API.md) - 详细 API 使用文档
- [PERSISTENCE_DESIGN.md](./PERSISTENCE_DESIGN.md) - 持久化设计文档
