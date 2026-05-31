package gedis

import (
	"testing"
)

func TestStreamBasic(t *testing.T) {
	db := New()

	db.XAdd("stream_test", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
		"field2": Buf("value2"),
	})

	db.XAdd("stream_test", "*", map[string]*PooledBuffer{
		"field1": Buf("value3"),
		"field2": Buf("value4"),
	})

	count := db.XLen("stream_test")
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}
}

func TestStreamConsumer(t *testing.T) {
	db := New()

	db.XAdd("stream_consumer", "*", map[string]*PooledBuffer{
		"key1": Buf("val1"),
	})

	db.XGroupCreate("stream_consumer", "group1", "$")

	result := db.XReadGroup("group1", "consumer1", map[string]string{"stream_consumer": ">"}, 1)
	if len(result) == 0 {
		t.Error("expected stream entries")
	}
}

func TestStreamXInfo(t *testing.T) {
	db := New()

	db.XAdd("stream_xinfo", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})

	info := db.XInfo("stream_xinfo")
	if info == nil {
		t.Error("expected stream info")
	}
}

func TestStreamXInfoGroups(t *testing.T) {
	db := New()

	db.XAdd("stream_groups", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})
	db.XGroupCreate("stream_groups", "group1", "$")

	groups := db.XInfoGroups("stream_groups")
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
	}
}

func TestStreamXPending(t *testing.T) {
	db := New()

	db.XAdd("stream_pending", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})
	db.XGroupCreate("stream_pending", "group1", "$")

	pending := db.XPending("stream_pending", "group1")
	if pending == nil {
		t.Error("expected pending info")
	}
}

func TestStreamXClaim(t *testing.T) {
	db := New()

	db.XAdd("stream_claim", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})
	db.XGroupCreate("stream_claim", "group1", "0")

	result := db.XReadGroup("group1", "consumer1", map[string]string{"stream_claim": ">"}, 1)
	if len(result) == 0 {
		t.Error("expected stream entries")
	}

	claimed := db.XClaim("stream_claim", "group1", "consumer2", 0, []string{"0-1"})
	if len(claimed) != 1 {
		t.Errorf("expected 1 claimed entry, got %d", len(claimed))
	}
}

func TestStreamXAutoClaim(t *testing.T) {
	db := New()

	db.XAdd("stream_autoclaim", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})
	db.XGroupCreate("stream_autoclaim", "group1", "0")

	db.XReadGroup("group1", "consumer1", map[string]string{"stream_autoclaim": ">"}, 1)

	nextID, claimed := db.XAutoClaim("stream_autoclaim", "group1", "consumer2", "0", 1)
	if nextID == "" {
		t.Error("expected next ID")
	}
	_ = claimed
}

func TestStreamXGroupConsumer(t *testing.T) {
	db := New()

	db.XAdd("stream_group_consumer", "*", map[string]*PooledBuffer{
		"field1": Buf("value1"),
	})
	db.XGroupCreate("stream_group_consumer", "group1", "$")

	created := db.XGroupCreateConsumer("stream_group_consumer", "group1", "consumer1")
	if !created {
		t.Error("expected consumer to be created")
	}

	count := db.XGroupDelConsumer("stream_group_consumer", "group1", "consumer1")
	if count != 1 {
		t.Errorf("expected 1 consumer deleted, got %d", count)
	}
}
