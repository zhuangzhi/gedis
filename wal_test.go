package gedis

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWALCommandSerialize(t *testing.T) {
	cmd := &Command{
		Op:   "SET",
		Key:  "key1",
		Args: [][]byte{[]byte("value1")},
	}

	data := cmd.Serialize()
	if len(data) == 0 {
		t.Fatal("serialized data is empty")
	}

	cmd2, err := DeserializeCommand(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	if cmd2.Op != cmd.Op {
		t.Fatalf("expected op %s, got %s", cmd.Op, cmd2.Op)
	}
	if cmd2.Key != cmd.Key {
		t.Fatalf("expected key %s, got %s", cmd.Key, cmd2.Key)
	}
	if len(cmd2.Args) != len(cmd.Args) {
		t.Fatalf("expected %d args, got %d", len(cmd.Args), len(cmd2.Args))
	}
}

func TestWALRecordSerialize(t *testing.T) {
	data := []byte("test command data")
	record := &WALRecord{
		Magic:     [3]byte{'W', 'A', 'L'},
		Timestamp: time.Now().UnixNano(),
		DataLen:   uint32(len(data)),
		Data:      data,
	}

	bytes, err := record.Serialize()
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	parsed, err := ParseWALRecord(bytes)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if parsed.Timestamp != record.Timestamp {
		t.Fatalf("expected timestamp %d, got %d", record.Timestamp, parsed.Timestamp)
	}
	if string(parsed.Data) != string(record.Data) {
		t.Fatalf("expected data %s, got %s", string(record.Data), string(parsed.Data))
	}
}

func TestWALRecordCRCError(t *testing.T) {
	data := []byte("test command data")
	record := &WALRecord{
		Magic:     [3]byte{'W', 'A', 'L'},
		Timestamp: time.Now().UnixNano(),
		DataLen:   uint32(len(data)),
		Data:      data,
	}

	bytes, _ := record.Serialize()

	bytes[10] ^= 0xFF

	_, err := ParseWALRecord(bytes)
	if err == nil {
		t.Fatal("expected CRC error")
	}
}

func TestWALWriterDisabled(t *testing.T) {
	config := WALConfig{
		Enabled: false,
		Path:    "/tmp/test.wal",
	}

	writer, err := NewWALWriter(config)
	if err != nil {
		t.Fatalf("create WAL writer failed: %v", err)
	}

	cmd := &Command{Op: "SET", Key: "k1", Args: [][]byte{[]byte("v1")}}
	err = writer.Append(cmd)
	if err != nil {
		t.Fatalf("append should not fail when disabled: %v", err)
	}

	err = writer.Close()
	if err != nil {
		t.Fatalf("close should not fail when disabled: %v", err)
	}
}

func TestWALWriterEnabled(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_wal_enabled_"+time.Now().Format("20060102150405")+".wal")
	defer os.Remove(tmpFile)

	config := WALConfig{
		Enabled:      true,
		Path:         tmpFile,
		Fsync:        FsyncNo,
		BatchSize:    1024,
		BatchTimeout: time.Second,
	}

	writer, err := NewWALWriter(config)
	if err != nil {
		t.Fatalf("create WAL writer failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		cmd := &Command{
			Op:   "SET",
			Key:  "key",
			Args: [][]byte{[]byte("value")},
		}
		if err := writer.Append(cmd); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	offset := writer.Offset()
	if offset <= 0 {
		t.Fatal("expected positive offset")
	}

	if err := writer.Flush(true); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestWALReader(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_wal_reader_"+time.Now().Format("20060102150405")+".wal")
	defer os.Remove(tmpFile)

	config := WALConfig{
		Enabled:      true,
		Path:         tmpFile,
		Fsync:        FsyncNo,
		BatchSize:    1024,
		BatchTimeout: time.Second,
	}

	writer, err := NewWALWriter(config)
	if err != nil {
		t.Fatalf("create WAL writer failed: %v", err)
	}

	testCases := []struct {
		op   string
		key  string
		args [][]byte
	}{
		{"SET", "k1", [][]byte{[]byte("v1")}},
		{"SET", "k2", [][]byte{[]byte("v2")}},
		{"HSET", "h1", [][]byte{[]byte("f1"), []byte("v3")}},
	}

	for _, tc := range testCases {
		cmd := &Command{Op: tc.op, Key: tc.key, Args: tc.args}
		if err := writer.Append(cmd); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	writer.Flush(true)
	writer.Close()

	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("create WAL reader failed: %v", err)
	}
	defer reader.Close()

	for i, tc := range testCases {
		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("read command %d failed: %v", i, err)
		}
		if cmd.Op != tc.op {
			t.Fatalf("command %d: expected op %s, got %s", i, tc.op, cmd.Op)
		}
		if cmd.Key != tc.key {
			t.Fatalf("command %d: expected key %s, got %s", i, tc.key, cmd.Key)
		}
	}
}

func TestWALWriterSwitch(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_wal_switch_"+time.Now().Format("20060102150405")+".wal")
	tmpFileNew := tmpFile + ".new"
	defer os.Remove(tmpFile)
	defer os.Remove(tmpFileNew)

	config := WALConfig{
		Enabled:      true,
		Path:         tmpFile,
		Fsync:        FsyncNo,
		BatchSize:    1024,
		BatchTimeout: time.Second,
	}

	writer, err := NewWALWriter(config)
	if err != nil {
		t.Fatalf("create WAL writer failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		cmd := &Command{Op: "SET", Key: "key", Args: [][]byte{[]byte("value")}}
		writer.Append(cmd)
	}
	writer.Flush(true)

	if err := writer.SwitchTo(tmpFileNew); err != nil {
		t.Fatalf("switch failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		cmd := &Command{Op: "SET", Key: "key2", Args: [][]byte{[]byte("value2")}}
		writer.Append(cmd)
	}
	writer.Flush(true)
	writer.Close()
}

func TestIsWriteCommand(t *testing.T) {
	writeCmds := []string{
		"SET", "SETNX", "SETEX", "APPEND", "DEL",
		"HSET", "HDEL", "LPUSH", "RPUSH", "SADD",
		"ZADD", "ZREM", "INCR", "INCRBY", "XADD",
	}

	for _, cmd := range writeCmds {
		if !IsWriteCommand(cmd) {
			t.Fatalf("expected %s to be a write command", cmd)
		}
	}

	readCmds := []string{
		"GET", "HGET", "LRANGE", "SMEMBERS", "ZRANGE",
		"EXISTS", "TTL", "TYPE", "DUMP",
	}

	for _, cmd := range readCmds {
		if IsWriteCommand(cmd) {
			t.Fatalf("expected %s to not be a write command", cmd)
		}
	}
}

func TestPersistenceConfigJSON(t *testing.T) {
	jsonStr := `{
		"wal": {
			"enabled": true,
			"path": "/tmp/wal.log",
			"fsync": "everysec",
			"batch_size": 4096,
			"batch_timeout": "500ms"
		},
		"rdb": {
			"enabled": true,
			"path": "/tmp/dump.rdb",
			"save_interval": "60s",
			"save_on_shutdown": true,
			"max_backups": 3,
			"write_count_trigger": 1000
		}
	}`

	cfg, err := ParsePersistenceConfig(jsonStr)
	if err != nil {
		t.Fatalf("parse config failed: %v", err)
	}

	if !cfg.WAL.Enabled {
		t.Fatal("expected WAL to be enabled")
	}
	if cfg.WAL.Path != "/tmp/wal.log" {
		t.Fatalf("expected WAL path /tmp/wal.log, got %s", cfg.WAL.Path)
	}
	if cfg.WAL.Fsync != FsyncEverySec {
		t.Fatalf("expected fsync everysec, got %s", cfg.WAL.Fsync)
	}
	if cfg.WAL.BatchSize != 4096 {
		t.Fatalf("expected batch size 4096, got %d", cfg.WAL.BatchSize)
	}
	if cfg.WAL.BatchTimeout != 500*time.Millisecond {
		t.Fatalf("expected batch timeout 500ms, got %v", cfg.WAL.BatchTimeout)
	}

	if !cfg.RDB.Enabled {
		t.Fatal("expected RDB to be enabled")
	}
	if cfg.RDB.Path != "/tmp/dump.rdb" {
		t.Fatalf("expected RDB path /tmp/dump.rdb, got %s", cfg.RDB.Path)
	}
	if cfg.RDB.SaveInterval != 60*time.Second {
		t.Fatalf("expected save interval 60s, got %v", cfg.RDB.SaveInterval)
	}
	if !cfg.RDB.SaveOnShutdown {
		t.Fatal("expected save on shutdown")
	}
	if cfg.RDB.MaxBackups != 3 {
		t.Fatalf("expected max backups 3, got %d", cfg.RDB.MaxBackups)
	}
	if cfg.RDB.WriteCountTrigger != 1000 {
		t.Fatalf("expected write count trigger 1000, got %d", cfg.RDB.WriteCountTrigger)
	}
}

func TestWALLargeDataWithCompression(t *testing.T) {
	numKeys := 1024
	valueSize := 1024 * 1024

	tmpDir := filepath.Join(os.TempDir(), "test_wal_large_"+time.Now().Format("20060102150405"))
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	walPath := filepath.Join(tmpDir, "test.wal")

	config := WALConfig{
		Enabled:      true,
		Path:         walPath,
		Fsync:        FsyncNo,
		BatchSize:    8192,
		BatchTimeout: 10 * time.Second,
		Compression:  true,
	}

	writer, err := NewWALWriter(config)
	if err != nil {
		t.Fatalf("create WAL writer failed: %v", err)
	}

	value := make([]byte, valueSize)
	rand.Read(value)

	start := time.Now()
	for i := 0; i < numKeys; i++ {
		cmd := &Command{
			Op:   "SET",
			Key:  fmt.Sprintf("key_%d", i),
			Args: [][]byte{value},
		}
		if err := writer.Append(cmd); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	writer.Flush(true)
	writer.Close()

	writeDuration := time.Since(start)

	walInfo, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal file failed: %v", err)
	}

	uncompressedSize := int64(numKeys) * int64(8+valueSize)
	compressionRatio := float64(uncompressedSize) / float64(walInfo.Size())

	t.Logf("Write %d keys x %d bytes = %d bytes (%.2f MB)",
		numKeys, valueSize, uncompressedSize, float64(uncompressedSize)/1024/1024)
	t.Logf("Compressed WAL size: %d bytes (%.2f MB)", walInfo.Size(), float64(walInfo.Size())/1024/1024)
	t.Logf("Compression ratio: %.2f%%", compressionRatio*100)
	t.Logf("Write duration: %v", writeDuration)

	reader, err := NewWALReader(walPath)
	if err != nil {
		t.Fatalf("create WAL reader failed: %v", err)
	}
	defer reader.Close()

	readStart := time.Now()
	readCount := 0
	for {
		cmd, err := reader.ReadCommand()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read command failed: %v", err)
		}
		if cmd.Op != "SET" {
			t.Fatalf("expected SET command, got %s", cmd.Op)
		}
		readCount++
	}

	readDuration := time.Since(readStart)

	t.Logf("Read %d commands in %v", readCount, readDuration)

	if readCount != numKeys {
		t.Fatalf("expected %d commands, got %d", numKeys, readCount)
	}

	throughput := float64(uncompressedSize) / writeDuration.Seconds() / 1024 / 1024
	t.Logf("Write throughput: %.2f MB/s", throughput)

	readThroughput := float64(uncompressedSize) / readDuration.Seconds() / 1024 / 1024
	t.Logf("Read throughput: %.2f MB/s", readThroughput)
}
