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

// 全文搜索（Full-Text Search）实现，基于倒排索引。
// 支持索引创建、文档添加和关键词搜索。
package gedis

import "strings"

func (db *RedisDB) FTCreate(index string, schema map[string]string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.ensureSearchIndex(index, schema)
}

func (db *RedisDB) FTAdd(index string, docID string, fields map[string]string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	idx := db.getSearchIndex(index)
	if idx == nil {
		return
	}

	docIdx := strings.ToLower(index + ":doc:" + docID)
	docHeadOff, ok := db.dict.Get([]byte(docIdx))
	if ok {
		oldDataOff := db.ObjectDataOffset(docHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
	}

	docData := serializeFields(fields)
	docOff := db.arena.AllocBytes(docData)
	if ok {
		db.ObjectSetDataOffset(docHeadOff, docOff)
	} else {
		docHeadOff = db.NewObject(ObjString, ObjEncodingRaw, docOff)
		db.dict.Set([]byte(docIdx), docHeadOff)
	}

	for field, value := range fields {
		terms := tokenize(value)
		for _, term := range terms {
			termKey := strings.ToLower(index + ":inv:" + field + ":" + term)

			termHeadOff, termExists := db.dict.Get([]byte(termKey))
			var postingList []string

			if termExists {
				postingList = db.readPostingList(termHeadOff)
			}

			found := false
			for _, id := range postingList {
				if id == docID {
					found = true
					break
				}
			}
			if !found {
				postingList = append(postingList, docID)
				db.storePostingList(termKey, termHeadOff, termExists, postingList)
			}
		}
	}
}

// FTSearch 在全文索引中搜索关键词并返回匹配的文档 ID。
// 对应 Redis: FT.SEARCH index query
// 优化：返回 *ZSlices 替代 []string，数据在 Arena 中紧凑存储。
// 调用方用 zs.Len()/zs.Get(i) 遍历后用 zs.Close() 归还底层缓冲区。
func (db *RedisDB) FTSearch(index string, query string, limit int) *ZSlices {
	db.mu.RLock()
	defer db.mu.RUnlock()

	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	schema := db.getSearchIndex(index)

	var result []string
	first := true

	for _, term := range terms {
		var matchingDocs []string
		seen := make(map[string]bool)

		for field := range schema {
			termKey := strings.ToLower(index + ":inv:" + field + ":" + term)
			headOff, ok := db.dict.Get([]byte(termKey))
			if ok {
				docs := db.readPostingList(headOff)
				for _, d := range docs {
					if !seen[d] {
						seen[d] = true
						matchingDocs = append(matchingDocs, d)
					}
				}
			}
		}

		if len(matchingDocs) == 0 {
			matchingDocs = db.scanForTerm(index, term)
		}

		if first {
			result = matchingDocs
			first = false
		} else {
			result = intersectStringSlices(result, matchingDocs)
		}
	}

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	zs := NewZSlices()
	for _, r := range result {
		zs.AddString(r)
	}
	zs.Finish()
	return zs
}

func (db *RedisDB) ensureSearchIndex(index string, schema map[string]string) {
	idxKey := strings.ToLower(index + ":idx:schema")
	_, ok := db.dict.Get([]byte(idxKey))
	if !ok {
		schemaData := serializeFields(schema)
		schemaOff := db.arena.AllocBytes(schemaData)
		headOff := db.NewObject(ObjString, ObjEncodingRaw, schemaOff)
		db.dict.Set([]byte(idxKey), headOff)
	}
}

func (db *RedisDB) getSearchIndex(index string) map[string]string {
	idxKey := strings.ToLower(index + ":idx:schema")
	headOff, ok := db.dict.Get([]byte(idxKey))
	if !ok {
		return nil
	}
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil
	}
	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)
	return deserializeFields(data)
}

func (db *RedisDB) readPostingList(headOff int) []string {
	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil
	}
	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), ",")
}

func (db *RedisDB) storePostingList(key string, headOff int, exists bool, docs []string) {
	data := strings.Join(docs, ",")
	dataOff := db.arena.AllocBytes([]byte(data))
	if exists {
		oldDataOff := db.ObjectDataOffset(headOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
		db.ObjectSetDataOffset(headOff, dataOff)
	} else {
		headOff = db.NewObject(ObjSearch, ObjEncodingRaw, dataOff)
		db.dict.Set([]byte(key), headOff)
	}
}

func (db *RedisDB) scanForTerm(index, term string) []string {
	var result []string
	return result
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '!' || r == '?' || r == ';' || r == ':' || r == '\n' || r == '\t'
	})

	seen := make(map[string]bool)
	var result []string
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			result = append(result, w)
		}
	}
	return result
}

func serializeFields(fields map[string]string) []byte {
	parts := make([]string, 0, len(fields)*2)
	for k, v := range fields {
		parts = append(parts, k, v)
	}
	return []byte(strings.Join(parts, "\x00"))
}

func deserializeFields(data []byte) map[string]string {
	parts := strings.Split(string(data), "\x00")
	result := make(map[string]string, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		result[parts[i]] = parts[i+1]
	}
	return result
}

func intersectStringSlices(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}

	var result []string
	for _, s := range a {
		if set[s] {
			result = append(result, s)
		}
	}
	return result
}
