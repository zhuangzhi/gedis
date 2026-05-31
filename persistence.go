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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	rdbMagic       = "GEDIS"
	rdbVersion     = 1
	rdbHeaderSize  = 5 + 4 + 8 + 8 + 4 + 4 + 4 + 4 + 4 // +4 for DictTableOff, +4 for DictSize
)

type RDBHeader struct {
	Magic         [5]byte
	Version       uint32
	CreateTime    int64
	LastWALOffset int64
	ArenaSize     uint32
	DictCount     uint32
	DictTableOff  uint32
	DictSize      uint32
	CRC32         uint32
}

type PersistenceConfig struct {
	WAL WALConfig
	RDB RDBConfig
}

type RDBConfig struct {
	Enabled         bool
	Path            string
	SaveInterval    time.Duration
	SaveOnShutdown  bool
	MaxBackups      int
	WriteCountTrigger int
}

type PersistenceManager struct {
	db          *RedisDB
	wal         *WALWriter
	config      PersistenceConfig
	mu          sync.RWMutex
	writeCount  int64
	saveTimer   *time.Timer
	stopChan    chan struct{}
	doneChan    chan struct{}
}

func NewPersistenceManager(db *RedisDB, config PersistenceConfig) (*PersistenceManager, error) {
	pm := &PersistenceManager{
		db:       db,
		config:   config,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}

	if config.WAL.Enabled {
		wal, err := NewWALWriter(config.WAL)
		if err != nil {
			return nil, fmt.Errorf("create WAL writer: %w", err)
		}
		pm.wal = wal
	}

	if config.RDB.Enabled && config.RDB.SaveInterval > 0 {
		pm.saveTimer = time.NewTimer(config.RDB.SaveInterval)
		go pm.backgroundSaveLoop()
	} else {
		close(pm.doneChan)
	}

	return pm, nil
}

func (pm *PersistenceManager) backgroundSaveLoop() {
	defer close(pm.doneChan)

	for {
		select {
		case <-pm.saveTimer.C:
			pm.TriggerSave()
			pm.saveTimer.Reset(pm.config.RDB.SaveInterval)
		case <-pm.stopChan:
			return
		}
	}
}

func (pm *PersistenceManager) Stop() {
	close(pm.stopChan)
	if pm.saveTimer != nil {
		pm.saveTimer.Stop()
	}
	<-pm.doneChan
	if pm.wal != nil {
		pm.wal.Close()
	}
}

func (pm *PersistenceManager) AppendCommand(cmd *Command) error {
	if pm.wal == nil {
		return pm.db.ExecuteCommand(cmd)
	}

	if err := pm.wal.Append(cmd); err != nil {
		return fmt.Errorf("wal append: %w", err)
	}

	if err := pm.db.ExecuteCommand(cmd); err != nil {
		return fmt.Errorf("execute command: %w", err)
	}

	pm.writeCount++
	if pm.config.RDB.WriteCountTrigger > 0 && pm.writeCount >= int64(pm.config.RDB.WriteCountTrigger) {
		pm.TriggerSave()
		pm.writeCount = 0
	}

	return nil
}

func (pm *PersistenceManager) WALOffset() int64 {
	if pm.wal == nil {
		return 0
	}
	return pm.wal.Offset()
}

func (pm *PersistenceManager) Save() error {
	return pm.saveToFile(pm.config.RDB.Path)
}

func (pm *PersistenceManager) TriggerSave() {
	go func() {
		if err := pm.Save(); err != nil {
			fmt.Printf("snapshot save failed: %v\n", err)
		}
	}()
}

