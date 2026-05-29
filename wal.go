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
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pierrec/lz4/v4"
)

const (
	walMagic       = "WAL"
	walMagicLen    = 3
	walHeaderSize  = walMagicLen + 4 + 8 + 4 + 4 + 1 // Magic + CRC32 + Timestamp + DataLen + StoredLen + Compressed
	walVersion      = 1
)

type FsyncPolicy string

const (
	FsyncAlways  FsyncPolicy = "always"
	FsyncEverySec FsyncPolicy = "everysec"
	FsyncNo       FsyncPolicy = "no"
)

type WALConfig struct {
	Enabled      bool
	Path         string
	Fsync        FsyncPolicy
	BatchSize    int
	BatchTimeout time.Duration
	Compression  bool
}

type WALRecord struct {
	Magic      [3]byte
	CRC32      uint32
	Timestamp  int64
	DataLen    uint32
	StoredLen  uint32
	Compressed bool
	Data       []byte
}

type Command struct {
	Op   string
	Key  string
	Args [][]byte
}

func (c *Command) Serialize() []byte {
	var buf bytes.Buffer
	buf.Grow(64)

	buf.WriteByte(byte(len(c.Op)))
	buf.WriteString(c.Op)

	buf.WriteByte(byte(len(c.Key)))
	buf.WriteString(c.Key)

	buf.WriteByte(byte(len(c.Args)))
	for _, arg := range c.Args {
		var argLen [4]byte
		binary.BigEndian.PutUint32(argLen[:], uint32(len(arg)))
		buf.Write(argLen[:])
		buf.Write(arg)
	}

	return buf.Bytes()
}

func DeserializeCommand(data []byte) (*Command, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("data too short")
	}

	pos := 0

	opLen := int(data[pos])
	pos++
	if pos+opLen > len(data) {
		return nil, fmt.Errorf("invalid op length")
	}
	op := string(data[pos : pos+opLen])
	pos += opLen

	keyLen := int(data[pos])
	pos++
	if pos+keyLen > len(data) {
		return nil, fmt.Errorf("invalid key length")
	}
	key := string(data[pos : pos+keyLen])
	pos += keyLen

	argsLen := int(data[pos])
	pos++
	args := make([][]byte, argsLen)
	for i := 0; i < argsLen; i++ {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("invalid arg length")
		}
		argLen := int(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		if pos+argLen > len(data) {
			return nil, fmt.Errorf("invalid arg data")
		}
		args[i] = data[pos : pos+argLen]
		pos += argLen
	}

	return &Command{Op: op, Key: key, Args: args}, nil
}

