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

// 图数据库（Graph）实现，支持节点、边的增删改查以及 Cypher 风格查询。
package gedis

import (
	"fmt"
	"strings"
)

// GraphNode 图节点，包含 ID、标签和属性。
type GraphNode struct {
	ID         string
	Labels     []string
	Properties map[string]string
}

type GraphEdge struct {
	ID         string
	Type       string
	Source     string
	Target     string
	Properties map[string]string
}

type GraphResult struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

func (db *RedisDB) GraphAddNode(graphName, nodeID string, labels []string, props map[string]string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.graphAddNodeLocked(graphName, nodeID, labels, props)
}

func (db *RedisDB) graphAddNodeLocked(graphName, nodeID string, labels []string, props map[string]string) {
	nodeKey := graphName + ":node:" + nodeID

	nodeData := serializeGraphEntity(labels, props)
	dataOff := db.arena.AllocBytes([]byte(nodeData))

	existingHeadOff, exists := db.dict.Get([]byte(nodeKey))
	var headOff int
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
		headOff = existingHeadOff
		db.ObjectSetDataOffset(headOff, dataOff)
	} else {
		headOff = db.NewObject(ObjGraph, ObjEncodingRaw, dataOff)
		db.dict.Set([]byte(nodeKey), headOff)
	}

	for _, label := range labels {
		labelIdxKey := graphName + ":label:" + label
		labelKeyBytes := []byte(labelIdxKey)

		existingOff, exists := db.dict.Get(labelKeyBytes)
		if exists {
			enc := db.ObjectEncoding(existingOff)
			dataOff := db.ObjectDataOffset(existingOff)

			if enc == ObjEncodingIntset && dataOff != 0 {
				isOff := int(dataOff)
				if isOff != 0 {
					db.arena.Free(isOff)
				}
				db.dict.Del(labelKeyBytes)
				innerDict := NewDict(db.arena)
				innerDict.Set([]byte(nodeID), 0)
				dictMetaOff := db.arena.Alloc(12)
				innerDict.StoreMeta(dictMetaOff)
				labelHeadOff := db.NewObject(ObjSet, ObjEncodingHashtable, dictMetaOff)
				db.dict.Set(labelKeyBytes, labelHeadOff)
			} else if enc == ObjEncodingHashtable && dataOff != 0 {
				innerDict := LoadDictMeta(db.arena, dataOff)
				if innerDict != nil {
					innerDict.Set([]byte(nodeID), 0)
				}
			}
		} else {
			innerDict := NewDict(db.arena)
			innerDict.Set([]byte(nodeID), 0)
			dictMetaOff := db.arena.Alloc(12)
			innerDict.StoreMeta(dictMetaOff)
			labelHeadOff := db.NewObject(ObjSet, ObjEncodingHashtable, dictMetaOff)
			db.dict.Set(labelKeyBytes, labelHeadOff)
		}
	}
}

func (db *RedisDB) GraphAddEdge(graphName, edgeID, edgeType, srcNodeID, dstNodeID string, props map[string]string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.graphAddEdgeLocked(graphName, edgeID, edgeType, srcNodeID, dstNodeID, props)
}

func (db *RedisDB) graphAddEdgeLocked(graphName, edgeID, edgeType, srcNodeID, dstNodeID string, props map[string]string) {
	edgeKey := graphName + ":edge:" + edgeID
	outKey := graphName + ":out:" + srcNodeID + ":" + edgeType + ":" + edgeID
	inKey := graphName + ":in:" + dstNodeID + ":" + edgeType + ":" + edgeID

	edgeData := append([]byte(edgeType+"|"+srcNodeID+"|"+dstNodeID+"|"), serializeGraphEntity(nil, props)...)
	dataOff := db.arena.AllocBytes(edgeData)

	existingHeadOff, exists := db.dict.Get([]byte(edgeKey))
	var headOff int
	if exists {
		oldDataOff := db.ObjectDataOffset(existingHeadOff)
		if oldDataOff != 0 {
			db.arena.Free(oldDataOff)
		}
		headOff = existingHeadOff
		db.ObjectSetDataOffset(headOff, dataOff)
	} else {
		headOff = db.NewObject(ObjGraph, ObjEncodingRaw, dataOff)
		db.dict.Set([]byte(edgeKey), headOff)
	}

	db.dict.Set([]byte(outKey), headOff)
	db.dict.Set([]byte(inKey), headOff)
}

func (db *RedisDB) GraphGetNode(graphName, nodeID string) *GraphNode {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.getGraphNode(graphName, nodeID)
}

