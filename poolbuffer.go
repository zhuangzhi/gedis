// 分级缓冲区对象池。
// 按预设容量级别（升序）管理 bytes.Buffer，通过 sync.Pool 复用内存，
// 减少频繁分配导致的 GC 压力。缓冲区不足时自动跃迁到更高级别，
// 超出最大级别时退化为普通 bytes.Buffer（不回收）。
//
// 默认 6 档 (1K/4K/16K/64K/256K/1M) 已覆盖绝大多数场景，直接使用 NewPool 即可:
//
//	pool := NewPool()
//	pb := pool.Get(512)
//	pb.Write(data)
//	result := pb.Bytes()
//	pb.Close()
//
// 如需自定义档位，可使用 NewLeveledPool。
package gedis

import (
	"bytes"
	"sync"
)

// 默认 6 档固定容量，覆盖绝大多数使用场景
var defaultSizes = []int{
	1 << 10, // 1K
	1 << 12, // 4K
	1 << 14, // 16K
	1 << 16, // 64K
	1 << 18, // 256K
	1 << 20, // 1M
}

// NewPool 使用默认 6 档容量 (1K/4K/16K/64K/256K/1M) 创建分级对象池。
func NewPool() *LeveledPool { return NewLeveledPool(defaultSizes) }

// ============================================================================
// 全局默认池 & 便捷函数
// ============================================================================

var defaultBufPool = NewPool()

// Buf 从默认池创建包含 s 内容的 PooledBuffer。调用方使用后须 Close。
func Buf(s string) *PooledBuffer {
	pb := defaultBufPool.Get(len(s))
	pb.WriteString(s)
	return pb
}

// NewBuf 从默认池获取空 PooledBuffer。调用方使用后须 Close。
func NewBuf(minCap int) *PooledBuffer { return defaultBufPool.Get(minCap) }

// ============================================================================

// LeveledPool 分级缓冲区对象池。
// 每个级别对应一个独立的 sync.Pool，sizes 必须升序排列。
type LeveledPool struct {
	pools []*sync.Pool
	sizes []int
}

// NewLeveledPool 创建分级对象池。sizes 为各级别初始容量，须升序排列。
func NewLeveledPool(sizes []int) *LeveledPool {
	lp := &LeveledPool{
		pools: make([]*sync.Pool, len(sizes)),
		sizes: sizes,
	}
	for i, size := range sizes {
		sz := size
		level := i
		lp.pools[i] = &sync.Pool{
			New: func() interface{} {
				return &PooledBuffer{
					buf:   bytes.NewBuffer(make([]byte, 0, sz)),
					pool:  lp,
					level: level,
				}
			},
		}
	}
	return lp
}

// Get 从池中获取容量 >= minCap 的缓冲区。若超出所有级别则创建不可回收缓冲区。
func (lp *LeveledPool) Get(minCap int) *PooledBuffer {
	for i, size := range lp.sizes {
		if minCap <= size {
			pb := lp.pools[i].Get().(*PooledBuffer)
			pb.buf.Reset()
			return pb
		}
	}
	return &PooledBuffer{
		buf:   bytes.NewBuffer(make([]byte, 0, minCap)),
		pool:  nil,
		level: -1,
	}
}

func (lp *LeveledPool) put(pb *PooledBuffer) {
	if pb == nil || pb.pool != lp || pb.level < 0 {
		return
	}
	pb.buf.Reset()
	lp.pools[pb.level].Put(pb)
}

// PooledBuffer 是一个可池化复用的 bytes.Buffer 包装。
// 委托 bytes.Buffer 实现 io.ReaderWriter 接口，超出池容量时自动升级。
type PooledBuffer struct {
	buf   *bytes.Buffer
	pool  *LeveledPool // nil 表示不回收
	level int          // 当前级别索引，-1 表示不回收
}

const growFactor = 1.5

// grow 确保缓冲区有至少 need 字节的空闲容量。空间不足时从更高级别池中获取
// 缓冲区并迁移数据；超出最大级别则退化为普通扩容。
func (pb *PooledBuffer) grow(need int) error {
	if pb.buf.Cap()-pb.buf.Len() >= need {
		return nil
	}

	totalNeed := pb.buf.Len() + need
	if pb.pool == nil {
		pb.buf.Grow(need)
		return nil
	}

	paddedNeed := int(float64(totalNeed) * growFactor)
	if paddedNeed > totalNeed {
		totalNeed = paddedNeed
	}

	newLevel := -1
	for i := pb.level + 1; i < len(pb.pool.sizes); i++ {
		if totalNeed <= pb.pool.sizes[i] {
			newLevel = i
			break
		}
	}

	if newLevel < 0 {
		pb.buf.Grow(need)
		pb.pool.put(pb)
		pb.pool = nil
		pb.level = -1
		return nil
	}

	newPb := pb.pool.pools[newLevel].Get().(*PooledBuffer)
	newPb.buf.Write(pb.buf.Bytes())
	pb.pool.put(pb)
	pb.buf = newPb.buf
	pb.level = newLevel
	return nil
}

func (pb *PooledBuffer) Write(p []byte) (int, error) {
	if err := pb.grow(len(p)); err != nil {
		return 0, err
	}
	return pb.buf.Write(p)
}

func (pb *PooledBuffer) WriteString(s string) (int, error) {
	if err := pb.grow(len(s)); err != nil {
		return 0, err
	}
	return pb.buf.WriteString(s)
}

func (pb *PooledBuffer) WriteByte(c byte) error {
	if err := pb.grow(1); err != nil {
		return err
	}
	return pb.buf.WriteByte(c)
}

func (pb *PooledBuffer) WriteRune(r rune) (int, error) {
	if err := pb.grow(runeLen(r)); err != nil {
		return 0, err
	}
	return pb.buf.WriteRune(r)
}

func runeLen(r rune) int {
	if r < 0x80 {
		return 1
	}
	if r < 0x800 {
		return 2
	}
	if r < 0x10000 {
		return 3
	}
	return 4
}

func (pb *PooledBuffer) Bytes() []byte                         { return pb.buf.Bytes() }
func (pb *PooledBuffer) String() string                        { return pb.buf.String() }
func (pb *PooledBuffer) Len() int                              { return pb.buf.Len() }
func (pb *PooledBuffer) Cap() int                              { return pb.buf.Cap() }
func (pb *PooledBuffer) Reset()                                { pb.buf.Reset() }
func (pb *PooledBuffer) Truncate(n int)                        { pb.buf.Truncate(n) }
func (pb *PooledBuffer) Read(p []byte) (int, error)            { return pb.buf.Read(p) }
func (pb *PooledBuffer) Next(n int) []byte                     { return pb.buf.Next(n) }
func (pb *PooledBuffer) ReadByte() (byte, error)               { return pb.buf.ReadByte() }
func (pb *PooledBuffer) ReadBytes(delim byte) ([]byte, error)  { return pb.buf.ReadBytes(delim) }
func (pb *PooledBuffer) ReadString(delim byte) (string, error) { return pb.buf.ReadString(delim) }
func (pb *PooledBuffer) Grow(n int)                            { pb.buf.Grow(n) }

func (pb *PooledBuffer) Close() error {
	if pb.pool != nil {
		pb.pool.put(pb)
	}
	return nil
}
