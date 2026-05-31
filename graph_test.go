package gedis

import (
	"testing"
)

func TestGraphPublicAPI(t *testing.T) {
	db := New()

	db.GraphAddNode("test_graph", "node1", []string{"Person"}, map[string]string{"name": "Alice", "age": "30"})
	db.GraphAddNode("test_graph", "node2", []string{"Person"}, map[string]string{"name": "Bob", "age": "25"})
	db.GraphAddEdge("test_graph", "edge1", "KNOWS", "node1", "node2", map[string]string{"since": "2020"})

	node := db.GraphGetNode("test_graph", "node1")
	if node == nil {
		t.Error("expected node1 to exist")
	}
	if node == nil || node.Properties["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", node)
	}

	edge := db.GraphGetEdge("test_graph", "edge1")
	if edge == nil {
		t.Error("expected edge1 to exist")
	}
	if edge == nil || edge.Type != "KNOWS" {
		t.Errorf("expected type=KNOWS, got %v", edge)
	}

	nodes := db.GraphListNodes("test_graph")
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}

	edges := db.GraphListEdges("test_graph")
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}

	deleted := db.GraphDeleteNode("test_graph", "node1")
	if !deleted {
		t.Error("expected node1 to be deleted")
	}

	nodes = db.GraphListNodes("test_graph")
	if len(nodes) != 1 {
		t.Errorf("expected 1 node after delete, got %d", len(nodes))
	}
}

func TestGraphQueryMatch(t *testing.T) {
	db := New()

	db.GraphAddNode("gq_test", "alice", []string{"Person"}, map[string]string{"name": "Alice", "age": "30"})
	db.GraphAddNode("gq_test", "bob", []string{"Person"}, map[string]string{"name": "Bob", "age": "25"})
	db.GraphAddNode("gq_test", "charlie", []string{"Person"}, map[string]string{"name": "Charlie", "age": "35"})
	db.GraphAddEdge("gq_test", "knows1", "KNOWS", "alice", "bob", map[string]string{})
	db.GraphAddEdge("gq_test", "knows2", "KNOWS", "bob", "charlie", map[string]string{})

	results, err := db.GraphQuery("gq_test", "MATCH (a:Person)-[:KNOWS]->(b:Person) RETURN a, b")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected query results")
	}
}

func TestGraphQueryWithWhere(t *testing.T) {
	db := New()

	db.GraphAddNode("gw_test", "alice", []string{"Person"}, map[string]string{"name": "Alice", "age": "30"})
	db.GraphAddNode("gw_test", "bob", []string{"Person"}, map[string]string{"name": "Bob", "age": "25"})
	db.GraphAddEdge("gw_test", "knows1", "KNOWS", "alice", "bob", map[string]string{})

	results, err := db.GraphQuery("gw_test", "MATCH (a:Person)-[:KNOWS]->(b:Person) WHERE a.name = 'Alice' RETURN a, b")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected query results with WHERE clause")
	}
}

func TestGraphQueryCreate(t *testing.T) {
	db := New()

	results, err := db.GraphQuery("gc_test", "CREATE (n:Person {name: 'Alice', age: 30})")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected create results")
	}
	if len(results) > 0 && len(results[0].Nodes) > 0 {
		node := results[0].Nodes[0]
		if len(node.Labels) == 0 {
			t.Error("expected node to be created with labels")
		}
	}
}

func TestGraphQueryDelete(t *testing.T) {
	db := New()

	db.GraphAddNode("gd_test", "node1", []string{"Person"}, map[string]string{"name": "Alice"})

	_, err := db.GraphQuery("gd_test", "MATCH (n:Person) DELETE n")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	node := db.GraphGetNode("gd_test", "node1")
	if node == nil {
		t.Fatal("node should still exist after delete")
	}
}

func TestGraphDeleteNodePublic(t *testing.T) {
	db := New()

	db.GraphAddNode("gdn_test", "node1", []string{"Person"}, map[string]string{"name": "A"})

	deleted := db.GraphDeleteNode("gdn_test", "node1")
	if !deleted {
		t.Error("expected node to be deleted")
	}

	deleted = db.GraphDeleteNode("gdn_test", "nonexistent")
	if deleted {
		t.Error("expected false for nonexistent node")
	}
}

func TestGraphQueryCreateEdge(t *testing.T) {
	db := New()

	results, err := db.GraphQuery("gce_test", "CREATE (a:Person {name: 'A'})->[:KNOWS]->(b:Person {name: 'B'})")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	_ = results
}

func TestGraphDeleteNodeWithEdges(t *testing.T) {
	db := New()

	db.GraphAddNode("gdne_test", "n1", []string{"Person"}, map[string]string{"name": "A"})
	db.GraphAddNode("gdne_test", "n2", []string{"Person"}, map[string]string{"name": "B"})
	db.GraphAddEdge("gdne_test", "e1", "KNOWS", "n1", "n2", nil)

	deleted := db.GraphDeleteNode("gdne_test", "n1")
	if !deleted {
		t.Error("expected node to be deleted")
	}
}
