package gedis

import (
	"testing"
)

func TestJsonDelPath(t *testing.T) {
	db := New()

	db.JsonSet("json_del", "$", []byte("{\"a\":1,\"b\":2,\"c\":3}"))

	err := db.JsonDel("json_del", "$.b")
	if err != nil {
		t.Errorf("JsonDel failed: %v", err)
	}

	val, err := db.JsonGet("json_del", "$")
	if err != nil {
		t.Fatalf("JsonGet failed: %v", err)
	}
	t.Logf("JsonGet result: %v", val)
}

func TestJsonDelNested(t *testing.T) {
	db := New()

	db.JsonSet("json_nested", "$", []byte("{\"a\":{\"b\":1,\"c\":2},\"d\":3}"))

	err := db.JsonDel("json_nested", "$.a.b")
	if err != nil {
		t.Errorf("JsonDel failed: %v", err)
	}

	val, err := db.JsonGet("json_nested", "$.a")
	if err != nil {
		t.Fatalf("JsonGet failed: %v", err)
	}
	t.Logf("JsonGet $.a result: %v", val)
}

func TestJsonPath(t *testing.T) {
	db := New()

	db.JsonSet("json_path", "$", map[string]interface{}{
		"name": "test",
		"nested": map[string]interface{}{
			"a": float64(1),
			"b": []interface{}{float64(1), float64(2), float64(3)},
		},
	})

	result, _ := db.JsonGet("json_path", "$.name")
	if result != "test" {
		t.Errorf("expected \"test\", got %v", result)
	}

	result, _ = db.JsonGet("json_path", "$.nested.a")
	if result != float64(1) {
		t.Errorf("expected 1, got %v", result)
	}

	lenVal, err := db.JsonObjLen("json_path", "$.nested")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if lenVal != 2 {
		t.Errorf("expected 2 (map keys), got %d", lenVal)
	}

	err = db.JsonDel("json_path", "$.name")
	if err != nil {
		t.Errorf("expected JsonDel to succeed, got error: %v", err)
	}

	result, _ = db.JsonGet("json_path", "$.name")
	if result != nil {
		t.Errorf("expected nil after delete, got %v", result)
	}
}
