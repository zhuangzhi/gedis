package gedis

import "encoding/binary"

// ZSlices 是一个零分配的字符串切片，使用 PooledBuffer 紧凑存储元素。
// 格式：[4字节count][len1][data1][len2][data2]...
// 与 []string 的区别：
//   - 内存紧凑、一次分配
//   - Get(i) 返回 []byte，直接引用内部缓冲区（零拷贝）
//   - 调用方遍历后必须调用 Close() 归还底层 PooledBuffer
//
// 使用示例：
//
//	zs := db.ZRange("key", 0, -1)
//	for i := 0; i < zs.Len(); i++ {
//	    fmt.Println(string(zs.Get(i)))
//	}
//	zs.Close()
type ZSlices struct {
	buf *PooledBuffer
}

// NewZSlices 创建一个新的空 ZSlices，从池中获取缓冲区
func NewZSlices() *ZSlices {
	//默认16k
	buf := defaultBufPool.Get(1 << 14) // 默认池，见下方初始化
	// 预留4字节写 count
	buf.Write([]byte{0, 0, 0, 0})
	return &ZSlices{buf: buf}
}

// Add 添加一个成员（字节形式）
func (zs *ZSlices) Add(data []byte) {
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(data)))
	zs.buf.Write(lenBuf)
	zs.buf.Write(data)
}

// AddString 添加一个字符串成员
func (zs *ZSlices) AddString(s string) {
	zs.Add([]byte(s))
}

// Len 返回元素个数
func (zs *ZSlices) Len() int {
	if zs.buf.Len() < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(zs.buf.Bytes()[:4]))
}

// Get 获取第 i 个元素（零拷贝，直接引用内部缓冲区）
func (zs *ZSlices) Get(i int) []byte {
	if i < 0 || i >= zs.Len() {
		return nil
	}
	data := zs.buf.Bytes()
	pos := 4 // 跳过 count
	// 定位到第 i 个元素
	for idx := 0; idx < i; idx++ {
		if pos+4 > len(data) {
			return nil
		}
		sz := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4 + sz
	}
	// 读取第 i 个元素
	if pos+4 > len(data) {
		return nil
	}
	sz := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
	start := pos + 4
	end := start + sz
	if end > len(data) {
		return nil
	}
	return data[start:end]
}

// Finish 在添加完所有元素后调用，写入实际的 count 到头部
func (zs *ZSlices) Finish() {
	if zs.buf.Len() < 4 {
		return
	}
	count := 0
	data := zs.buf.Bytes()
	pos := 4
	for pos < len(data) {
		if pos+4 > len(data) {
			break
		}
		sz := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4 + sz
		count++
	}
	binary.LittleEndian.PutUint32(data[:4], uint32(count))
}

// Bytes 返回只读的底层字节切片（包含完整格式），调用后不应再修改
func (zs *ZSlices) Bytes() []byte {
	return zs.buf.Bytes()
}

// Close 释放底层缓冲区回池
func (zs *ZSlices) Close() error {
	if zs.buf != nil {
		return zs.buf.Close()
	}
	return nil
}
