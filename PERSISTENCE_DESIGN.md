# Gedis 数据持久化方案设计

> 目标：在保证高性能的前提下，实现节点故障后**最多丢失 1 秒内的写操作**（可配置），同时恢复时间可控。

---

## 一、核心思路

借鉴 Redis AOF + RDB 混合持久化模式：

- **实时 WAL（Write-Ahead Log）**：每个写命令在执行前，先追加写入磁盘上的日志文件。保证即使进程崩溃，已回复客户端成功的数据也已经持久化。
- **定期快照（RDB）**：每隔一段时间（或写操作次数）将整个内存数据（MultiArena + Dict）以二进制快照形式保存到磁盘。
- **恢复流程**：先加载最新的 RDB 快照，再重放 WAL 中快照之后的所有命令。

**组合效果**：
- 最多丢失 WAL 中尚未落盘的部分（可控制在 fsync 策略内，比如 1 秒）。
- 恢复时不需要重放全部历史 WAL，只需重放最后一次快照之后的增量，大大缩短恢复时间。

---

## 二、WAL 设计

### 2.1 WAL 记录格式

每个写命令被序列化为二进制记录，追加写入 `wal.log` 文件。记录格式：

```
+---------+---------+---------+---------+---------+---------+--------------------+
| Magic   | CRC32   | Timestamp| DataLen | StoredLen| Compressed| Data bytes         |
| 3 bytes | 4 bytes | 8 bytes  | 4 bytes | 4 bytes | 1 byte   | (variable)         |
+---------+---------+---------+---------+---------+---------+--------------------+
```

| 字段 | 大小 | 说明 |
|------|------|------|
| Magic | 3 bytes | 固定值 `WAL`，用于快速定位记录边界 |
| CRC32 | 4 bytes | 对整条记录（不含自身）的校验，用于检测磁盘损坏 |
| Timestamp | 8 bytes | 纳秒级时间戳，用于恢复时排序 |
| DataLen | 4 bytes | 原始命令序列化后的长度 |
| StoredLen | 4 bytes | 存储在磁盘上的数据长度（压缩后可能更小） |
| Compressed | 1 byte | 0=未压缩，1=LZ4压缩 |
| Data bytes | variable | 命令的二进制编码，包含操作类型、key、参数等（可压缩） |

每个命令在执行前通过 `write(fd, record)` 写入操作系统的页缓存，然后根据 `fsync` 策略决定何时强制落盘。

### 2.2 命令序列化

```go
type Command struct {
    Op     string   // "SET", "HSET", "ZADD", "DEL", ...
    Args   [][]byte // 参数，第一个通常是 key
}
```

序列化方式：长度前缀 + 原始字节（可以使用 msgpack 或自定义格式）。

### 2.3 fsync 策略

| 策略 | 行为 | 丢失风险 | 性能 |
|------|------|----------|------|
| `always` | 每个命令后调用 `fsync` | 几乎为 0 | 极低 |
| `everysec` | 每秒调用一次 `fsync` | 最多 1 秒的数据 | 较高（推荐） |
| `no` | 由操作系统决定（通常 30 秒） | 可能丢失 30 秒数据 | 最高 |

对于要求**数据不丢失**的场景，选择 **`everysec`** 是性价比最高的方案。

### 2.4 WAL 文件管理

- 单个 WAL 文件无限增长会拖慢恢复，需要配合快照进行**重写（rewrite）**。
- 每次快照完成后，创建一个新的 WAL 文件（`wal.log` → `wal.log.old`），后续命令写入新文件。
- 恢复时只需要 `最新快照 + 对应的增量 WAL`。

### 2.5 并发写入优化（组提交）

```go
type WALWriter struct {
    mu            sync.Mutex
    buffer        bytes.Buffer
    lastFlush     time.Time
    batchSize     int           // 累积字节数
    batchTimeout  time.Duration // 超时时间
    commands      chan []byte   // 异步写入通道
    done          chan struct{}
}

func (w *WALWriter) Append(cmd []byte) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    w.buffer.Write(cmd)

    if w.buffer.Len() >= w.batchSize ||
       time.Since(w.lastFlush) > w.batchTimeout {
        return w.flush(true)  // fsync
    }
    return nil
}
```

### 2.6 LZ4 压缩支持

