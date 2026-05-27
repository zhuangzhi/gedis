package main

import (
	"fmt"
	"os"
	"strconv"

	"gedis"
)

func main() {
	db := gedis.New()

	fmt.Println("=== Strings ===")
	{
		alice := gedis.Buf("Alice")
		db.Set("user:1:name", alice)
		alice.Close()

		score100 := gedis.Buf("100")
		db.Set("user:1:score", score100)
		score100.Close()

		val, _ := db.Get("user:1:name")
		fmt.Printf("  GET user:1:name = %s\n", val.String())
		val.Close()

		db.IncrBy("user:1:score", 50)
		score, _ := db.Get("user:1:score")
		fmt.Printf("  INCRBY score +50 = %s\n", score.String())
		score.Close()

		smith := gedis.Buf(" Smith")
		db.Append("user:1:name", smith)
		smith.Close()
		val, _ = db.Get("user:1:name")
		fmt.Printf("  APPEND name = %s\n", val.String())
		val.Close()

		sub, _ := db.GetRange("user:1:name", 6, 10)
		fmt.Printf("  GETRANGE 6..10 = %s\n", sub.String())
		sub.Close()
	}

	fmt.Println("\n=== Lists ===")
	{
		db.LPush("queue",
			gedis.Buf("first"), gedis.Buf("second"), gedis.Buf("third"),
		)
		fmt.Printf("  LLEN = %d\n", db.LLen("queue"))

		item, _ := db.RPop("queue")
		fmt.Printf("  RPOP = %s\n", item.String())
		item.Close()
		item, _ = db.LPop("queue")
		fmt.Printf("  LPOP = %s\n", item.String())
		item.Close()

		item, _ = db.LIndex("queue", 0)
		fmt.Printf("  LINDEX 0 = %s\n", item.String())
		item.Close()
	}

	fmt.Println("\n=== Hashes ===")
	{
		db.HSet("user:1", "name", gedis.Buf("Alice"))
		db.HSet("user:1", "email", gedis.Buf("alice@example.com"))
		db.HSet("user:1", "age", gedis.Buf("30"))

		val, _ := db.HGet("user:1", "email")
		fmt.Printf("  HGET email = %s\n", val.String())
		val.Close()
		fmt.Printf("  HLEN = %d\n", db.HLen("user:1"))
		fmt.Printf("  HEXISTS name = %v\n", db.HExists("user:1", "name"))

		db.HIncrBy("user:1", "age", 1)
		val, _ = db.HGet("user:1", "age")
		fmt.Printf("  HINCRBY age = %s\n", val.String())
		val.Close()

		all := db.HGetAll("user:1")
		for k, v := range all {
			fmt.Printf("    %s: %s\n", k, v.String())
		}
	}

	fmt.Println("\n=== Sets ===")
	{
		db.SAdd("tags", gedis.Buf("go"), gedis.Buf("redis"), gedis.Buf("database"))
		fmt.Printf("  SCARD = %d\n", db.SCard("tags"))
		fmt.Printf("  SISMEMBER go = %v\n", db.SIsMember("tags", gedis.Buf("go")))

		db.SAdd("tags2", gedis.Buf("go"), gedis.Buf("python"), gedis.Buf("memory"))
		inter := db.SInter("tags", "tags2")
		fmt.Printf("  SINTER = %d members\n", len(inter))
		for _, m := range inter {
			fmt.Printf("    %s\n", m.String())
		}
	}

	fmt.Println("\n=== Sorted Sets ===")
	{
		db.ZAdd("leaderboard", 1000, gedis.Buf("Alice"))
		db.ZAdd("leaderboard", 850, gedis.Buf("Bob"))
		db.ZAdd("leaderboard", 950, gedis.Buf("Charlie"))

		score, _ := db.ZScore("leaderboard", gedis.Buf("Alice"))
		fmt.Printf("  ZSCORE Alice = %.0f\n", score)
		fmt.Printf("  ZCARD = %d\n", db.ZCard("leaderboard"))

		fmt.Println("  ZRANGE 0..-1:")
		members := db.ZRange("leaderboard", 0, -1)
		for i := 0; i < members.Len(); i++ {
			fmt.Printf("    %s\n", string(members.Get(i)))
		}

		fmt.Println("  ZRANGEITER (zero-alloc):")
		db.ZRangeIter("leaderboard", 0, -1, func(member []byte) {
			fmt.Printf("    %s\n", string(member))
		})

		fmt.Println("  ZRANGEWITHSCORES:")
		names, scores := db.ZRangeWithScores("leaderboard", 0, -1)
		for i, name := range names {
			fmt.Printf("    %s: %.0f\n", name, scores[i])
		}

		db.ZRem("leaderboard", gedis.Buf("Bob"))
		fmt.Printf("  ZREM Bob -> ZCARD = %d\n", db.ZCard("leaderboard"))
	}

	fmt.Println("\n=== HyperLogLog ===")
	{
		db.PFAdd("visitors",
			gedis.Buf("user1"), gedis.Buf("user2"), gedis.Buf("user3"),
			gedis.Buf("user4"), gedis.Buf("user5"), gedis.Buf("user1"),
		)
		count := db.PFCount("visitors")
		fmt.Printf("  PFCOUNT visitors ~= %d (actual: 5)\n", count)
	}

	fmt.Println("\n=== Bitmaps ===")
	{
		db.SetBit("online", 0, 1)
		db.SetBit("online", 3, 1)
		db.SetBit("online", 7, 1)
		fmt.Printf("  GETBIT 3 = %d\n", db.GetBit("online", 3))
		fmt.Printf("  BITCOUNT = %d\n", db.BitCount("online", 0, -1))
	}

	fmt.Println("\n=== Probabilistic - Bloom Filter ===")
	{
		db.BFReserve("bf", 0.01, 100000)
		db.BFAdd("bf", gedis.Buf("apple"))
		db.BFAdd("bf", gedis.Buf("banana"))
		db.BFAdd("bf", gedis.Buf("cherry"))
		fmt.Printf("  BF.EXISTS apple = %v\n", db.BFExists("bf", gedis.Buf("apple")))
		fmt.Printf("  BF.EXISTS grape = %v\n", db.BFExists("bf", gedis.Buf("grape")))
	}

	fmt.Println("\n=== Probabilistic - Cuckoo Filter ===")
	{
		db.CFReserve("cf", 1000)
		db.CFAdd("cf", gedis.Buf("go"))
		db.CFAdd("cf", gedis.Buf("rust"))
		fmt.Printf("  CF.EXISTS go = %v\n", db.CFExists("cf", gedis.Buf("go")))
		db.CFDel("cf", gedis.Buf("go"))
		fmt.Printf("  CF.DEL go -> CF.EXISTS go = %v\n", db.CFExists("cf", gedis.Buf("go")))
	}

	fmt.Println("\n=== Probabilistic - Count-Min Sketch ===")
	{
		db.CMSInitByDim("cms", 2000, 10)
		db.CMSIncrBy("cms", gedis.Buf("item_a"), 3)
		db.CMSIncrBy("cms", gedis.Buf("item_b"), 5)
		qs := db.CMSQuery("cms", gedis.Buf("item_a"), gedis.Buf("item_b"), gedis.Buf("item_c"))
		fmt.Printf("  CMS.QUERY item_a = %d\n", qs[0])
		fmt.Printf("  CMS.QUERY item_b = %d\n", qs[1])
		fmt.Printf("  CMS.QUERY item_c = %d\n", qs[2])
	}

	fmt.Println("\n=== Probabilistic - Top-K ===")
	{
		db.TopKReserve("topk", 3)
		db.TopKAdd("topk", "python", "go", "typescript", "go", "rust", "go", "typescript", "go")
		items := db.TopKList("topk")
		for _, item := range items {
			fmt.Printf("  %s: %d\n", item.Item, item.Count)
		}
	}

	fmt.Println("\n=== Rate Limiting (Cell) ===")
	{
		r := db.Throttle("api:login", 10, 5, 1000)
		fmt.Printf("  Throttle allowed    = %v\n", r.Allowed)
		fmt.Printf("  Throttle remaining  = %d\n", r.Remaining)

		r2 := db.Throttle("api:login", 10, 0, 1000)
		fmt.Printf("  Throttle(rate=0) allowed = %v\n", r2.Allowed)
	}

	fmt.Println("\n=== TimeSeries ===")
	{
		db.TSAdd("cpu:usage", 1000, 45.2)
		db.TSAdd("cpu:usage", 1005, 52.1)
		db.TSAdd("cpu:usage", 1010, 48.7)
		db.TSAdd("cpu:usage", 1015, 61.3)

		ts, val, _ := db.TSLast("cpu:usage")
		fmt.Printf("  TS.LAST = (%d, %.1f)\n", ts, val)

		points := db.TSRange("cpu:usage", 1000, 1010)
		for _, p := range points {
			fmt.Printf("    ts=%d, val=%.1f\n", p.Timestamp, p.Value)
		}
	}

	fmt.Println("\n=== Geo ===")
	{
		db.GeoAdd("cities", 116.397, 39.908, "Beijing")
		db.GeoAdd("cities", 121.473, 31.230, "Shanghai")
		db.GeoAdd("cities", 113.264, 23.129, "Guangzhou")

		dist := db.GeoDist("cities", "Beijing", "Shanghai", "km")
		fmt.Printf("  GEODIST Beijing-Shanghai = %.0f km\n", dist)

		nearby := db.GeoRadius("cities", 121.473, 31.230, 500, "km")
		fmt.Printf("  GEORADIUS Shanghai 500km: %v\n", nearby)
	}

	fmt.Println("\n=== Streams ===")
	{
		db.XGroupCreate("mystream", "mygroup", "0")
		db.XAdd("mystream", "*", map[string]*gedis.PooledBuffer{
			"user":   gedis.Buf("alice"),
			"action": gedis.Buf("login"),
		})
		db.XAdd("mystream", "*", map[string]*gedis.PooledBuffer{
			"user":   gedis.Buf("bob"),
			"action": gedis.Buf("purchase"),
		})
		fmt.Printf("  XLEN = %d\n", db.XLen("mystream"))
	}

	fmt.Println("\n=== JSON ===")
	{
		db.JsonSet("doc:1", ".", map[string]any{
			"name":  "Alice",
			"tags":  []any{"go", "redis"},
			"score": 100,
		})
		val, _ := db.JsonGet("doc:1", ".name")
		fmt.Printf("  JSON.GET .name = %v\n", val)

		db.JsonArrAppend("doc:1", ".tags", "database")
		val, _ = db.JsonGet("doc:1", ".tags")
		fmt.Printf("  JSON.GET .tags = %v\n", val)
	}

	fmt.Println("\n=== Search ===")
	{
		db.FTCreate("idx:users", map[string]string{
			"name": "text",
			"role": "tag",
		})
		db.FTAdd("idx:users", "u1", map[string]string{
			"name": "Alice Johnson",
			"role": "admin",
		})
		db.FTAdd("idx:users", "u2", map[string]string{
			"name": "Bob Smith",
			"role": "user",
		})
		results := db.FTSearch("idx:users", "alice", 10)
		fmt.Printf("  FT.SEARCH alice: %v\n", results)
	}

	fmt.Println("\n=== Graph ===")
	{
		db.GraphQuery("social", "CREATE (a:User {name:'Alice'})")
		db.GraphQuery("social", "CREATE (b:User {name:'Bob'})")
		db.GraphQuery("social", "MATCH (a:User {name:'Alice'}), (b:User {name:'Bob'}) CREATE (a)-[r:Follows]->(b)")
		results, _ := db.GraphQuery("social", "MATCH (a:User)-[r:Follows]->(b:User) RETURN a.name, b.name")
		for _, r := range results {
			for _, n := range r.Nodes {
				fmt.Printf("  Node: %v\n", n.Properties)
			}
		}
	}

	fmt.Println("\n=== Key Operations ===")
	{
		fmt.Printf("  EXISTS user:1 = %v\n", db.Exists("user:1"))
		db.Del("user:1")
		fmt.Printf("  DEL user:1 -> EXISTS user:1 = %v\n", db.Exists("user:1"))
	}

	fmt.Println()

	_ = strconv.Itoa
	os.Exit(0)
}
