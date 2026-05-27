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

func (db *RedisDB) graphAddNode(graphName, nodeID string, labels []string, props map[string]string) {
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
		pb := Buf(nodeID)
		db.SAdd(labelIdxKey, pb)
		pb.Close()
	}
}

func (db *RedisDB) graphAddEdge(graphName, edgeID, edgeType, sourceID, targetID string, props map[string]string) {
	edgeKey := graphName + ":edge:" + edgeID
	outKey := graphName + ":out:" + sourceID + ":" + edgeType
	inKey := graphName + ":in:" + targetID + ":" + edgeType

	edgeData := append([]byte(edgeType+"|"+sourceID+"|"+targetID+"|"), serializeGraphEntity(nil, props)...)
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

	pb1 := Buf(edgeID)
	db.ZAdd(outKey, 0, pb1)
	pb1.Close()

	pb2 := Buf(edgeID)
	db.ZAdd(inKey, 0, pb2)
	pb2.Close()
}

func (db *RedisDB) GraphQuery(graphName, cypher string) ([]GraphResult, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	cypher = strings.TrimSpace(cypher)
	upper := strings.ToUpper(cypher)

	if strings.HasPrefix(upper, "MATCH") {
		return db.graphQueryMatch(graphName, cypher)
	}

	return nil, nil
}

func (db *RedisDB) graphQueryMatch(graphName, cypher string) ([]GraphResult, error) {
	returnClause := ""
	if idx := strings.Index(strings.ToUpper(cypher), "RETURN"); idx >= 0 {
		returnClause = strings.TrimSpace(cypher[idx+6:])
		cypher = strings.TrimSpace(cypher[:idx])
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
			edge := db.buildGraphEdge(graphName, edgeID)

			result := GraphResult{
				Nodes: []GraphNode{*srcNode, *tgtNode},
				Edges: []GraphEdge{*edge},
			}
			results = append(results, result)
		}
	}

	_ = sourceVar
	_ = edgeVar
	_ = targetVar
	_ = returnClause

	return results, nil
}

func (db *RedisDB) getNodeIDsByLabel(graphName, label string) []string {
	if label == "" {
		return nil
	}
	labelIdxKey := graphName + ":label:" + label
	members := db.SMembers(labelIdxKey)
	result := make([]string, len(members))
	for i, m := range members {
		result[i] = m.String()
	}
	return result
}

func (db *RedisDB) getAllNodeIDs(graphName string) []string {
	var result []string
	return result
}

func (db *RedisDB) getOutgoingEdges(graphName, nodeID, edgeType string) []string {
	outKey := graphName + ":out:" + nodeID + ":" + edgeType
	members := db.ZRange(outKey, 0, -1)
	result := make([]string, members.Len())
	for i := 0; i < members.Len(); i++ {
		result[i] = string(members.Get(i))
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