Gedis WAL 支持 LZ4 压缩，可显著减少磁盘空间占用：

```go
type WALConfig struct {
    Enabled      bool
    Path         string
    Fsync        FsyncPolicy
    BatchSize    int
    BatchTimeout time.Duration
    Compression  bool  // 启用 LZ4 压缩
}
```

**压缩性能测试数据（1GB 数据）：**

| 数据类型 | 原始大小 | 压缩后 | 压缩比 | 写入吞吐 | 读取吞吐 |
|----------|----------|--------|--------|----------|----------|
| 随机数据 | 1024 MB | 1024 MB | 1:1 | ~736 MB/s | ~950 MB/s |
| 重复数据 | 1024 MB | 4.3 MB | 236:1 | ~1908 MB/s | ~2009 MB/s |

**实际业务数据压缩效果（3-10 倍）：**

| 数据类型 | 压缩比 | 有效写入吞吐 |
|----------|--------|--------------|
| JSON/文本日志 | 3-5:1 | 2-3 GB/s |
| 配置文件 | 5-10:1 | 3-5 GB/s |
| 序列化对象 | 2-4:1 | 1.5-2.5 GB/s |

**压缩算法选择 LZ4 的原因：**
- **极快的压缩/解压速度**：压缩速度可达数 GB/s
- **低 CPU 开销**：适合高并发写入场景
- **合理的压缩比**：比 zlib 快 10 倍，压缩比稍低
- **适合实时压缩**：边写边压，不阻塞业务

**注意事项：**
- 压缩会增加约 5-10% 的 CPU 开销
- 对于已经是压缩格式的数据（如图片、加密数据）压缩效果差
- 生产环境建议开启压缩，除非数据完全随机

---

## 三、快照与 WAL 协同

### 3.1 快照生成时机

- **定时触发**：每 N 秒（如 60 秒）自动执行一次后台快照。
- **写次数触发**：每 M 次写操作触发。
- **手动触发**：执行 `SAVE` 或 `BGSAVE` 命令。

### 3.2 快照期间 WAL 处理流程

1. 开始快照前，先 `fsync` 当前 WAL，确保所有之前的命令已落盘。
2. 创建新 WAL 文件（例如 `wal.log` → `wal_<timestamp>.log`，新命令写入 `wal.log.new`）。
3. 后台执行快照（使用元数据复制或 fork 子进程）。
4. 快照完成后，将新 WAL 文件原子重命名为活跃文件，删除旧的 WAL。

### 3.3 快照文件格式

RDB 文件头中记录 **快照的最后一个 WAL 位置（偏移量）**，用于恢复时决定从何处开始重放。

```
[Header]
  Magic           5 bytes   // "GEDIS"
  Version         4 bytes   // 格式版本
  CreateTime      8 bytes   // 创建时间戳
  LastWALOffset   8 bytes   // 创建快照时，当前 WAL 文件的写入偏移量
  ArenaCount      4 bytes   // Arena 数量
  DictCount       4 bytes   // Dict 键数量
  CRC32           4 bytes   // 头部校验和
[Data]
  Arena[]                   // 多Arena数据
  Dict[]                    // 键值对数据
```

**恢复时**：
1. 加载 `dump.rdb` 到内存。
2. 打开 `wal.log`，从 `LastWALOffset` 处开始读取并重放命令。

---

## 四、恢复流程详解

```go
func (db *RedisDB) Recover() error {
    // 1. 加载最新的 RDB 快照
    rdbFile := findLatestRDB()
    if rdbFile != "" {
        if err := db.LoadFromFile(rdbFile); err != nil {
            return err
        }
    } else {
        db.arena = NewMultiArena(...)
        db.dict = NewDict()
    }

    // 2. 找到对应的 WAL 文件（与 RDB 同名前缀）
    walFile := rdbFile + ".wal"
    if _, err := os.Stat(walFile); os.IsNotExist(err) {
        return nil // 没有 WAL，恢复结束
    }

    // 3. 从快照中记录的偏移量开始重放
    snapshotOffset := db.getLastWALOffset() // 从 RDB 头部读取
    f, _ := os.Open(walFile)
    defer f.Close()
    f.Seek(snapshotOffset, io.SeekStart)

    reader := newWALReader(f)
    for {
        cmd, err := reader.ReadCommand()
        if err == io.EOF {
            break
        }
        if err != nil {
            log.Warn("corrupt wal entry, skip", err)
            // 从下一个 magic bytes 继续扫描
            continue
        }
        db.executeCommand(cmd) // 直接执行，不写 WAL（防止循环）
    }
    return nil
}
```