func (r *WALRecord) Serialize() ([]byte, error) {
	var buf bytes.Buffer
	dataToWrite := r.Data
	r.StoredLen = uint32(len(dataToWrite))

	if r.Compressed {
		var compressed bytes.Buffer
		writer := lz4.NewWriter(&compressed)
		_, err := writer.Write(r.Data)
		if err != nil {
			return nil, fmt.Errorf("compress data: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("close lz4 writer: %w", err)
		}
		dataToWrite = compressed.Bytes()
		r.StoredLen = uint32(len(dataToWrite))
	}

	buf.Grow(walHeaderSize + int(r.StoredLen))

	buf.Write(r.Magic[:])

	var crcBuf bytes.Buffer
	binary.Write(&crcBuf, binary.BigEndian, r.Timestamp)
	binary.Write(&crcBuf, binary.BigEndian, r.DataLen)
	binary.Write(&crcBuf, binary.BigEndian, r.StoredLen)
	crcBuf.WriteByte(boolToByte(r.Compressed))
	crcBuf.Write(dataToWrite)
	r.CRC32 = crc32.ChecksumIEEE(crcBuf.Bytes())
	binary.Write(&buf, binary.BigEndian, r.CRC32)

	binary.Write(&buf, binary.BigEndian, r.Timestamp)
	binary.Write(&buf, binary.BigEndian, r.DataLen)
	binary.Write(&buf, binary.BigEndian, r.StoredLen)
	buf.WriteByte(boolToByte(r.Compressed))
	buf.Write(dataToWrite)

	return buf.Bytes(), nil
}

func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func ParseWALRecord(data []byte) (*WALRecord, error) {
	if len(data) < walHeaderSize {
		return nil, fmt.Errorf("data too short for header")
	}

	pos := 0

	magic := [3]byte{data[pos], data[pos+1], data[pos+2]}
	if magic != [3]byte{'W', 'A', 'L'} {
		return nil, fmt.Errorf("invalid magic bytes")
	}
	pos += walMagicLen

	crc32Val := binary.BigEndian.Uint32(data[pos:])
	pos += 4

	timestamp := int64(binary.BigEndian.Uint64(data[pos:]))
	pos += 8

	dataLen := binary.BigEndian.Uint32(data[pos:])
	pos += 4

	storedLen := binary.BigEndian.Uint32(data[pos:])
	pos += 4

	compressed := data[pos] != 0
	pos += 1

	if pos+int(storedLen) > len(data) {
		return nil, fmt.Errorf("stored data length exceeds buffer")
	}
	storedData := data[pos : pos+int(storedLen)]

	var crcBuf bytes.Buffer
	binary.Write(&crcBuf, binary.BigEndian, timestamp)
	binary.Write(&crcBuf, binary.BigEndian, dataLen)
	binary.Write(&crcBuf, binary.BigEndian, storedLen)
	crcBuf.WriteByte(boolToByte(compressed))
	crcBuf.Write(storedData)
	computedCRC := crc32.ChecksumIEEE(crcBuf.Bytes())
	if computedCRC != crc32Val {
		return nil, fmt.Errorf("CRC mismatch: expected %x, got %x", computedCRC, crc32Val)
	}

	var recordData []byte
	if compressed {
		var decompressed bytes.Buffer
		reader := lz4.NewReader(bytes.NewReader(storedData))
		if _, err := io.Copy(&decompressed, reader); err != nil {
			return nil, fmt.Errorf("decompress data: %w", err)
		}
		recordData = decompressed.Bytes()
	} else {
		recordData = storedData
	}

	return &WALRecord{
		Magic:      magic,
		CRC32:      crc32Val,
		Timestamp:  timestamp,
		DataLen:    dataLen,
		StoredLen:  storedLen,
		Compressed: compressed,
		Data:       recordData,
	}, nil
}

type WALWriter struct {
	file         *os.File
	mu           sync.Mutex
	buffer       bytes.Buffer
	lastFlush    time.Time
	config       WALConfig
	bgFlushChan  chan struct{}
	doneChan     chan struct{}
	writeCount   int64
	offset       int64
}

func NewWALWriter(config WALConfig) (*WALWriter, error) {
	if !config.Enabled {
		return &WALWriter{config: config}, nil
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 4096
	}
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = time.Second
	}

	file, err := os.OpenFile(config.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open wal file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat wal file: %w", err)
	}

	w := &WALWriter{
		file:        file,
		config:      config,
		lastFlush:   time.Now(),
		bgFlushChan: make(chan struct{}, 1),
		doneChan:    make(chan struct{}),
		offset:      stat.Size(),
	}

	if config.Fsync == FsyncEverySec {
		go w.backgroundFlush()
	}

	return w, nil
}

func (w *WALWriter) Append(cmd *Command) error {
	if w.file == nil {
		return nil
	}

	data := cmd.Serialize()

	record := &WALRecord{
		Magic:      [3]byte{'W', 'A', 'L'},
		Timestamp:  time.Now().UnixNano(),
		DataLen:    uint32(len(data)),
		Compressed: w.config.Compression,
		Data:       data,
	}

	recordBytes, err := record.Serialize()
	if err != nil {
		return fmt.Errorf("serialize record: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer.Write(recordBytes)
	w.writeCount++
	w.offset += int64(len(recordBytes))

	if w.config.Fsync == FsyncAlways {
		return w.flush(true)
	}

	if w.buffer.Len() >= w.config.BatchSize {
		return w.flush(false)
	}

	return nil
}

func (w *WALWriter) flush(sync bool) error {
	if w.buffer.Len() == 0 {
		return nil
	}

	if _, err := w.file.Write(w.buffer.Bytes()); err != nil {
		return fmt.Errorf("write to wal: %w", err)
	}

	if sync {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("fsync wal: %w", err)
		}
	}

	w.buffer.Reset()
	w.lastFlush = time.Now()
	return nil
}

func (w *WALWriter) backgroundFlush() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			if time.Since(w.lastFlush) >= time.Second && w.buffer.Len() > 0 {
				w.flush(false)
			}
			w.mu.Unlock()
		case <-w.bgFlushChan:
			w.mu.Lock()
			if w.buffer.Len() > 0 {
				w.flush(true)
			}
			w.mu.Unlock()
			return
		}
	}
}

func (w *WALWriter) Flush(sync bool) error {
	if w.file == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.config.Fsync == FsyncEverySec && !sync {
		select {
		case w.bgFlushChan <- struct{}{}:
		default:
		}
		return nil
	}

	return w.flush(sync)
}