func (pm *PersistenceManager) saveToFile(path string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.wal != nil {
		if err := pm.wal.Flush(true); err != nil {
			return fmt.Errorf("flush WAL: %w", err)
		}
	}

	walOffset := int64(0)
	if pm.wal != nil {
		walOffset = pm.wal.Offset()
	}

	pm.db.mu.RLock()
	defer pm.db.mu.RUnlock()

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	var headerBuf [rdbHeaderSize]byte
	copy(headerBuf[0:5], rdbMagic)
	binary.BigEndian.PutUint32(headerBuf[5:9], rdbVersion)
	binary.BigEndian.PutUint64(headerBuf[9:17], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(headerBuf[17:25], uint64(walOffset))
	binary.BigEndian.PutUint32(headerBuf[25:29], uint32(pm.db.arena.Size()))
	binary.BigEndian.PutUint32(headerBuf[29:33], uint32(pm.db.dict.Len()))
	binary.BigEndian.PutUint32(headerBuf[33:37], uint32(pm.db.dict.table))
	binary.BigEndian.PutUint32(headerBuf[37:41], uint32(pm.db.dict.size))

	crc := crc32.ChecksumIEEE(headerBuf[:41])
	binary.BigEndian.PutUint32(headerBuf[41:45], crc)

	if _, err := file.Write(headerBuf[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	arenaData := pm.db.arena.Dump()
	if _, err := file.Write(arenaData); err != nil {
		return fmt.Errorf("write arena: %w", err)
	}

	if err := pm.snapshotWALSwitch(); err != nil {
		return fmt.Errorf("WAL switch: %w", err)
	}

	return nil
}

func (pm *PersistenceManager) snapshotWALSwitch() error {
	if pm.wal == nil {
		return nil
	}

	newPath := pm.config.WAL.Path + fmt.Sprintf(".new.%d", time.Now().UnixNano())
	if err := pm.wal.SwitchTo(newPath); err != nil {
		return err
	}

	return nil
}

func (pm *PersistenceManager) Recover() error {
	rdbPath := pm.config.RDB.Path
	walPath := pm.config.WAL.Path

	if _, err := os.Stat(rdbPath); os.IsNotExist(err) {
		return nil
	}

	if err := pm.loadRDB(rdbPath); err != nil {
		return fmt.Errorf("load RDB: %w", err)
	}

	if pm.wal == nil {
		return nil
	}

	walOffset := pm.db.lastWALOffset

	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		return nil
	}

	reader, err := NewWALReader(walPath)
	if err != nil {
		return fmt.Errorf("open WAL reader: %w", err)
	}
	defer reader.Close()

	if err := reader.Replay(pm.db, walOffset); err != nil {
		return fmt.Errorf("replay WAL: %w", err)
	}

	return nil
}

func (pm *PersistenceManager) loadRDB(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var headerBuf [rdbHeaderSize]byte
	if _, err := file.Read(headerBuf[:]); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	magic := string(headerBuf[0:5])
	if magic != rdbMagic {
		return fmt.Errorf("invalid magic: %s", magic)
	}

	version := binary.BigEndian.Uint32(headerBuf[5:9])
	if version != rdbVersion {
		return fmt.Errorf("unsupported RDB version: %d", version)
	}

	createTime := int64(binary.BigEndian.Uint64(headerBuf[9:17]))
	_ = createTime

	walOffset := int64(binary.BigEndian.Uint64(headerBuf[17:25]))
	arenaSize := binary.BigEndian.Uint32(headerBuf[25:29])
	dictCount := binary.BigEndian.Uint32(headerBuf[29:33])
	dictTableOff := binary.BigEndian.Uint32(headerBuf[33:37])
	dictSize := binary.BigEndian.Uint32(headerBuf[37:41])
	storedCRC := binary.BigEndian.Uint32(headerBuf[41:45])

	computedCRC := crc32.ChecksumIEEE(headerBuf[:41])
	if storedCRC != computedCRC {
		return fmt.Errorf("header CRC mismatch: expected %x, got %x", storedCRC, computedCRC)
	}

	arenaData := make([]byte, arenaSize)
	if _, err := file.Read(arenaData); err != nil {
		return fmt.Errorf("read arena data: %w", err)
	}

	pm.db.mu.Lock()
	defer pm.db.mu.Unlock()

	pm.db.arena = LoadArena(arenaData)
	pm.db.dict = &Dict{
		arena: pm.db.arena,
		table: int(dictTableOff),
		size:  int(dictSize),
		used:  int(dictCount),
	}
	pm.db.expiry = NewDict(pm.db.arena)
	pm.db.lastWALOffset = walOffset

	return nil
}

func findLatestRDB(dir, prefix string) (string, error) {
	pattern := filepath.Join(dir, prefix+"*.rdb")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	type fileInfo struct {
		path    string
	modTime  time.Time
	}

	files := make([]fileInfo, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: path, modTime: info.ModTime()})
	}

	if len(files) == 0 {
		return "", nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	return files[0].path, nil
}

func cleanupOldFiles(dir, prefix, ext string, keep int) error {
	pattern := filepath.Join(dir, prefix+"*"+ext)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}

	files := make([]fileInfo, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: path, modTime: info.ModTime()})
	}

	if len(files) <= keep {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, f := range files[keep:] {
		os.Remove(f.path)
	}

	return nil
}

