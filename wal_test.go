package gedis

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWALBackgroundFlush(t *testing.T) {
	tmpDir := os.TempDir()
	walPath := filepath.Join(tmpDir, "test_bg_flush.wal")
	defer os.Remove(walPath)

	db := New()

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled:      true,
			Path:         walPath,
			Fsync:        FsyncEverySec,
			BatchSize:    10,
			BatchTimeout: 10 * time.Millisecond,
			Compression:  false,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	for i := 0; i < 20; i++ {
		cmd := &Command{
			Op:   "SET",
			Key:  "key" + string(rune('0'+i)),
			Args: [][]byte{[]byte("value" + string(rune('0'+i)))},
		}
		pm.AppendCommand(cmd)
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("expected WAL file to exist")
	}
}

func TestWALOffset(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_offset.rdb")
	walPath := filepath.Join(tmpDir, "test_offset.wal")
	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
		WAL: WALConfig{
			Enabled:   true,
			Path:      walPath,
			Fsync:     FsyncAlways,
			BatchSize: 100,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	db.Set("key1", []byte("value1"))

	pm.AppendCommand(&Command{
		Op:   "SET",
		Key:  "wal_key",
		Args: [][]byte{[]byte("wal_value")},
	})

	offset := pm.WALOffset()
	if offset <= 0 {
		t.Errorf("expected positive offset after writes, got %d", offset)
	}
}

func TestPersistenceConfig(t *testing.T) {
	cfg := &PersistenceConfig{
		WAL: WALConfig{
			Enabled:     true,
			Path:        "/tmp/wal",
			Compression: true,
		},
		RDB: RDBConfig{
			Enabled:           true,
			Path:              "/tmp/test",
			WriteCountTrigger: 100,
		},
	}
	if cfg.RDB.WriteCountTrigger != 100 {
		t.Error("expected WriteCountTrigger 100")
	}
	if !cfg.WAL.Compression {
		t.Error("expected compression enabled")
	}
}

func TestWALConfig(t *testing.T) {
	cfg := WALConfig{
		Path:        "/tmp/wal",
		Enabled:     true,
		Compression: true,
		BatchSize:   100,
		Fsync:       FsyncAlways,
	}
	if !cfg.Compression {
		t.Error("expected compress enabled")
	}
	if cfg.BatchSize != 100 {
		t.Error("expected BatchSize 100")
	}
}

func TestDeserializeCommand(t *testing.T) {
	cmd := &Command{
		Op:   "SET",
		Key:  "test_key",
		Args: [][]byte{[]byte("value1"), []byte("value2")},
	}

	data := cmd.Serialize()

	decoded, err := DeserializeCommand(data)
	if err != nil {
		t.Fatalf("DeserializeCommand failed: %v", err)
	}
	if decoded.Op != "SET" {
		t.Errorf("expected Op SET, got %s", decoded.Op)
	}
	if decoded.Key != "test_key" {
		t.Errorf("expected Key test_key, got %s", decoded.Key)
	}
	if len(decoded.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(decoded.Args))
	}
	if string(decoded.Args[0]) != "value1" {
		t.Errorf("expected value1, got %s", decoded.Args[0])
	}
}

func TestDeserializeCommandError(t *testing.T) {
	_, err := DeserializeCommand([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}

	_, err = DeserializeCommand([]byte("AB"))
	if err == nil {
		t.Error("expected error for short data")
	}

	data := []byte{255, 65, 66, 67}
	_, err = DeserializeCommand(data)
	if err == nil {
		t.Error("expected error for invalid op length")
	}

	data2 := []byte{3, 65, 66, 67, 3}
	_, err = DeserializeCommand(data2)
	if err == nil {
		t.Error("expected error for missing data after key")
	}
}

func TestParseWALRecord(t *testing.T) {
	cmd := &Command{
		Op:   "HSET",
		Key:  "hash_key",
		Args: [][]byte{[]byte("field1"), []byte("value1")},
	}
	data := cmd.Serialize()

	record := &WALRecord{
		Magic:      [3]byte{'W', 'A', 'L'},
		Timestamp:  time.Now().UnixNano(),
		DataLen:    uint32(len(data)),
		Compressed: false,
		Data:       data,
	}

	recordBytes, err := record.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	parsed, err := ParseWALRecord(recordBytes)
	if err != nil {
		t.Fatalf("ParseWALRecord failed: %v", err)
	}
	if parsed.Timestamp != record.Timestamp {
		t.Errorf("expected timestamp %d, got %d", record.Timestamp, parsed.Timestamp)
	}
	if parsed.Compressed != record.Compressed {
		t.Error("expected compressed to match")
	}
	if len(parsed.Data) != len(record.Data) {
		t.Errorf("expected data len %d, got %d", len(record.Data), len(parsed.Data))
	}

	decodedCmd, err := DeserializeCommand(parsed.Data)
	if err != nil {
		t.Fatalf("DeserializeCommand failed: %v", err)
	}
	if decodedCmd.Op != "HSET" {
		t.Errorf("expected HSET, got %s", decodedCmd.Op)
	}
}

func TestParseWALRecordCompressed(t *testing.T) {
	cmd := &Command{
		Op:   "MSET",
		Key:  "multi_key",
		Args: [][]byte{[]byte("k1"), []byte("v1"), []byte("k2"), []byte("v2")},
	}
	data := cmd.Serialize()

	record := &WALRecord{
		Magic:      [3]byte{'W', 'A', 'L'},
		Timestamp:  time.Now().UnixNano(),
		Compressed: true,
		Data:       data,
	}

	recordBytes, err := record.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	parsed, err := ParseWALRecord(recordBytes)
	if err != nil {
		t.Fatalf("ParseWALRecord failed: %v", err)
	}
	if !parsed.Compressed {
		t.Error("expected compressed to be true")
	}

	decodedCmd, err := DeserializeCommand(parsed.Data)
	if err != nil {
		t.Fatalf("DeserializeCommand failed: %v", err)
	}
	if decodedCmd.Op != "MSET" {
		t.Errorf("expected MSET, got %s", decodedCmd.Op)
	}
}

func TestParseWALRecordError(t *testing.T) {
	_, err := ParseWALRecord([]byte("short"))
	if err == nil {
		t.Error("expected error for short data")
	}

	invalidMagic := make([]byte, 100)
	copy(invalidMagic[0:3], []byte("XXX"))
	_, err = ParseWALRecord(invalidMagic)
	if err == nil {
		t.Error("expected error for invalid magic")
	}
}