func (w *WALWriter) Close() error {
	if w.file == nil {
		return nil
	}

	if w.config.Fsync == FsyncEverySec {
		select {
		case w.bgFlushChan <- struct{}{}:
		case <-w.doneChan:
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.flush(true); err != nil {
		return err
	}

	return w.file.Close()
}

func (w *WALWriter) Offset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.offset
}

func (w *WALWriter) SwitchTo(newPath string) error {
	if w.file == nil {
		return nil
	}

	w.mu.Lock()
	if err := w.flush(true); err != nil {
		w.mu.Unlock()
		return err
	}
	w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close old wal: %w", err)
	}

	backupPath := w.config.Path + ".old"
	os.Remove(backupPath)
	if err := os.Rename(w.config.Path, backupPath); err != nil {
		return fmt.Errorf("rename old wal: %w", err)
	}

	file, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open new wal: %w", err)
	}

	w.mu.Lock()
	w.file = file
	w.config.Path = newPath
	w.buffer.Reset()
	w.offset = 0
	w.mu.Unlock()

	return nil
}

type WALReader struct {
	file   *os.File
	offset int64
}

func NewWALReader(path string) (*WALReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open wal file: %w", err)
	}

	return &WALReader{
		file:   file,
		offset: 0,
	}, nil
}

func (r *WALReader) Seek(offset int64) error {
	r.offset = offset
	_, err := r.file.Seek(offset, io.SeekStart)
	return err
}

func (r *WALReader) ReadCommand() (*Command, error) {
	header := make([]byte, walHeaderSize)
	_, err := r.file.Read(header)
	if err == io.EOF {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	pos := 0
	magic := [3]byte{header[pos], header[pos+1], header[pos+2]}
	if magic != [3]byte{'W', 'A', 'L'} {
		return nil, fmt.Errorf("invalid magic")
	}
	pos += walMagicLen

	pos += 4

	_ = int64(binary.BigEndian.Uint64(header[pos:]))
	pos += 8

	pos += 4

	storedLen := binary.BigEndian.Uint32(header[pos:])
	pos += 4

	storedData := make([]byte, storedLen)
	_, err = r.file.Read(storedData)
	if err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	fullData := make([]byte, walHeaderSize+int(storedLen))
	copy(fullData, header)
	copy(fullData[walHeaderSize:], storedData)

	record, err := ParseWALRecord(fullData)
	if err != nil {
		return nil, fmt.Errorf("parse record: %w", err)
	}

	cmd, err := DeserializeCommand(record.Data)
	if err != nil {
		return nil, fmt.Errorf("deserialize command: %w", err)
	}

	r.offset += int64(len(fullData))

	return cmd, nil
}

func (r *WALReader) Close() error {
	return r.file.Close()
}

func (r *WALReader) Replay(db *RedisDB, offset int64) error {
	if offset > 0 {
		if err := r.Seek(offset); err != nil {
			return fmt.Errorf("seek to offset %d: %w", offset, err)
		}
	}

	for {
		cmd, err := r.ReadCommand()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("replay wal: %w", err)
		}

		if err := db.ExecuteCommand(cmd); err != nil {
			return fmt.Errorf("execute command %s: %w", cmd.Op, err)
		}
	}

	return nil
}

func (db *RedisDB) ExecuteCommand(cmd *Command) error {
	switch cmd.Op {
	case "SET":
		if len(cmd.Args) >= 1 {
			db.Set(cmd.Key, cmd.Args[0])
		}
	case "DEL":
		db.Del(cmd.Key)
	case "HSET":
		if len(cmd.Args) >= 2 {
			db.HSet(cmd.Key, string(cmd.Args[0]), cmd.Args[1])
		}
	case "HDEL":
		db.HDel(cmd.Key, string(cmd.Args[0]))
	case "ZADD":
		if len(cmd.Args) >= 2 {
			score := binary.LittleEndian.Uint64(cmd.Args[0])
			db.ZAdd(cmd.Key, float64(score), cmd.Args[1])
		}
	case "ZREM":
		db.ZRem(cmd.Key, cmd.Args[0])
	case "INCR":
		db.IncrBy(cmd.Key, 1)
	case "INCRBY":
		if len(cmd.Args) >= 1 {
			delta, _ := binary.Varint(cmd.Args[0])
			db.IncrBy(cmd.Key, delta)
		}
	case "APPEND":
		if len(cmd.Args) >= 1 {
			db.Append(cmd.Key, cmd.Args[0])
		}
	default:
		return fmt.Errorf("unknown command: %s", cmd.Op)
	}
	return nil
}
