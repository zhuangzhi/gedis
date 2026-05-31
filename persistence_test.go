package gedis

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceManagerSaveLoad(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_save_load.rdb")
	walPath := filepath.Join(tmpDir, "test_save_load.wal")

	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()
	db.Set("key1", []byte("value1"))
	db.Set("key2", []byte("value2"))

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.loadRDB(rdbPath); err != nil {
		t.Fatalf("loadRDB failed: %v", err)
	}

	val, ok := db2.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if val.String() != "value1" {
		t.Errorf("expected value1, got %s", val.String())
	}
	val.Close()

	val, ok = db2.Get("key2")
	if !ok {
		t.Fatal("expected key2 to exist")
	}
	if val.String() != "value2" {
		t.Errorf("expected value2, got %s", val.String())
	}
	val.Close()
}

func TestPersistenceManagerWithWAL(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_wal.rdb")
	walPath := filepath.Join(tmpDir, "test_wal.wal")

	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled: true,
			Path:    walPath,
			Fsync:   FsyncNo,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	cmd := &Command{
		Op:   "SET",
		Key:  "testkey",
		Args: [][]byte{[]byte("testvalue")},
	}

	if err := pm.AppendCommand(cmd); err != nil {
		t.Fatalf("AppendCommand failed: %v", err)
	}

	val, ok := db.Get("testkey")
	if !ok {
		t.Fatal("expected testkey to exist")
	}
	if val.String() != "testvalue" {
		t.Errorf("expected testvalue, got %s", val.String())
	}
	val.Close()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		WAL: WALConfig{
			Enabled: true,
			Path:    walPath,
			Fsync:   FsyncNo,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	val, ok = db2.Get("testkey")
	if !ok {
		t.Fatal("expected testkey to exist after recovery")
	}
	if val.String() != "testvalue" {
		t.Errorf("expected testvalue after recovery, got %s", val.String())
	}
	val.Close()
}

func TestParseCommand(t *testing.T) {
	cmd, err := ParseCommand("SET key value")
	if err != nil {
		t.Fatalf("ParseCommand failed: %v", err)
	}
	if cmd.Op != "SET" {
		t.Errorf("expected SET, got %s", cmd.Op)
	}
	if string(cmd.Args[0]) != "key" {
		t.Errorf("expected key, got %s", string(cmd.Args[0]))
	}
	if string(cmd.Args[1]) != "value" {
		t.Errorf("expected value, got %s", string(cmd.Args[1]))
	}

	_, err = ParseCommand("")
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestPersistenceRecoverEmpty(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_empty.rdb")

	db := New()
	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	err = pm.Recover()
	if err != nil {
		t.Errorf("Recover should not fail for non-existent RDB: %v", err)
	}
}

func TestPersistenceInvalidRDB(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_invalid.rdb")

	f, err := os.Create(rdbPath)
	if err != nil {
		t.Fatalf("create file failed: %v", err)
	}
	f.WriteString("INVALID")
	f.Close()
	defer os.Remove(rdbPath)

	db := New()
	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	err = pm.Recover()
	if err == nil {
		t.Error("expected error for invalid RDB")
	}
}

func TestPersistenceManagerBackgroundSave(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_bg_save.rdb")

	defer os.Remove(rdbPath)

	db := New()
	db.Set("bgkey", []byte("bgvalue"))

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled:      true,
			Path:         rdbPath,
			SaveInterval: 100 * time.Millisecond,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	pm.Stop()

	if _, err := os.Stat(rdbPath); os.IsNotExist(err) {
		t.Error("expected RDB file to exist after background save")
	}
}

func TestPersistenceWriteCountTrigger(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_write_trigger.rdb")
	walPath := filepath.Join(tmpDir, "test_write_trigger.wal")

	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled: true,
			Path:    walPath,
			Fsync:   FsyncNo,
		},
		RDB: RDBConfig{
			Enabled:           true,
			Path:              rdbPath,
			WriteCountTrigger: 3,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	for i := 0; i < 5; i++ {
		cmd := &Command{
			Op:   "SET",
			Key:  "key" + string(rune('0'+i)),
			Args: [][]byte{[]byte("value" + string(rune('0'+i)))},
		}
		if err := pm.AppendCommand(cmd); err != nil {
			t.Fatalf("AppendCommand failed: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(rdbPath); os.IsNotExist(err) {
		t.Error("expected RDB file to exist after write count trigger")
	}
}

func TestPersistenceAllDataTypes(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_all_types.rdb")
	defer os.Remove(rdbPath)

	db := New()
	db.Set("string:key", []byte("string:value"))
	db.Set("int:key", []byte("123"))
	db.SetEx("expiring:key", 100, []byte("expires"))

	db.HSet("hash:key", "field1", []byte("value1"))
	db.HSet("hash:key", "field2", []byte("value2"))
	db.HSet("hash:key", "field3", []byte("123"))

	db.LPush("list:key", []byte("item1"), []byte("item2"), []byte("item3"))
	db.RPush("list:key2", []byte("a"), []byte("b"), []byte("c"))

	db.SAdd("set:key", []byte("member1"), []byte("member2"), []byte("member3"))

	db.ZAdd("zset:key", 1.0, []byte("member1"))
	db.ZAdd("zset:key", 2.0, []byte("member2"))
	db.ZAdd("zset:key", 3.0, []byte("member3"))

	fields := make(map[string]*PooledBuffer)
	fields["field1"] = BufFromBytes([]byte("value1"))
	fields["field2"] = BufFromBytes([]byte("123"))
	for _, v := range fields {
		v.Close()
	}
	db.XAdd("stream:key", "1-0", fields)

	db.TSAdd("timeseries:key", 1000, 10.5)
	db.TSAdd("timeseries:key", 2000, 20.5)

	db.SetBit("bitmap:key", 0, 1)
	db.SetBit("bitmap:key", 10, 1)
	db.SetBit("bitmap:key", 100, 1)

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.loadRDB(rdbPath); err != nil {
		t.Fatalf("loadRDB failed: %v", err)
	}

	val, ok := db2.Get("string:key")
	if !ok {
		t.Fatal("string key missing")
	}
	if val.String() != "string:value" {
		t.Errorf("string value mismatch: got %s", val.String())
	}
	val.Close()

	val, ok = db2.Get("int:key")
	if !ok {
		t.Fatal("int key missing")
	}
	if val.String() != "123" {
		t.Errorf("int value mismatch: got %s", val.String())
	}
	val.Close()

	hlen := db2.HLen("hash:key")
	if hlen != 3 {
		t.Errorf("hash len mismatch: expected 3, got %d", hlen)
	}

	llen := db2.LLen("list:key")
	if llen != 3 {
		t.Errorf("list len mismatch: expected 3, got %d", llen)
	}

	scard := db2.SCard("set:key")
	if scard != 3 {
		t.Errorf("set card mismatch: expected 3, got %d", scard)
	}

	zcard := db2.ZCard("zset:key")
	if zcard != 3 {
		t.Errorf("zset card mismatch: expected 3, got %d", zcard)
	}

	xlen := db2.XLen("stream:key")
	if xlen != 1 {
		t.Errorf("stream len mismatch: expected 1, got %d", xlen)
	}

	tslen := len(db2.TSRange("timeseries:key", 0, 9999999999999))
	if tslen != 2 {
		t.Errorf("timeseries len mismatch: expected 2, got %d", tslen)
	}

	bit := db2.GetBit("bitmap:key", 0)
	if bit != 1 {
		t.Errorf("bitmap bit 0 mismatch: expected 1, got %d", bit)
	}
}

func TestPersistenceSaveLoadCycle(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_save_cycle.rdb")
	defer os.Remove(rdbPath)

	for round := 0; round < 3; round++ {
		db := New()

		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("round%d:key%d", round, i)
			db.Set(key, []byte(fmt.Sprintf("value%d", i)))
		}

		cfg := PersistenceConfig{
			RDB: RDBConfig{
				Enabled: true,
				Path:    rdbPath,
			},
		}

		pm, err := NewPersistenceManager(db, cfg)
		if err != nil {
			t.Fatalf("round %d: NewPersistenceManager failed: %v", round, err)
		}

		if err := pm.Save(); err != nil {
			t.Fatalf("round %d: Save failed: %v", round, err)
		}
		pm.Stop()

		db2 := New()
		cfg2 := PersistenceConfig{
			RDB: RDBConfig{
				Enabled: true,
				Path:    rdbPath,
			},
		}
		pm2, err := NewPersistenceManager(db2, cfg2)
		if err != nil {
			t.Fatalf("round %d: NewPersistenceManager for db2 failed: %v", round, err)
		}

		if err := pm2.loadRDB(rdbPath); err != nil {
			t.Fatalf("round %d: loadRDB failed: %v", round, err)
		}

		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("round%d:key%d", round, i)
			val, ok := db2.Get(key)
			expected := fmt.Sprintf("value%d", i)
			if !ok {
				t.Fatalf("round %d: key %s missing", round, key)
			}
			if val.String() != expected {
				t.Errorf("round %d: key %s value mismatch: expected %s, got %s", round, key, expected, val.String())
			}
			val.Close()
		}
		pm2.Stop()
	}
}

func TestPersistenceLargeData(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_large_data.rdb")
	defer os.Remove(rdbPath)

	db := New()

	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}
	db.Set("large:string", largeValue)

	for i := 0; i < 1000; i++ {
		db.HSet("large:hash", fmt.Sprintf("field%d", i), []byte(fmt.Sprintf("value%d", i)))
	}

	for i := 0; i < 1000; i++ {
		db.ZAdd("large:zset", float64(i), []byte(fmt.Sprintf("member%d", i)))
	}

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.loadRDB(rdbPath); err != nil {
		t.Fatalf("loadRDB failed: %v", err)
	}

	val, ok := db2.Get("large:string")
	if !ok {
		t.Fatal("large string key missing")
	}
	if len(val.Bytes()) != len(largeValue) {
		t.Errorf("large string size mismatch: expected %d, got %d", len(largeValue), len(val.Bytes()))
	}
	val.Close()

	hlen := db2.HLen("large:hash")
	if hlen != 1000 {
		t.Errorf("large hash len mismatch: expected 1000, got %d", hlen)
	}

	zcard := db2.ZCard("large:zset")
	if zcard != 1000 {
		t.Errorf("large zset card mismatch: expected 1000, got %d", zcard)
	}
}

func TestPersistenceWALRecovery(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_wal_recovery.rdb")
	walPath := filepath.Join(tmpDir, "test_wal_recovery.wal")

	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()
	db.Set("before:wal", []byte("value1"))

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled:   true,
			Path:      walPath,
			Fsync:     FsyncNo,
			BatchSize: 10,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}

	pm.AppendCommand(&Command{
		Op:   "SET",
		Key:  "after:wal",
		Args: [][]byte{[]byte("value2")},
	})

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	pm.Stop()

	db2 := New()
	cfg2 := PersistenceConfig{
		WAL: WALConfig{
			Enabled:   true,
			Path:      walPath,
			Fsync:     FsyncNo,
			BatchSize: 10,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	val, ok := db2.Get("before:wal")
	if !ok {
		t.Fatal("before:wal key missing")
	}
	if val.String() != "value1" {
		t.Errorf("before:wal value mismatch: expected value1, got %s", val.String())
	}
	val.Close()

	val, ok = db2.Get("after:wal")
	if !ok {
		t.Fatal("after:wal key missing (WAL recovery failed)")
	}
	if val.String() != "value2" {
		t.Errorf("after:wal value mismatch: expected value2, got %s", val.String())
	}
	val.Close()
}

func TestPersistenceCrashRecovery(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_crash_recovery.rdb")
	walPath := filepath.Join(tmpDir, "test_crash_recovery.wal")

	defer os.Remove(rdbPath)
	defer os.Remove(walPath)

	db := New()
	for i := 0; i < 100; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled:   true,
			Path:      walPath,
			Fsync:     FsyncNo,
			BatchSize: 100,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}

	for i := 100; i < 150; i++ {
		db.Set(fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	pm.Stop()

	db2 := New()
	cfg2 := PersistenceConfig{
		WAL: WALConfig{
			Enabled:   true,
			Path:      walPath,
			Fsync:     FsyncNo,
			BatchSize: 100,
		},
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	for i := 0; i < 150; i++ {
		key := fmt.Sprintf("key%d", i)
		expected := fmt.Sprintf("value%d", i)
		val, ok := db2.Get(key)
		if !ok {
			t.Fatalf("crash recovery key %s missing", key)
		}
		if val.String() != expected {
			t.Errorf("crash recovery key %s value mismatch: expected %s, got %s", key, expected, val.String())
		}
		val.Close()
	}
}

func TestPersistenceCorruptedWAL(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_corrupted_wal.rdb")

	defer os.Remove(rdbPath)

	db := New()
	db.Set("key1", []byte("value1"))

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	val, ok := db2.Get("key1")
	if !ok {
		t.Fatal("key1 should exist from RDB even without WAL")
	}
	if val.String() != "value1" {
		t.Errorf("key1 value mismatch: expected value1, got %s", val.String())
	}
	val.Close()
}

func TestPersistenceNoWALOnlyRDB(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_no_wal.rdb")
	defer os.Remove(rdbPath)

	db := New()
	db.Set("key1", []byte("value1"))
	db.Set("key2", []byte("value2"))
	db.Set("key3", []byte("value3"))

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("key%d", i)
		expected := fmt.Sprintf("value%d", i)
		val, ok := db2.Get(key)
		if !ok {
			t.Fatalf("key%d missing after recovery", i)
		}
		if val.String() != expected {
			t.Errorf("key%d value mismatch: expected %s, got %s", i, expected, val.String())
		}
		val.Close()
	}
}

func TestPersistenceEmptyDatabase(t *testing.T) {
	tmpDir := os.TempDir()
	rdbPath := filepath.Join(tmpDir, "test_empty_db.rdb")
	defer os.Remove(rdbPath)

	db := New()

	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	db2 := New()
	cfg2 := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    rdbPath,
		},
	}
	pm2, err := NewPersistenceManager(db2, cfg2)
	if err != nil {
		t.Fatalf("NewPersistenceManager for db2 failed: %v", err)
	}
	defer pm2.Stop()

	if err := pm2.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if db2.dict.Len() != 0 {
		t.Errorf("expected empty database, got %d keys", db2.dict.Len())
	}
}
