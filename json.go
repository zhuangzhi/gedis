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

// JSON 文档操作实现，支持路径读写和 JSONPath 风格的字段访问。
package gedis

import (
	"encoding/json"
	"strconv"
	"strings"
)

// JsonSet 在 JSON 文档中设置指定路径的值。
func (db *RedisDB) JsonSet(key string, path string, value interface{}) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)

	if path == "." || path == "$" {
		path = ""
	}

	var doc map[string]interface{}

	headOff, ok := db.dict.Get(keyBytes)
	if ok {
		enc := db.ObjectEncoding(headOff)
		dataOff := db.ObjectDataOffset(headOff)
		if enc == ObjEncodingRaw && dataOff != 0 {
			size := db.arena.SizeAt(dataOff)
			data := db.arena.ReadBytes(dataOff, size)
			if err := json.Unmarshal(data, &doc); err != nil {
				return err
			}
			db.arena.Free(dataOff)
		}
	} else {
		doc = make(map[string]interface{})
	}

	if path == "" {
		if m, ok := value.(map[string]interface{}); ok {
			doc = m
		} else {
			doc = map[string]interface{}{"_": value}
		}
	} else {
		parts := parseJSONPath(path)
		jsonSetPath(doc, parts, value)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	newOff := db.arena.AllocBytes(data)

	if ok {
		db.ObjectSetDataOffset(headOff, newOff)
	} else {
		headOff = db.NewObject(ObjJSON, ObjEncodingRaw, newOff)
		db.dict.Set(keyBytes, headOff)
	}

	return nil
}

// JsonGet 获取 JSON 文档中指定路径的值。
func (db *RedisDB) JsonGet(key string, path string) (interface{}, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	headOff, ok := db.dict.Get([]byte(key))
	if !ok {
		return nil, nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)
	if enc != ObjEncodingRaw || dataOff == 0 {
		return nil, nil
	}

	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)

	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if path == "" || path == "." || path == "$" {
		return doc, nil
	}

	parts := parseJSONPath(path)
	return jsonGetPath(doc, parts), nil
}

// JsonDel 删除 JSON 文档中指定路径的值。
func (db *RedisDB) JsonDel(key string, path string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)
	if enc != ObjEncodingRaw || dataOff == 0 {
		return nil
	}

	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}

	if path == "" || path == "." || path == "$" {
		db.arena.Free(dataOff)
		db.dict.Del(keyBytes)
		db.FreeObject(headOff)
		return nil
	}

	parts := parseJSONPath(path)
	jsonDelPath(doc, parts)

	newData, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	db.arena.Free(dataOff)
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)
	return nil
}

func (db *RedisDB) JsonArrAppend(key string, path string, values ...interface{}) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)
	if enc != ObjEncodingRaw || dataOff == 0 {
		return nil
	}

	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)

	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}

	parts := parseJSONPath(path)
	arr := jsonGetPath(doc, parts)
	if arrSlice, ok := arr.([]interface{}); ok {
		arrSlice = append(arrSlice, values...)
		jsonSetPathRaw(doc, parts, arrSlice)
	}

	newData, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	db.arena.Free(dataOff)
	newOff := db.arena.AllocBytes(newData)
	db.ObjectSetDataOffset(headOff, newOff)
	return nil
}

func (db *RedisDB) JsonObjLen(key string, path string) (int, error) {
	val, err := db.JsonGet(key, path)
	if err != nil {
		return 0, err
	}
	if m, ok := val.(map[string]interface{}); ok {
		return len(m), nil
	}
	return 0, nil
}

func parseJSONPath(path string) []string {
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")

	parts := make([]string, 0)
	current := ""
	inBracket := false

	for _, ch := range path {
		if ch == '[' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
			inBracket = true
		} else if ch == ']' {
			current = strings.Trim(current, "'\"")
			parts = append(parts, current)
			current = ""
			inBracket = false
		} else if ch == '.' && !inBracket {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

func jsonSetPath(doc map[string]interface{}, parts []string, value interface{}) {
	if len(parts) == 0 {
		return
	}

	if len(parts) == 1 {
		doc[parts[0]] = value
		return
	}

	key := parts[0]
	if existing, ok := doc[key]; ok {
		if m, ok := existing.(map[string]interface{}); ok {
			jsonSetPath(m, parts[1:], value)
			return
		}
	}

	newMap := make(map[string]interface{})
	doc[key] = newMap
	jsonSetPath(newMap, parts[1:], value)
}

func jsonSetPathRaw(root interface{}, parts []string, value interface{}) {
	if len(parts) == 0 {
		return
	}

	if m, ok := root.(map[string]interface{}); ok {
		if len(parts) == 1 {
			m[parts[0]] = value
		} else if existing, ok := m[parts[0]]; ok {
			if nm, ok := existing.(map[string]interface{}); ok {
				jsonSetPathRaw(nm, parts[1:], value)
			}
		}
	}
}

func jsonGetPath(root interface{}, parts []string) interface{} {
	if len(parts) == 0 {
		return root
	}

	if m, ok := root.(map[string]interface{}); ok {
		if val, ok := m[parts[0]]; ok {
			if len(parts) == 1 {
				return val
			}
			return jsonGetPath(val, parts[1:])
		}
	}

	if arr, ok := root.([]interface{}); ok {
		idx, err := strconv.Atoi(parts[0])
		if err != nil || idx < 0 || idx >= len(arr) {
			return nil
		}
		if len(parts) == 1 {
			return arr[idx]
		}
		return jsonGetPath(arr[idx], parts[1:])
	}

	return nil
}

func jsonDelPath(doc map[string]interface{}, parts []string) {
	if len(parts) == 0 {
		return
	}

	if len(parts) == 1 {
		delete(doc, parts[0])
		return
	}

	key := parts[0]
	if val, ok := doc[key]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			jsonDelPath(m, parts[1:])
			if len(m) == 0 {
				delete(doc, key)
			}
		}
	}
}

var _ = json.Marshal
