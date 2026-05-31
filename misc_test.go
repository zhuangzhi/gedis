package gedis

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecrBy(t *testing.T) {
	db := New()

	db.Set("decr_key", []byte("10"))
	db.DecrBy("decr_key", 3)

	val, ok := db.Get("decr_key")
	if !ok {
		t.Fatal("expected decr_key to exist")
	}
	if val.String() != "7" {
		t.Errorf("expected '7', got %s", val.String())
	}
	val.Close()

	db.DecrBy("decr_key", 10)
	val, ok = db.Get("decr_key")
	if !ok {
		t.Fatal("expected decr_key to exist")
	}
	if val.String() != "-3" {
		t.Errorf("expected '-3', got %s", val.String())
	}
	val.Close()
}

func TestSDiffAndSDiffStore(t *testing.T) {
	db := New()

	db.SAdd("set1", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("set2", []byte("b"), []byte("c"), []byte("d"))
	db.SAdd("set3", []byte("c"))

	result := db.SDiff("set1", "set2")
	if result.Len() != 1 {
		t.Errorf("expected 1 element, got %d", result.Len())
	}
	result.Close()

	count := db.SDiffStore("diff_result", "set1", "set2", "set3")
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	result2 := db.SMembers("diff_result")
	if result2.Len() != 1 {
		t.Errorf("expected 1 element, got %d", result2.Len())
	}
	result2.Close()
}

func TestZRankWithElementByRank(t *testing.T) {
	db := New()

	db.ZAdd("zset", 1.0, []byte("a"))
	db.ZAdd("zset", 2.0, []byte("b"))
	db.ZAdd("zset", 3.0, []byte("c"))

	rank, ok := db.ZRank("zset", []byte("b"))
	if !ok || rank != 1 {
		t.Errorf("expected rank 1 for 'b', got %d (ok=%v)", rank, ok)
	}

	rank, ok = db.ZRevRank("zset", []byte("b"))
	if !ok || rank != 1 {
		t.Errorf("expected rev rank 1 for 'b', got %d (ok=%v)", rank, ok)
	}
}

func TestGetEx(t *testing.T) {
	db := New()

	db.Set("getex_key", []byte("old_value"))

	result := db.GetEx("getex_key", []byte("new_value"), 1000)
	if !result {
		t.Error("expected GetEx to return true")
	}
}

func TestHSetNX(t *testing.T) {
	db := New()

	pb := Buf("value1")
	result := db.HSetNX("hash_nx", "field1", pb)
	pb.Close()
	if !result {
		t.Error("expected HSetNX to return true for new field")
	}

	pb = Buf("value2")
	result = db.HSetNX("hash_nx", "field1", pb)
	pb.Close()
	if result {
		t.Error("expected HSetNX to return false for existing field")
	}

	val, ok := db.HGet("hash_nx", "field1")
	if !ok || val.String() != "value1" {
		t.Errorf("expected value1, got %s", val.String())
	}
	val.Close()
}

func TestHIncrByFloat(t *testing.T) {
	db := New()

	db.HSet("hash_incr", "float_field", []byte("10.5"))

	result, err := db.HIncrByFloat("hash_incr", "float_field", 2.5)
	if err != nil {
		t.Fatalf("HIncrByFloat failed: %v", err)
	}
	if result != 13.0 {
		t.Errorf("expected 13.0, got %f", result)
	}

	val, ok := db.HGet("hash_incr", "float_field")
	if !ok || val.String() != "13" {
		t.Errorf("expected '13', got %s", val.String())
	}
	val.Close()
}

func TestPFCount(t *testing.T) {
	db := New()

	db.PFAdd("hll1", []byte("a"), []byte("b"), []byte("c"))
	db.PFAdd("hll2", []byte("b"), []byte("c"), []byte("d"))

	count := db.PFCount("hll1")
	if count < 2 || count > 4 {
		t.Errorf("expected approximate count 3, got %d", count)
	}

	count = db.PFCount("hll1", "hll2")
	if count < 3 || count > 6 {
		t.Errorf("expected approximate count 4-5, got %d", count)
	}
}

func TestFindLatestRDB(t *testing.T) {
	tmpDir := os.TempDir()

	rdb1 := filepath.Join(tmpDir, "gedis_1000.rdb")
	rdb2 := filepath.Join(tmpDir, "gedis_2000.rdb")
	rdb3 := filepath.Join(tmpDir, "gedis_500.rdb")

	os.WriteFile(rdb1, []byte("test1"), 0644)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(rdb2, []byte("test2"), 0644)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(rdb3, []byte("test3"), 0644)

	defer os.Remove(rdb1)
	defer os.Remove(rdb2)
	defer os.Remove(rdb3)

	db := New()
	cfg := PersistenceConfig{
		RDB: RDBConfig{
			Enabled: true,
			Path:    filepath.Join(tmpDir, "gedis.rdb"),
		},
	}

	pm, err := NewPersistenceManager(db, cfg)
	if err != nil {
		t.Fatalf("NewPersistenceManager failed: %v", err)
	}
	defer pm.Stop()

	latest, err := findLatestRDB(tmpDir, "gedis")
	if err != nil {
		t.Fatalf("findLatestRDB failed: %v", err)
	}
	if latest != rdb3 {
		t.Errorf("expected %s, got %s", rdb3, latest)
	}
}

func TestExecuteCommand(t *testing.T) {
	db := New()

	cmd := &Command{
		Op:   "SET",
		Key:  "exec_key",
		Args: [][]byte{[]byte("exec_value")},
	}

	err := db.ExecuteCommand(cmd)
	if err != nil {
		t.Errorf("ExecuteCommand failed: %v", err)
	}

	val, ok := db.Get("exec_key")
	if !ok || val.String() != "exec_value" {
		t.Errorf("expected exec_value, got %s", val.String())
	}
	val.Close()
}

func TestExecuteCommandDel(t *testing.T) {
	db := New()
	db.Set("del_key", []byte("del_value"))

	cmd := &Command{
		Op:  "DEL",
		Key: "del_key",
	}

	err := db.ExecuteCommand(cmd)
	if err != nil {
		t.Errorf("ExecuteCommand DEL failed: %v", err)
	}

	if db.Exists("del_key") {
		t.Error("expected key to be deleted")
	}
}

func TestExecuteCommandHSet(t *testing.T) {
	db := New()

	cmd := &Command{
		Op:   "HSET",
		Key:  "hset_exec",
		Args: [][]byte{[]byte("field1"), []byte("value1")},
	}

	err := db.ExecuteCommand(cmd)
	if err != nil {
		t.Errorf("ExecuteCommand HSET failed: %v", err)
	}

	val, ok := db.HGet("hset_exec", "field1")
	if !ok || val.String() != "value1" {
		t.Errorf("expected value1, got %s", val.String())
	}
	val.Close()
}

func TestExecuteCommandIncr(t *testing.T) {
	db := New()
	db.Set("incr_key", []byte("10"))

	cmd := &Command{
		Op:  "INCR",
		Key: "incr_key",
	}

	err := db.ExecuteCommand(cmd)
	if err != nil {
		t.Errorf("ExecuteCommand INCR failed: %v", err)
	}

	val, ok := db.Get("incr_key")
	if !ok || val.String() != "11" {
		t.Errorf("expected 11, got %s", val.String())
	}
	val.Close()
}

func TestExecuteCommandAppend(t *testing.T) {
	db := New()
	db.Set("append_key", []byte("hello"))

	cmd := &Command{
		Op:   "APPEND",
		Key:  "append_key",
		Args: [][]byte{[]byte(" world")},
	}

	err := db.ExecuteCommand(cmd)
	if err != nil {
		t.Errorf("ExecuteCommand APPEND failed: %v", err)
	}

	val, ok := db.Get("append_key")
	if !ok || val.String() != "hello world" {
		t.Errorf("expected 'hello world', got %s", val.String())
	}
	val.Close()
}

func TestExecuteCommandUnknown(t *testing.T) {
	db := New()

	cmd := &Command{
		Op:   "UNKNOWN",
		Key:  "unknown_key",
		Args: [][]byte{[]byte("arg1")},
	}

	err := db.ExecuteCommand(cmd)
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestIntSetAddLowCov(t *testing.T) {
	db := New()

	db.SAdd("intset_lowcov", []byte("1"))
	db.SAdd("intset_lowcov", []byte("2"))
	db.SAdd("intset_lowcov", []byte("3"))

	if !db.SIsMember("intset_lowcov", []byte("1")) {
		t.Error("expected 1 to be member")
	}
	if !db.SIsMember("intset_lowcov", []byte("2")) {
		t.Error("expected 2 to be member")
	}
	if !db.SIsMember("intset_lowcov", []byte("3")) {
		t.Error("expected 3 to be member")
	}
}

func TestSDiffStoreSingle(t *testing.T) {
	db := New()

	db.SAdd("set_single", []byte("a"), []byte("b"))

	count := db.SDiffStore("diff_single", "set_single")
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	result := db.SMembers("diff_single")
	if result == nil || result.Len() != 2 {
		t.Errorf("expected 2 elements, got %d", result.Len())
	}
	if result != nil {
		result.Close()
	}
}

func TestSDiffStoreAllEmpty(t *testing.T) {
	db := New()

	count := db.SDiffStore("diff_all_empty", "nonexistent1", "nonexistent2")
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestGeoRadiusWithUnits(t *testing.T) {
	db := New()

	db.GeoAdd("geo_test", 13.361389, 38.115556, "Palermo")
	db.GeoAdd("geo_test", 15.087269, 37.502669, "Catania")

	results := db.GeoRadius("geo_test", 15.087269, 37.502669, 100.0, "km")
	if results == nil || results.Len() == 0 {
		t.Error("expected at least one result")
	}
	t.Logf("GeoRadius results count: %d", results.Len())
	if results != nil {
		results.Close()
	}
}

func TestIntSetUpgrade(t *testing.T) {
	db := New()

	db.SAdd("intset_upgrade", []byte("1"))
	db.SAdd("intset_upgrade", []byte("2"))
	db.SAdd("intset_upgrade", []byte("2147483648"))

	if !db.SIsMember("intset_upgrade", []byte("2147483648")) {
		t.Error("expected large int to be member")
	}
}

func TestListIntEntries(t *testing.T) {
	db := New()

	db.LPush("list_int", []byte("12345"))
	db.LPush("list_int", []byte("67890"))

	val, ok := db.LIndex("list_int", 0)
	if !ok {
		t.Error("expected element at index 0")
	}
	t.Logf("LIndex result: %s", val.String())
	val.Close()

	val, ok = db.LIndex("list_int", 1)
	if !ok {
		t.Error("expected element at index 1")
	}
	t.Logf("LIndex result: %s", val.String())
	val.Close()
}

func TestIntSetLargeValues(t *testing.T) {
	db := New()

	db.SAdd("intset_large", []byte("-2147483648"))
	db.SAdd("intset_large", []byte("2147483647"))
	db.SAdd("intset_large", []byte("0"))

	if !db.SIsMember("intset_large", []byte("-2147483648")) {
		t.Error("expected -2147483648 to be member")
	}
	if !db.SIsMember("intset_large", []byte("2147483647")) {
		t.Error("expected 2147483647 to be member")
	}
}

func TestIntSetDuplicate(t *testing.T) {
	db := New()

	db.SAdd("intset_dup", []byte("1"))
	db.SAdd("intset_dup", []byte("1"))
	db.SAdd("intset_dup", []byte("2"))

	count := db.SCard("intset_dup")
	if count != 2 {
		t.Errorf("expected 2 unique elements, got %d", count)
	}
}

func TestIntSetMixedValues(t *testing.T) {
	db := New()

	db.SAdd("intset_mixed", []byte("1"))
	db.SAdd("intset_mixed", []byte("300"))
	db.SAdd("intset_mixed", []byte("-200"))

	if !db.SIsMember("intset_mixed", []byte("1")) {
		t.Error("expected 1 to be member")
	}
	if !db.SIsMember("intset_mixed", []byte("300")) {
		t.Error("expected 300 to be member")
	}
	if !db.SIsMember("intset_mixed", []byte("-200")) {
		t.Error("expected -200 to be member")
	}
}

func TestHLLMurmurHash(t *testing.T) {
	db := New()

	db.PFAdd("hll_murmur", []byte("hello"))
	db.PFAdd("hll_murmur", []byte("world"))
	db.PFAdd("hll_murmur", []byte("hello"))

	count := db.PFCount("hll_murmur")
	if count < 1 || count > 3 {
		t.Errorf("expected count between 1-3, got %d", count)
	}
}

func TestHashHIncrByFloat(t *testing.T) {
	db := New()

	db.HSet("hash_float", "field1", []byte("1.5"))

	result, err := db.HIncrByFloat("hash_float", "field1", 2.5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != 4.0 {
		t.Errorf("expected 4.0, got %f", result)
	}
}

func TestGeoConvertToMeters(t *testing.T) {
	db := New()

	db.GeoAdd("geo_test", 116.4074, 39.9042, "beijing")
	db.GeoAdd("geo_test", 121.4737, 31.2304, "shanghai")

	dist := db.GeoDist("geo_test", "beijing", "shanghai", "m")
	if dist <= 0 {
		t.Errorf("expected positive distance, got %f", dist)
	}

	distKm := db.GeoDist("geo_test", "beijing", "shanghai", "km")
	if distKm <= 0 {
		t.Errorf("expected positive distance in km, got %f", distKm)
	}
}

func TestHashStrlenLowCov(t *testing.T) {
	db := New()

	db.HSet("hsl_test", "field1", []byte("hello"))
	db.HSet("hsl_test", "field2", []byte("world!"))

	if v := db.HStrLen("hsl_test", "field1"); v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
	if v := db.HStrLen("hsl_test", "field2"); v != 6 {
		t.Errorf("expected 6, got %d", v)
	}
}

func TestSMoveLowCov(t *testing.T) {
	db := New()

	db.SAdd("src_set", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("dst_set", []byte("x"))

	moved := db.SMove("src_set", "dst_set", []byte("a"))
	if !moved {
		t.Error("expected move to succeed")
	}
	if db.SIsMember("src_set", []byte("a")) {
		t.Error("expected a to be removed from src")
	}
	if !db.SIsMember("dst_set", []byte("a")) {
		t.Error("expected a to be in dst")
	}
}

func TestZRankAndRevRankLowCov(t *testing.T) {
	db := New()

	db.ZAdd("zrank_test", 10, []byte("c"))
	db.ZAdd("zrank_test", 5, []byte("a"))
	db.ZAdd("zrank_test", 8, []byte("b"))

	rank, ok := db.ZRank("zrank_test", []byte("a"))
	if !ok || rank != 0 {
		t.Errorf("expected rank 0, got %d, ok=%v", rank, ok)
	}

	revRank, ok := db.ZRevRank("zrank_test", []byte("c"))
	if !ok || revRank != 0 {
		t.Errorf("expected rev rank 0, got %d, ok=%v", revRank, ok)
	}
}

func TestZLexCountLowCov(t *testing.T) {
	db := New()

	db.ZAdd("zlc_test", 0, []byte("a"))
	db.ZAdd("zlc_test", 0, []byte("b"))
	db.ZAdd("zlc_test", 0, []byte("c"))
	db.ZAdd("zlc_test", 0, []byte("d"))

	count := db.ZLexCount("zlc_test", "[a", "[c")
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestZScoreLowCov(t *testing.T) {
	db := New()

	db.ZAdd("zscore_test", 1.5, []byte("member1"))
	db.ZAdd("zscore_test", 2.5, []byte("member2"))

	score, ok := db.ZScore("zscore_test", []byte("member1"))
	if !ok || score != 1.5 {
		t.Errorf("expected 1.5, got %f, ok=%v", score, ok)
	}
}

func TestTTLNonexistent(t *testing.T) {
	db := New()

	ttl := db.TTL("nonexistent_key")
	if ttl != -1 {
		t.Errorf("expected -1 for nonexistent key, got %d", ttl)
	}
}

func TestLMoveLowCov(t *testing.T) {
	db := New()

	db.RPush("lmove_src", []byte("a"), []byte("b"), []byte("c"))

	result, ok := db.LMove("lmove_src", "lmove_dst", "RIGHT", "LEFT")
	if !ok || result == nil {
		t.Error("expected LMove to return element")
	}
	result.Close()
}

func TestPooledBufferMore(t *testing.T) {
	pb := Buf("hello")
	defer pb.Close()

	if pb.Len() != 5 {
		t.Errorf("expected len 5, got %d", pb.Len())
	}
	if string(pb.Bytes()) != "hello" {
		t.Error("expected 'hello'")
	}
}

func TestZSlicesMore(t *testing.T) {
	zs := NewZSlices()

	zs.Add([]byte("a"))
	zs.Add([]byte("b"))
	zs.Add([]byte("c"))

	zs.Finish()

	if zs.Len() != 3 {
		t.Errorf("expected len 3, got %d", zs.Len())
	}

	if string(zs.Get(0)) != "a" {
		t.Errorf("expected 'a', got '%s'", zs.Get(0))
	}
}

func TestExistsAPI(t *testing.T) {
	db := New()

	db.Set("exists_key", []byte("value"))

	if !db.Exists("exists_key") {
		t.Error("expected key to exist")
	}
	if db.Exists("nonexistent") {
		t.Error("expected key to not exist")
	}
}

func TestDelAPI(t *testing.T) {
	db := New()

	db.Set("del_key", []byte("value"))

	if !db.Del("del_key") {
		t.Error("expected delete to succeed")
	}
	if db.Exists("del_key") {
		t.Error("expected key to be deleted")
	}
	if db.Del("nonexistent") {
		t.Error("expected delete to fail for nonexistent key")
	}
}

func TestFlushAllAPI(t *testing.T) {
	db := New()

	db.Set("fa_key1", []byte("v1"))
	db.Set("fa_key2", []byte("v2"))

	db.FlushAll()

	if db.Exists("fa_key1") || db.Exists("fa_key2") {
		t.Error("expected all keys to be flushed")
	}
}

func TestIncrDecrAPI(t *testing.T) {
	db := New()

	v, err := db.IncrBy("incr_key", 1)
	if err != nil || v != 1 {
		t.Errorf("expected 1, got %d, err=%v", v, err)
	}

	v, err = db.IncrBy("incr_key", 5)
	if err != nil || v != 6 {
		t.Errorf("expected 6, got %d, err=%v", v, err)
	}

	v, err = db.DecrBy("incr_key", 2)
	if err != nil || v != 4 {
		t.Errorf("expected 4, got %d, err=%v", v, err)
	}
}

func TestZPopMinAPI(t *testing.T) {
	db := New()

	db.ZAdd("zpop_test", 1, []byte("a"))
	db.ZAdd("zpop_test", 2, []byte("b"))

	result := db.ZPopMin("zpop_test", 1)
	if result == nil || result.Len() == 0 {
		t.Error("expected popped members")
	}
}

func TestZPopMaxAPI(t *testing.T) {
	db := New()

	db.ZAdd("zpmax_test", 1, []byte("a"))
	db.ZAdd("zpmax_test", 5, []byte("b"))

	result := db.ZPopMax("zpmax_test", 1)
	if result == nil || result.Len() == 0 {
		t.Error("expected popped members")
	}
}

func TestGetDelAPI(t *testing.T) {
	db := New()

	db.Set("gdl_key", []byte("hello"))

	v, ok := db.GetDel("gdl_key")
	if !ok || string(v.Bytes()) != "hello" {
		t.Errorf("expected 'hello', got '%s'", v.Bytes())
	}
	if db.Exists("gdl_key") {
		t.Error("expected key to be deleted after GetDel")
	}
}

func TestSDiffAPI(t *testing.T) {
	db := New()

	db.SAdd("sd1", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("sd2", []byte("b"))

	result := db.SDiff("sd1", "sd2")
	if result == nil || result.Len() != 2 {
		t.Errorf("expected 2 members, got %d", result.Len())
	}
}

func TestSUnionAPI(t *testing.T) {
	db := New()

	db.SAdd("su1", []byte("a"), []byte("b"))
	db.SAdd("su2", []byte("c"), []byte("d"))

	result := db.SUnion("su1", "su2")
	if result == nil || result.Len() != 4 {
		t.Errorf("expected 4 members, got %d", result.Len())
	}
}

func TestSInterAPI(t *testing.T) {
	db := New()

	db.SAdd("si1", []byte("a"), []byte("b"), []byte("c"))
	db.SAdd("si2", []byte("b"), []byte("c"), []byte("d"))

	result := db.SInter("si1", "si2")
	if result == nil || result.Len() != 2 {
		t.Errorf("expected 2 members, got %d", result.Len())
	}
}

func TestSMembersAPI(t *testing.T) {
	db := New()

	db.SAdd("sm_test", []byte("a"), []byte("b"))

	result := db.SMembers("sm_test")
	if result == nil || result.Len() != 2 {
		t.Errorf("expected 2 members, got %d", result.Len())
	}
}

func TestSCardAPI(t *testing.T) {
	db := New()

	db.SAdd("sc_test", []byte("a"), []byte("b"), []byte("c"))

	if db.SCard("sc_test") != 3 {
		t.Errorf("expected 3, got %d", db.SCard("sc_test"))
	}
}

func TestGetExWithExpire(t *testing.T) {
	db := New()

	db.Set("getex_expire", []byte("value"))
	db.GetEx("getex_expire", []byte("new_value"), 1000)

	ttl := db.TTL("getex_expire")
	if ttl <= 0 {
		t.Errorf("expected positive TTL, got %d", ttl)
	}
}

func TestMGetMSet(t *testing.T) {
	db := New()

	db.MSet(map[string]*PooledBuffer{
		"key1": Buf("value1"),
		"key2": Buf("value2"),
	})

	v1, ok := db.Get("key1")
	if !ok || v1.String() != "value1" {
		t.Errorf("expected value1, got %s", v1.String())
	}
	v1.Close()

	v2, ok := db.Get("key2")
	if !ok || v2.String() != "value2" {
		t.Errorf("expected value2, got %s", v2.String())
	}
	v2.Close()
}

func TestSetExAPI(t *testing.T) {
	db := New()

	db.SetEx("setex_key", 1000, []byte("setex_value"))

	val, ok := db.Get("setex_key")
	if !ok || val.String() != "setex_value" {
		t.Errorf("expected setex_value, got %s", val.String())
	}
	val.Close()
}

func TestStrlenAPI(t *testing.T) {
	db := New()

	db.Set("strlen_key", []byte("hello"))

	if db.Strlen("strlen_key") != 5 {
		t.Errorf("expected 5, got %d", db.Strlen("strlen_key"))
	}
}

func TestAppendGetRange(t *testing.T) {
	db := New()

	db.Set("append_range", []byte("Hello"))

	db.Append("append_range", []byte(" World"))

	val, ok := db.GetRange("append_range", 0, 4)
	if !ok || val.String() != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", val.String())
	}
	val.Close()
}

func TestZCountZiplist(t *testing.T) {
	db := New()
	db.ZAdd("zc_ziplist", 1.0, []byte("a"))
	db.ZAdd("zc_ziplist", 2.0, []byte("b"))
	db.ZAdd("zc_ziplist", 3.0, []byte("c"))

	count := db.ZCount("zc_ziplist", 1.0, 2.5)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	count = db.ZCount("zc_ziplist", 0.5, 1.5)
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	count = db.ZCount("zc_ziplist", 3.0, 5.0)
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	count = db.ZCount("zc_ziplist", 10.0, 20.0)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestZCountSkiplist(t *testing.T) {
	db := New()
	for i := 1; i <= 20; i++ {
		db.ZAdd("zc_skiplist", float64(i), []byte(string(rune('a'+i-1))))
	}

	count := db.ZCount("zc_skiplist", 5.0, 10.0)
	if count != 6 {
		t.Errorf("expected 6, got %d", count)
	}

	count = db.ZCount("zc_skiplist", 1.0, 20.0)
	if count != 20 {
		t.Errorf("expected 20, got %d", count)
	}
}