func (db *RedisDB) GraphGetEdge(graphName, edgeID string) *GraphEdge {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.buildGraphEdge(graphName, edgeID)
}

func (db *RedisDB) GraphDeleteNode(graphName, nodeID string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	nodeKey := graphName + ":node:" + nodeID
	headOff, ok := db.dict.Get([]byte(nodeKey))
	if !ok {
		return false
	}

	edges := db.getAllEdgeIDs(graphName)
	for _, edgeID := range edges {
		edgeInfo := db.getGraphEdge(graphName, edgeID)
		if edgeInfo != nil && (edgeInfo[1] == nodeID || edgeInfo[2] == nodeID) {
			edgeKey := graphName + ":edge:" + edgeID
			db.dict.Del([]byte(edgeKey))
		}
	}

	db.dict.Del([]byte(nodeKey))
	_ = headOff
	return true
}

func (db *RedisDB) GraphListNodes(graphName string) []GraphNode {
	db.mu.RLock()
	defer db.mu.RUnlock()

	nodeIDs := db.getAllNodeIDs(graphName)
	nodes := make([]GraphNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		node := db.getGraphNode(graphName, id)
		if node != nil {
			nodes = append(nodes, *node)
		}
	}
	return nodes
}

func (db *RedisDB) GraphListEdges(graphName string) []GraphEdge {
	db.mu.RLock()
	defer db.mu.RUnlock()

	edgeIDs := db.getAllEdgeIDs(graphName)
	edges := make([]GraphEdge, 0, len(edgeIDs))
	for _, id := range edgeIDs {
		edge := db.buildGraphEdge(graphName, id)
		if edge != nil {
			edges = append(edges, *edge)
		}
	}
	return edges
}

func (db *RedisDB) GraphQuery(graphName, cypher string) ([]GraphResult, error) {
	cypher = strings.TrimSpace(cypher)
	upper := strings.ToUpper(cypher)

	if strings.HasPrefix(upper, "CREATE") {
		db.mu.Lock()
		defer db.mu.Unlock()
		return db.graphQueryCreate(graphName, cypher)
	}

	if strings.HasPrefix(upper, "MATCH") {
		db.mu.RLock()
		defer db.mu.RUnlock()
		return db.graphQueryMatch(graphName, cypher)
	}

	if strings.HasPrefix(upper, "DELETE") {
		db.mu.Lock()
		defer db.mu.Unlock()
		return db.graphQueryDelete(graphName, cypher)
	}

	return nil, nil
}