### 4.1 命令执行与 WAL 写入顺序

```
Client Request
      │
      ▼
┌─────────────┐
│  Write WAL  │ ──► 成功 ──► Execute Command ──► Reply Client
└─────────────┘
      │                      │
      └── 失败 ──► 返回错误（不执行）
```

**保证**：客户端感知到的成功 = 数据已持久化。

---

## 五、性能优化与权衡

### 5.1 批量 fsync

- 将多个命令合并写入后再 `fsync`，减少系统调用次数。
- 例如：每 100ms 或累积 1KB 数据执行一次 `fsync`。

### 5.2 快照期间的服务影响

- **fork 方案（Linux）**：子进程内存快照，父进程继续服务。COW 会略微增加内存，但性能影响很小。
- **纯 Go 元数据复制方案**：需短暂加锁复制 Arena 列表和 Dict，期间写操作被阻塞（几微秒到几十微秒）。

### 5.3 磁盘空间管理

- 定期删除旧的 WAL 和 RDB 文件，保留最近 2~3 份。
- WAL 文件可开启压缩（如 LZ4），但会增加 CPU 开销。

---

## 六、配置示例

```yaml
persistence:
  wal:
    enabled: true
    path: "/var/gedis/wal.log"
    fsync: "everysec"           # always, everysec, no
    batch_size: 4096           # 累积 4KB 后 flush
    compression: true           # 启用 LZ4 压缩（推荐）
  rdb:
    enabled: true
    path: "/var/gedis/dump.rdb"
    save_interval: 60          # 秒
    save_on_shutdown: true
    compression: "zstd"
    max_backups: 3             # 保留快照数量
  recovery:
    max_replay_time_sec: 300   # 恢复时最多重放 300 秒的 WAL
    replay_speed: "normal"     # "fast" 跳过校验，"normal" 全量重放
```

---

## 七、数据丢失风险分析

| 故障场景 | 数据丢失可能性 | 恢复方式 |
|----------|----------------|----------|
| 进程崩溃（everysec） | 最多丢失 1 秒内未 fsync 的命令 | 加载 RDB + 重放 WAL 到崩溃点 |
| 操作系统崩溃（everysec） | 同上 | 同上 |
| 磁盘损坏（WAL 文件损坏） | 可能丢失自上次完整快照以来的所有数据 | 仅加载 RDB，丢弃损坏 WAL |
| 磁盘写满导致 WAL 写入失败 | 命令会失败，不丢失已成功的数据 | - |

---

## 七、工业级数据安全保障

### 7.1 RAID 与持久化配合

| RAID 级别 | 容错能力 | 数据安全性 | 推荐场景 |
|-----------|----------|-----------|----------|
| RAID 0 | 无 | ❌ 任意盘故障 = 全丢 | 测试环境 |
| RAID 1 | 50% 盘故障 | ✅ 高 | 高可靠性需求 |
| RAID 5 | 1 块盘故障 | ⚠️ 重建时有风险 | 一般生产环境 |
| RAID 6 | 2 块盘故障 | ✅ 较高 | 高可靠性需求 |
| RAID 10 | 半数盘故障 | ✅ 最高 | 金融级应用 |

**RAID 无法防止的场景：**
- 控制器故障
- 多盘同时故障（RAID 5/6 重建期间）
- 逻辑错误（软件 bug、人为误操作）
- 物理灾难（火灾、洪水）

### 7.2 Fsync 策略与 UPS 配合

| Fsync 策略 | 数据安全性 | 性能 | 推荐配合 |
|------------|------------|------|----------|
| `always` | ✅ 最高 | ❌ 最低 | RAID + UPS（金融级） |
| `everysec` | ✅ 最多丢 1 秒 | ⚠️ 中等 | RAID（生产环境推荐） |
| `no` | ❌ 依赖 OS | ✅ 最高 | SSD + UPS（高并发场景） |

### 7.3 完整数据安全方案

