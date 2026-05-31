package gedis

import (
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