func (db *RedisDB) graphQueryMatch(graphName, cypher string) ([]GraphResult, error) {
	whereConditions := make(map[string]string)
	returnClause := ""

	upper := strings.ToUpper(cypher)
	
	if returnIdx := strings.Index(upper, "RETURN"); returnIdx >= 0 {
		returnClause = strings.TrimSpace(cypher[returnIdx+6:])
		cypher = strings.TrimSpace(cypher[:returnIdx])
		upper = strings.ToUpper(cypher)
	}

	if whereIdx := strings.Index(upper, "WHERE"); whereIdx >= 0 {
		whereStr := strings.TrimSpace(cypher[whereIdx+5:])
		cypher = strings.TrimSpace(cypher[:whereIdx])
		whereConditions = parseWhereConditions(whereStr)
	}

	cypher = strings.TrimPrefix(cypher, "MATCH")
	cypher = strings.TrimSpace(cypher)

	parts := strings.SplitN(cypher, "-", 2)
	if len(parts) < 2 {
		return nil, nil
	}

	leftPart := strings.TrimSpace(parts[0])
	leftPart = strings.Trim(leftPart, "()")

	rightPart := strings.TrimPrefix(parts[1], "[")
	rightPart = strings.TrimSuffix(rightPart, "]->")
	rightPart = strings.TrimSpace(rightPart)
	if idx := strings.Index(rightPart, "]-"); idx >= 0 {
		rightPart = strings.TrimSpace(rightPart[:idx])
	}

	targetPart := strings.TrimPrefix(parts[1], "[")
	if idx := strings.Index(targetPart, "]->("); idx >= 0 {
		targetPart = targetPart[idx+4:]
		targetPart = strings.TrimRight(targetPart, ")")
		targetPart = strings.TrimSpace(targetPart)
	} else {
		return nil, nil
	}

	leftParts := strings.Split(leftPart, ":")
	sourceVar := strings.TrimSpace(leftParts[0])
	var sourceLabel string
	if len(leftParts) > 1 {
		sourceLabel = strings.TrimSpace(leftParts[1])
	}

	edgeParts := strings.Split(rightPart, ":")
	edgeVar := strings.TrimSpace(edgeParts[0])
	var edgeType string
	if len(edgeParts) > 1 {
		edgeType = strings.TrimSpace(edgeParts[1])
	}

	targetParts := strings.Split(targetPart, ":")
	targetVar := strings.TrimSpace(targetParts[0])
	var targetLabel string
	if len(targetParts) > 1 {
		targetLabel = strings.TrimSpace(targetParts[1])
	}

	var results []GraphResult

	sourceIDs := db.getNodeIDsByLabel(graphName, sourceLabel)
	if len(sourceIDs) == 0 {
		sourceIDs = db.getAllNodeIDs(graphName)
	}

	for _, srcID := range sourceIDs {
		outgoingEdges := db.getOutgoingEdges(graphName, srcID, edgeType)
		for _, edgeID := range outgoingEdges {
			edgeInfo := db.getGraphEdge(graphName, edgeID)
			if edgeInfo == nil {
				continue
			}

			targetID := edgeInfo[1]
			if targetLabel != "" {
				targetIDs := db.getNodeIDsByLabel(graphName, targetLabel)
				found := false
				for _, tid := range targetIDs {
					if tid == targetID {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			srcNode := db.getGraphNode(graphName, srcID)
			tgtNode := db.getGraphNode(graphName, targetID)

			if len(whereConditions) > 0 {
				matched := false
				for key, value := range whereConditions {
					propKey := key
					if idx := strings.Index(key, "."); idx >= 0 {
						propKey = key[idx+1:]
					}
					if srcNode.Properties[propKey] == value || tgtNode.Properties[propKey] == value {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			edge := db.buildGraphEdge(graphName, edgeID)

			result := GraphResult{}

			if returnClause == "" || strings.Contains(strings.ToUpper(returnClause), "COUNT") {
				result.Nodes = []GraphNode{*srcNode, *tgtNode}
				result.Edges = []GraphEdge{*edge}
			} else {
				if strings.Contains(strings.ToUpper(returnClause), sourceVar) || strings.Contains(strings.ToUpper(returnClause), "*") {
					if !containsNode(result.Nodes, *srcNode) {
						result.Nodes = append(result.Nodes, *srcNode)
					}
				}
				if strings.Contains(strings.ToUpper(returnClause), targetVar) || strings.Contains(strings.ToUpper(returnClause), "*") {
					if !containsNode(result.Nodes, *tgtNode) {
						result.Nodes = append(result.Nodes, *tgtNode)
					}
				}
				if strings.Contains(strings.ToUpper(returnClause), edgeVar) || strings.Contains(strings.ToUpper(returnClause), "*") {
					result.Edges = append(result.Edges, *edge)
				}
			}

			if len(result.Nodes) > 0 || len(result.Edges) > 0 {
				results = append(results, result)
			}
		}
	}

	_ = sourceVar
	_ = edgeVar
	_ = targetVar
	_ = returnClause

	return results, nil
}

func (db *RedisDB) graphQueryCreate(graphName, cypher string) ([]GraphResult, error) {
	cypher = strings.TrimPrefix(cypher, "CREATE")
	cypher = strings.TrimSpace(cypher)

	cypher = strings.TrimPrefix(cypher, "(")
	cypher = strings.TrimSuffix(cypher, ")")

	var results []GraphResult

	parts := strings.Split(cypher, "->")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "()[]")

		if strings.Contains(part, ":") {
			labelParts := strings.SplitN(part, ":", 2)
			varName := strings.TrimSpace(labelParts[0])
			rest := strings.TrimSpace(labelParts[1])

			props := make(map[string]string)
			if idx := strings.Index(rest, "{"); idx >= 0 {
				propStr := rest[idx:]
				rest = strings.TrimSpace(rest[:idx])
				propStr = strings.Trim(propStr, "{}")
				for _, kv := range strings.Split(propStr, ",") {
					kvParts := strings.SplitN(kv, ":", 2)
					if len(kvParts) == 2 {
						props[strings.TrimSpace(kvParts[0])] = strings.TrimSpace(kvParts[1])
					}
				}
			}

			if rest != "" {
				nodeLabel := rest
				nodeID := fmt.Sprintf("node_%d_%s", i, varName)
				if existingID := db.findNodeByProperty(graphName, varName, props); existingID != "" {
					nodeID = existingID
				} else {
					db.graphAddNodeLocked(graphName, nodeID, []string{nodeLabel}, props)
				}
				node := db.getGraphNode(graphName, nodeID)
				results = append(results, GraphResult{Nodes: []GraphNode{*node}})
			}
		}

		if i < len(parts)-1 {
			edgePart := strings.TrimSpace(parts[i+1])
			edgePart = strings.Trim(edgePart, "()[]")

			var edgeType, tgtVar string
			if strings.Contains(edgePart, ":") {
				edgeParts := strings.SplitN(edgePart, ":", 2)
				_ = strings.TrimSpace(edgeParts[0])
				edgeType = strings.TrimSpace(edgeParts[1])
				edgeParts2 := strings.SplitN(edgePart, "(", 2)
				if len(edgeParts2) > 1 {
					tgtPart := strings.Trim(edgeParts2[1], ")")
					tgtParts := strings.SplitN(tgtPart, ":", 2)
					tgtVar = strings.TrimSpace(tgtParts[0])
				}
			}

			if edgeType != "" {
				edgeID := fmt.Sprintf("edge_%d_%s", i, edgeType)
				props := make(map[string]string)
				if idx := strings.Index(edgePart, "{"); idx >= 0 {
					propStr := edgePart[idx:]
					propStr = strings.Trim(propStr, "{}")
					for _, kv := range strings.Split(propStr, ",") {
						kvParts := strings.SplitN(kv, ":", 2)
						if len(kvParts) == 2 {
							props[strings.TrimSpace(kvParts[0])] = strings.TrimSpace(kvParts[1])
						}
					}
				}

				lastNodeID := ""
				if len(results) > 0 && len(results[len(results)-1].Nodes) > 0 {
					lastNodeID = results[len(results)-1].Nodes[len(results[len(results)-1].Nodes)-1].ID
				}

				db.graphAddEdgeLocked(graphName, edgeID, edgeType, lastNodeID, tgtVar, props)
			}
		}
	}

	return results, nil
}

func (db *RedisDB) graphQueryDelete(graphName, cypher string) ([]GraphResult, error) {
	cypher = strings.TrimPrefix(cypher, "DELETE")
	cypher = strings.TrimSpace(cypher)

	var results []GraphResult

	if strings.HasPrefix(strings.ToUpper(cypher), "NODE") {
		cypher = strings.TrimPrefix(cypher, "NODE")
		cypher = strings.TrimSpace(cypher)
		cypher = strings.Trim(cypher, "()")

		if nodeID := strings.TrimSpace(cypher); nodeID != "" {
			nodeKey := graphName + ":node:" + nodeID
			headOff, ok := db.dict.Get([]byte(nodeKey))
			if ok {
				node := db.getGraphNode(graphName, nodeID)
				results = append(results, GraphResult{Nodes: []GraphNode{*node}})
				db.dict.Del([]byte(nodeKey))
				_ = headOff
			}
		}
	}

	return results, nil
}

func (db *RedisDB) findNodeByProperty(graphName, varName string, props map[string]string) string {
	return ""
}

func parseWhereConditions(whereStr string) map[string]string {
	conditions := make(map[string]string)
	whereStr = strings.TrimSpace(whereStr)

	parts := strings.Split(whereStr, "AND")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		for _, op := range []string{"=", "!=", ">=", "<=", ">", "<"} {
			if idx := strings.Index(part, op); idx >= 0 {
				key := strings.TrimSpace(part[:idx])
				value := strings.TrimSpace(part[idx+len(op):])
				key = strings.Trim(key, " .")
				value = strings.Trim(value, " '\"")

				conditions[key] = value
				break
			}
		}
	}
	return conditions
}

func nodeMatchesConditions(node *GraphNode, conditions map[string]string) bool {
	for key, value := range conditions {
		propKey := key
		if idx := strings.Index(key, "."); idx >= 0 {
			propKey = key[idx+1:]
		}
		if nodeVal, ok := node.Properties[propKey]; ok {
			if nodeVal != value {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

func containsNode(nodes []GraphNode, node GraphNode) bool {
	for _, n := range nodes {
		if n.ID == node.ID {
			return true
		}
	}
	return false
}

func (db *RedisDB) getNodeIDsByLabel(graphName, label string) []string {
	if label == "" {
		return nil
	}
	labelIdxKey := graphName + ":label:" + label
	headOff, exists := db.dict.Get([]byte(labelIdxKey))
	if !exists {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc == ObjEncodingIntset && dataOff != 0 {
		isOff := int(dataOff)
		n := intsetLen(db.arena, isOff)
		result := make([]string, n)
		for i := 0; i < n; i++ {
			val := intsetGet(db.arena, isOff, i)
			result[i] = fmt.Sprintf("%d", val)
		}
		return result
	}

	if enc == ObjEncodingHashtable && dataOff != 0 {
		innerDict := LoadDictMeta(db.arena, dataOff)
		var result []string
		iter := innerDict.Iterator()
		for iter.Next() {
			result = append(result, string(iter.Key()))
		}
		return result
	}

	return nil
}

func (db *RedisDB) getAllNodeIDs(graphName string) []string {
	var result []string
	prefix := graphName + ":node:"
	iter := db.dict.Iterator()
	for iter.Next() {
		key := string(iter.Key())
		if strings.HasPrefix(key, prefix) {
			nodeID := strings.TrimPrefix(key, prefix)
			result = append(result, nodeID)
		}
	}
	return result
}

func (db *RedisDB) getAllEdgeIDs(graphName string) []string {
	var result []string
	prefix := graphName + ":edge:"
	iter := db.dict.Iterator()
	for iter.Next() {
		key := string(iter.Key())
		if strings.HasPrefix(key, prefix) {
			edgeID := strings.TrimPrefix(key, prefix)
			result = append(result, edgeID)
		}
	}
	return result
}

func (db *RedisDB) getOutgoingEdges(graphName, nodeID, edgeType string) []string {
	prefix := graphName + ":edge:"
	edgeIDs := db.getAllEdgeIDs(graphName)
	var result []string
	for _, edgeID := range edgeIDs {
		edgeKey := prefix + edgeID
		headOff, ok := db.dict.Get([]byte(edgeKey))
		if !ok {
			continue
		}
		dataOff := db.ObjectDataOffset(headOff)
		if dataOff == 0 {
			continue
		}
		size := db.arena.SizeAt(dataOff)
		data := db.arena.ReadBytes(dataOff, size)
		edgeData := string(data)
		parts := strings.Split(edgeData, "|")
		if len(parts) >= 3 && parts[1] == nodeID && parts[0] == edgeType {
			result = append(result, edgeID)
		}
	}
	return result
}

func (db *RedisDB) getGraphNode(graphName, nodeID string) *GraphNode {
	nodeKey := graphName + ":node:" + nodeID
	headOff, ok := db.dict.Get([]byte(nodeKey))
	if !ok {
		return &GraphNode{ID: nodeID}
	}

	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return &GraphNode{ID: nodeID}
	}
	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)

	labels, props := deserializeGraphEntity(string(data))
	return &GraphNode{
		ID:         nodeID,
		Labels:     labels,
		Properties: props,
	}
}

func (db *RedisDB) getGraphEdge(graphName, edgeID string) []string {
	edgeKey := graphName + ":edge:" + edgeID
	headOff, ok := db.dict.Get([]byte(edgeKey))
	if !ok {
		return nil
	}

	dataOff := db.ObjectDataOffset(headOff)
	if dataOff == 0 {
		return nil
	}
	size := db.arena.SizeAt(dataOff)
	data := db.arena.ReadBytes(dataOff, size)

	parts := strings.SplitN(string(data), "|", 4)
	if len(parts) < 3 {
		return nil
	}
	return parts
}

func (db *RedisDB) buildGraphEdge(graphName, edgeID string) *GraphEdge {
	parts := db.getGraphEdge(graphName, edgeID)
	if parts == nil || len(parts) < 3 {
		return &GraphEdge{ID: edgeID}
	}

	edge := &GraphEdge{
		ID:     edgeID,
		Type:   parts[0],
		Source: parts[1],
		Target: parts[2],
	}
	return edge
}

func serializeGraphEntity(labels []string, props map[string]string) string {
	parts := make([]string, 0)
	parts = append(parts, strings.Join(labels, ","))
	for k, v := range props {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "|")
}

func deserializeGraphEntity(data string) (labels []string, props map[string]string) {
	parts := strings.Split(data, "|")
	props = make(map[string]string)

	if len(parts) > 0 && parts[0] != "" {
		labels = strings.Split(parts[0], ",")
	} else {
		labels = []string{}
	}

	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			props[kv[0]] = kv[1]
		}
	}
	return
}

func stringToNodeIDHash(s string) uint64 {
	var hash uint64 = 5381
	for _, c := range s {
		hash = ((hash << 5) + hash) + uint64(c)
	}
	if hash == 0 {
		hash = 1
	}
	return hash
}