type PersistenceConfigJSON struct {
	 WAL struct {
		Enabled      bool   `json:"enabled"`
		Path         string `json:"path"`
		Fsync        string `json:"fsync"`
		BatchSize    int    `json:"batch_size"`
		BatchTimeout string `json:"batch_timeout"`
		Compression  bool   `json:"compression"`
	} `json:"wal"`
	RDB struct {
		Enabled          bool   `json:"enabled"`
		Path             string `json:"path"`
		SaveInterval     string `json:"save_interval"`
		SaveOnShutdown   bool   `json:"save_on_shutdown"`
		MaxBackups       int    `json:"max_backups"`
		WriteCountTrigger int   `json:"write_count_trigger"`
	} `json:"rdb"`
}

func ParsePersistenceConfig(jsonStr string) (PersistenceConfig, error) {
	var cfgJSON PersistenceConfigJSON
	if err := json.Unmarshal([]byte(jsonStr), &cfgJSON); err != nil {
		return PersistenceConfig{}, err
	}

	cfg := PersistenceConfig{
		WAL: WALConfig{
			Enabled:      cfgJSON.WAL.Enabled,
			Path:         cfgJSON.WAL.Path,
			Fsync:        FsyncPolicy(cfgJSON.WAL.Fsync),
			BatchSize:    cfgJSON.WAL.BatchSize,
			Compression:  cfgJSON.WAL.Compression,
		},
		RDB: RDBConfig{
			Enabled:           cfgJSON.RDB.Enabled,
			Path:              cfgJSON.RDB.Path,
			SaveOnShutdown:    cfgJSON.RDB.SaveOnShutdown,
			MaxBackups:        cfgJSON.RDB.MaxBackups,
			WriteCountTrigger: cfgJSON.RDB.WriteCountTrigger,
		},
	}

	if cfgJSON.WAL.BatchTimeout != "" {
		dur, err := time.ParseDuration(cfgJSON.WAL.BatchTimeout)
		if err == nil {
			cfg.WAL.BatchTimeout = dur
		}
	}

	if cfgJSON.RDB.SaveInterval != "" {
		dur, err := time.ParseDuration(cfgJSON.RDB.SaveInterval)
		if err == nil {
			cfg.RDB.SaveInterval = dur
		}
	}

	return cfg, nil
}

func IsWriteCommand(op string) bool {
	writeCommands := map[string]bool{
		"SET": true, "SETNX": true, "SETEX": true, "PSETEX": true,
		"APPEND": true, "SETRANGE": true,
		"INCR": true, "INCRBY": true, "INCRBYFLOAT": true, "DECR": true, "DECRBY": true,
		"DEL": true, "UNLINK": true,
		"HSET": true, "HSETNX": true, "HMSET": true, "HDEL": true,
		"HINCRBY": true, "HINCRBYFLOAT": true,
		"LPUSH": true, "RPUSH": true, "LPUSHX": true, "RPUSHX": true,
		"LPOP": true, "RPOP": true, "LREM": true, "LTRIM": true, "LINSERT": true,
		"LMOVE": true, "BLMOVE": true,
		"SADD": true, "SREM": true, "SPOP": true, "SMOVE": true,
		"SINTER": true, "SUNION": true, "SDIFF": true,
		"SINTERSTORE": true, "SUNIONSTORE": true, "SDIFFSTORE": true,
		"ZADD": true, "ZREM": true, "ZINCRBY": true,
		"ZREMRANGEBYRANK": true, "ZREMRANGEBYSCORE": true, "ZREMRANGEBYLEX": true,
		"ZUNIONSTORE": true, "ZINTERSTORE": true,
		"XADD": true, "XTRIM": true, "XDEL": true,
		"XCLAIM": true, "XAUTOCLAIM": true,
		"BZMPOP": true, "BZPOPMIN": true, "BZPOPMAX": true,
		"BITOP": true, "BITCOUNT": true, "BITPOS": true, "SETBIT": true,
		"JSONSET": true, "JSONDEL": true,
		"BFADD": true, "BFINSERT": true, "BFDEL": true,
		"CFADD": true, "CFINSERT": true, "CFDEL": true,
		"CMSINCRBY": true, "CMSADD": true,
		"TOPKADD": true, "TOPKINCBY": true,
		"TSADD": true, "TSINCRBY": true,
		"FTADD": true, "FTDEL": true,
		"GADD": true, "GSET": true,
		"CELLSET": true, "CELLDEL": true,
	}

	return writeCommands[op]
}

func ParseCommand(line string) (*Command, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	op := strings.ToUpper(parts[0])
	args := make([][]byte, 0, len(parts)-1)
	for _, part := range parts[1:] {
		args = append(args, []byte(part))
	}

	return &Command{
		Op:   op,
		Key:  "",
		Args: args,
	}, nil
}
