package db_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/shortner/app/seed"
	"github.com/go-batteries/slicendice"
)

func Cleanup(database *db.SqliteCoordinator[string]) {
	database.DeInit()

	shards, ok := database.GetShards()
	if !ok {
		log.Fatal("should have found shards")
	}

	for _, shard := range shards {
		os.Remove(fmt.Sprintf("%s.db", shard.ID()))
	}
}

func Test_SqliteCoordinator(t *testing.T) {
	ctx := context.Background()
	seeder := seed.RegisterUrlSeeder()
	keyRanges := seeder.Shards(5)

	database := db.NewSqliteCoordinator(keyRanges)

	err := database.RegisterShards(ctx)
	if err != nil {
		t.Fatalf("failed to create databases")
	}

	shards, ok := database.GetShards()
	if !ok {
		t.Fatal("should not have failed to get shards")
	}

	var shardMapper = slicendice.Reduce(
		keyRanges,
		func(acc map[string]db.Shard[string], keyRange string, _ int) map[string]db.Shard[string] {
			for _, shard := range shards {
				if shard.ShardKey() == keyRange {
					acc[keyRange] = shard
				}
			}
			return acc
		},
		map[string]db.Shard[string]{},
	)

	database.SetPolicy(&db.KeyBasedPolicy[string]{Shards: shardMapper})

	defer Cleanup(database)

	if len(shards) != len(keyRanges) {
		t.Fatalf("number of shards should have matched. got %d, expected %d", len(shards), len(keyRanges))
	}

	key := "a****"
	shard, err := database.GetShard(key)
	if err != nil {
		t.Fatalf("should not have failed here %v", err)
	}

	if shard.ShardKey() != "a-e" {
		t.Fatalf("invalid shard key. expected %s, got %s", "a-e", shard.ShardKey())
	}

	key = "c***"
	shard, err = database.GetShard(key)
	if err != nil {
		t.Fatalf("should not have failed here %v", err)
	}

	if shard.ShardKey() != "a-e" {
		t.Fatalf("invalid shard key. expected %s, got %s", "a-e", shard.ShardKey())
	}

	key = "o***"
	shard, err = database.GetShard(key)
	if err != nil {
		t.Fatalf("should have failed here %v", err)
	}

	if shard.ShardKey() != "k-p" {
		t.Fatal("expected shard keyrange 'k-p', got", shard.ShardKey())
	}
}