```
┌─────────────────────────────────────────────────────────┐
│                     数据安全架构                          │
├─────────────────────────────────────────────────────────┤
│                                                         │
│   应用层: Gedis (FsyncEverySec)                         │
│       │                                                 │
│       ▼                                                 │
│   文件系统: XFS / EXT4 (with barrier)                   │
│       │                                                 │
│       ▼                                                 │
│   RAID 控制器: RAID 6 / RAID 10                         │
│       │                                                 │
│       ▼                                                 │
│   物理磁盘: SSD / NVMe (with power-loss protection)     │
│       │                                                 │
│       ▼                                                 │
│   UPS: 断电保护                                          │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

**安全等级对比：**

| 方案 | 进程崩溃 | OS 崩溃 | 断电 | 磁盘故障 | 灾难 |
|------|----------|---------|------|----------|------|
| FsyncNo + RAID 6 | ❌ 丢 30s | ❌ 丢 30s | ❌ 可能丢大量 | ✅ | ❌ |
| FsyncEverySec + RAID 6 | ✅ 丢 1s | ✅ 丢 1s | ✅ 丢 1s | ✅ | ❌ |
| FsyncAlways + RAID 10 + UPS | ✅ 不丢 | ✅ 不丢 | ✅ 不丢 | ✅ | ❌ |

### 7.4 备份策略建议

- **本地快照**：每小时/每天自动 RDB 快照
- **异地备份**：将 RDB + WAL 定期复制到异地数据中心
- **定期演练**：每季度测试恢复流程

---

## 八、与纯 RDB 或纯 AOF 的对比

| 方案 | 恢复速度 | 数据丢失窗口 | 运行时性能影响 |
|------|----------|--------------|----------------|
| 仅 RDB | 快 | 几分钟 | 极小 |
| 仅 AOF | 慢（需重放全部） | 1 秒（everysec） | 中等（写放大） |
| **混合（RDB+WAL）** | **快**（只重放增量） | **1 秒** | 中等（需同时写 WAL） |

混合模式在保障数据安全的同时，将恢复时间控制在可接受范围。

---

## 九、关键实现要点

### 9.1 WALWriter 模块

```go
type WALWriter struct {
    file          *os.File
    mu            sync.Mutex
    buffer        bytes.Buffer
    lastFlush     time.Time
    batchSize     int
    batchTimeout  time.Duration
}

func (w *WALWriter) Append(cmd *Command) error
func (w *WALWriter) Flush(sync bool) error
func (w *WALWriter) ReplayFrom(offset int64, db *RedisDB) error
```

### 9.2 命令序列化格式

| 类型 | 编码格式 |
|------|----------|
| Op | 1 byte 类型码 + 变长字符串 |
| Key | 变长字符串（长度前缀） |
| Args | N × 变长字符串 |

### 9.3 快照与 WAL 协同

- 在 `SaveToFile` 开始时，先调用 `WAL.Flush(true)`（强制 fsync）。
- 获取当前 WAL 偏移量，写入 RDB 头部。
- 然后切换到新的 WAL 文件（原子重命名）。

---

## 十二、总结

通过 **实时 WAL + 定期 RDB 快照** 的组合方案，Gedis 可以达到：

- **数据持久化级别**：最多丢失 1 秒内的写操作（`everysec` 策略）。
- **恢复时间**：RDB 加载时间 + 轻量级 WAL 重放时间（通常是秒到分钟级）。
- **性能**：写入路径增加一次序列化和异步 fsync，对吞吐影响通常小于 20%。
- **存储效率**：启用 LZ4 压缩后，磁盘占用可减少 3-10 倍（取决于数据特性）。

**关键特性：**

| 特性 | 说明 |
|------|------|
| WAL 格式 | 支持 LZ4 压缩，包含 DataLen/StoredLen 用于正确处理压缩数据 |
| 压缩性能 | 随机数据 ~736 MB/s，可压缩数据 2-5 GB/s |
| 数据安全 | FsyncEverySec + RAID 6 = 最多丢 1 秒数据 |
| 工业级部署 | FsyncAlways + RAID 10 + UPS = 接近零丢失 |

此方案兼顾了数据安全、恢复速度、存储效率和运行时性能，是 Gedis 生产环境持久化的推荐选择。
