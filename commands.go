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
	"encoding/base64"
	"encoding/binary"
)

// Dump 将 key 的值序列化
func (db *RedisDB) Dump(key string) ([]byte, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil, false
	}

	if db.isExpired(keyBytes, headOff) {
		return nil, false
	}

	var buf bytes.Buffer
	
	buf.WriteByte(0x00)
	
	objType := db.ObjectType(headOff)
	buf.WriteByte(uint8(objType))
	
	encoding := db.ObjectEncoding(headOff)
	buf.WriteByte(uint8(encoding))
	
	dataOff := db.ObjectDataOffset(headOff)
	dataSize := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, dataSize)
	
	err := binary.Write(&buf, binary.LittleEndian, uint64(dataSize))
	if err != nil {
		return nil, false
	}
	
	buf.Write(data)
	
	expiry, hasExpiry := db.getExpiryLocked(keyBytes)
	if hasExpiry {
		buf.WriteByte(0x01)
		err = binary.Write(&buf, binary.LittleEndian, uint64(expiry))
		if err != nil {
			return nil, false
		}
	} else {
		buf.WriteByte(0x00)
	}

	return buf.Bytes(), true
}

// Restore 将序列化的值恢复到 key
// ttl: 过期时间（毫秒），使用 DB_RESTORE_ABSTTL 标志时为绝对时间戳
func (db *RedisDB) Restore(key string, ttl int64, value []byte, replace bool) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	
	if !replace {
		_, exists := db.dict.Get(keyBytes)
		if exists {
			return false
		}
	}

	if len(value) < 1 {
		return false
	}

	reader := bytes.NewReader(value)
	
	magic, err := reader.ReadByte()
	if err != nil || magic != 0x00 {
		return false
	}

	objTypeByte, err := reader.ReadByte()
	if err != nil {
		return false
	}
	objType := objTypeByte

	encodingByte, err := reader.ReadByte()
	if err != nil {
		return false
	}
	encoding := encodingByte

	var dataSize uint64
	err = binary.Read(reader, binary.LittleEndian, &dataSize)
	if err != nil {
		return false
	}

	data := make([]byte, dataSize)
	_, err = reader.Read(data)
	if err != nil {
		return false
	}

	hasExpiry, err := reader.ReadByte()
	if err != nil {
		return false
	}

	var expiry int64
	if hasExpiry == 0x01 {
		var expTime uint64
		err = binary.Read(reader, binary.LittleEndian, &expTime)
		if err != nil {
			return false
		}
		expiry = int64(expTime)
	}

	dataOff := db.arena.AllocBytes(data)
	headOff := db.NewObject(objType, encoding, dataOff)
	db.dict.Set(keyBytes, headOff)

	if ttl > 0 {
		expTime := currentTimeMs() + ttl
		expTimeOff := db.arena.Alloc(8)
		db.arena.WriteUint64(expTimeOff, uint64(expTime))
		db.expiry.Set(keyBytes, expTimeOff)
	} else if ttl == -1 && hasExpiry == 0x01 {
		expTimeOff := db.arena.Alloc(8)
		db.arena.WriteUint64(expTimeOff, uint64(expiry))
		db.expiry.Set(keyBytes, expTimeOff)
	}

	return true
}

// Rename 将 key 重命名为 newkey
func (db *RedisDB) Rename(key, newkey string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.renameLocked(key, newkey, true)
}

// RenameNx 仅当 newkey 不存在时，将 key 重命名为 newkey
func (db *RedisDB) RenameNx(key, newkey string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.renameLocked(key, newkey, false)
}

// renameLocked 内部重命名实现
func (db *RedisDB) renameLocked(key, newkey string, replace bool) bool {
	keyBytes := []byte(key)
	newkeyBytes := []byte(newkey)

	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return false
	}

	if db.isExpired(keyBytes, headOff) {
		return false
	}

	if !replace {
		_, exists := db.dict.Get(newkeyBytes)
		if exists {
			return false
		}
	}

	db.dict.Set(newkeyBytes, headOff)
	
	expiryOff, hasExpiry := db.expiry.Get(keyBytes)
	if hasExpiry {
		db.expiry.Set(newkeyBytes, expiryOff)
		db.expiry.Del(keyBytes)
	}

	db.dict.Del(keyBytes)

	db.incrementKeyVersion(key)
	db.incrementKeyVersion(newkey)

	return true
}

// DumpBase64 将 key 的值序列化为 Base64 字符串
func (db *RedisDB) DumpBase64(key string) (string, bool) {
	data, ok := db.Dump(key)
	if !ok {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(data), true
}

// RestoreBase64 从 Base64 字符串恢复值
func (db *RedisDB) RestoreBase64(key string, ttl int64, value string, replace bool) bool {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	return db.Restore(key, ttl, data, replace)
}

// getExpiryLocked 获取 key 的过期时间（内部使用）
func (db *RedisDB) getExpiryLocked(key []byte) (int64, bool) {
	expOff, ok := db.expiry.Get(key)
	if !ok {
		return 0, false
	}
	expTime := db.arena.ReadUint64(expOff)
	return int64(expTime), true
}


