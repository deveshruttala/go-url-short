package runners

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/shortner/app/models"
	"github.com/go-batteries/shortner/app/seed"
	"github.com/go-batteries/slicendice"
	"github.com/rs/zerolog/log"
)

// TODO: this should be config as well
var startMaps = map[byte]uint64{
	'a': uint64(1000000000),
	'f': uint64(2000000000),
	'k': uint64(3000000000),
	'q': uint64(4000000000),
	'v': uint64(5000000000),
}

var getShardStart = func(startKey byte) uint64 {
	if startKey >= 'a' && startKey < 'f' {
		return startMaps['a']
	}

	if startKey >= 'f' && startKey < 'k' {
		return startMaps['f']
	}

	if startKey >= 'k' && startKey < 'q' {
		return startMaps['k']
	}

	if startKey >= 'q' && startKey < 'v' {
		return startMaps['q']
	}

	return startMaps['v']
}

type result struct {
	err        error
	keyRange   string
	prefixes   []byte
	shardStats *models.ShardStatus
}

func SeedSqliteDB(ctx context.Context, shortKeyLen int, batchSize int, seedSize uint64) (errr error) {
	seeder := seed.RegisterUrlSeeder()
	keyRanges := seeder.Shards(5)

	database := db.NewSqliteCoordinator(keyRanges)

	err := database.RegisterShards(ctx)
	if err != nil {
		log.Fatal().Msg("failed to create databases")
	}

	defer database.DeInit()

	lowers := seeder.Lowers()
	total := big.NewInt(0).Mul(big.NewInt(int64(seedSize)), big.NewInt(int64(len(lowers))))

	log.Info().Str("n_keys", total.String()).Msg("generating url keys")

	// TODO: handle shortKeyLen to generate the start size
	cdb, err := database.RegisterCoordinator(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create coordinator db")
	}
	defer cdb.Close()

	shards, ok := database.GetShards()
	if !ok {
		log.Fatal().Msg("should not have failed to create shards")
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

	repo := models.NewURLRepo(database)

	// Create one worker for each range in
	// seeder.GetShards

	// Each worker has a job channel
	// on which it receives the next set of
	// data, and inserts it in the database.
	// Ideally it should also update the state
	// in the shard_status db. (for laters).
	// Even with sqlite threads, sqlite writes
	// one by one. Its writing which is the problem
	// I think, we need a sqlite like database
	// supporting more concurrency.
	// Maybe an LSM tree.

	// Each keyrange will be responsible for
	// sequentially generating.
	// The other approach would be to
	// create one database for each and then
	// load database in sqlite for each key range.

	resultsChan := make(chan result, len(keyRanges))

	for _, keyrange := range keyRanges {
		log.Info().Msgf("generating keys for shard %s", keyrange)

		go func(keyRange string) {
			res := result{keyRange: keyRange}

			start, end, ok := models.ExplodeKeyRange(keyRange)
			if !ok {
				res.err = fmt.Errorf("invalid key range %s", keyRange)
				resultsChan <- res
				return
			}

			res.err = GenerateForKeyRange(ctx, start, end, lowers, batchSize, seedSize, repo)

			for ch := start; ch <= end; ch++ {
				res.prefixes = append(res.prefixes, ch)
			}

			resultsChan <- res
		}(keyrange)

	}

	cordDB := models.NewShardStatusRepo(database.CoordinatorDB)

	for i := 0; i < len(keyRanges); i++ {
		res := <-resultsChan

		if res.err != nil {
			log.Error().Err(err).Msg("failed to generate for key-range")
			errr = err
		}

		prefixes := res.prefixes
		shardID := res.keyRange

		for _, prefix := range prefixes {
			start := getShardStart(prefix)
			err := cordDB.Create(ctx, &models.ShardStatus{
				ShardID:   shardID,
				ShardChar: string(prefix),
				Status:    models.StatusProcessed,
				Start:     uint64(start),
				End:       uint64(start + seedSize),
			})
			if err != nil {
				log.Error().Err(err).Str("shard", shardID).Str("shardChar", string(prefix)).Msg("failed to sync to coordinator db")
			}
		}
	}

	close(resultsChan)

	return errr
}

func GenerateForKeyRange(ctx context.Context, keyStart, keyEnd byte, lowers []string, batchSize int, seedSize uint64, repo *models.URLRepo) error {
	batchShard := []byte{}
	for _, lower := range lowers {
		if lower[0] >= byte(keyStart) && lower[0] <= byte(keyEnd) {
			batchShard = append(batchShard, lower[0])
		}
	}

	sinchan := make(chan []string, 1)
	defer close(sinchan)

	for _, shardKey := range batchShard {
		log.Info().Msg("") // to add a new line
		log.Info().Str("shardKey", string(shardKey)).Msg("inserting records for shardkey")

		last := getShardStart(shardKey)
		totalCount := last + seedSize

		generator := seed.NewBase58Generator(last, seedSize, string(shardKey))
		resultChan := generator.NextBatch(ctx, totalCount, uint64(batchSize))

		now := time.Now().UTC()

		for i := 0; i < int(seedSize/uint64(batchSize)); i++ {
			log.Debug().Msg("sending back the channel")
			resultChan <- sinchan

			log.Debug().Msg("waiting for data")
			urls := slicendice.Map(<-sinchan, func(shorKey string, _ int) *models.URL {
				return &models.URL{ShortKey: shorKey, CreatedAt: now}
			})

			log.Debug().Msgf("creating urls for %s", string(shardKey))
			err := repo.CreateBatches(ctx, urls)
			if err != nil {
				log.Error().Err(err).Str("shardKey", string(shardKey)).Msg("failed to create urls")
				return err
			}
		}
	}

	return nil
}
