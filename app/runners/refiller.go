package runners

import (
	"context"
	"fmt"
	"time"

	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/shortner/app/models"
	"github.com/go-batteries/shortner/app/seed"
	"github.com/go-batteries/slicendice"
	"github.com/rs/zerolog/log"
)

const FillThreshold = 10000

func RefillKeys(ctx context.Context, seeder *seed.Seeder, batchSize int, seedSize uint64) error {
	keyRanges := seeder.Shards(5)
	lowers := seeder.Lowers()

	database := db.NewSqliteCoordinator(keyRanges)

	err := database.ConnectShards(ctx, db.DBReadWriteMode)
	if err != nil {
		log.Fatal().Msg("failed to create databases")
	}

	defer database.DeInit()

	cdb, err := database.ConnectCoordinatorDB(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create coordinator db")
	}
	defer cdb.Close()

	shards, ok := database.GetShards()
	if !ok {
		log.Fatal().Msg("should not have failed to create shards")
	}

	database.SetPolicy(&db.RoundRobinPolicy[string]{Shards: shards})
	repo := models.NewURLRepo(database)

	resultsChan := make(chan result, len(keyRanges))

	cordDB := models.NewShardStatusRepo(database.CoordinatorDB)

	for _, shard := range shards {
		go func(shard db.Shard[string]) {
			keyRange := shard.ShardKey()

			res := result{keyRange: keyRange}

			prober := models.NewProber(keyRange, shard.Conn(), models.URLKeysProberQuery)
			stats, err := prober.GetStats(ctx)
			if err != nil {
				res.err = fmt.Errorf("failed to get count error: %v", keyRange, err)
				resultsChan <- res
				return
			}

			if stats.EmptyRecords < FillThreshold {
				start, end, ok := models.ExplodeKeyRange(keyRange)
				if !ok {
					res.err = fmt.Errorf("invalid key range %s", keyRange)
					resultsChan <- res
					return
				}

				stats, err := cordDB.GetLastState(ctx, keyRange, string(start))
				if err != nil {
					res.err = fmt.Errorf("failed to get last state. %v", err)
					resultsChan <- res
					return
				}

				// Ideally we should be setting the status to
				// processing. but fuckit, this is run non-concurrently

				res.shardStats = stats
				res.err = GenerateKeyRangeFrom(
					ctx,
					start, end,
					lowers,
					stats.End,
					batchSize, seedSize,
					repo,
				)
				for ch := start; ch <= end; ch++ {
					res.prefixes = append(res.prefixes, ch)
				}

				resultsChan <- res
			}
		}(shard)
	}

	var errr error

	for i := 0; i < len(keyRanges); i++ {
		res := <-resultsChan

		if res.err != nil {
			log.Error().Err(err).Msg("failed to generate for key-range")
			errr = err
		}

		// Ideally we should set the status to failed
		// for specific errors

		prefixes := res.prefixes
		shardID := res.keyRange

		for _, prefix := range prefixes {
			err := cordDB.Create(ctx, &models.ShardStatus{
				ShardID:   shardID,
				ShardChar: string(prefix),
				Status:    models.StatusProcessed,
				Start:     res.shardStats.End,
				End:       res.shardStats.End + seedSize,
			})
			if err != nil {
				log.Error().Err(err).
					Str("shard", shardID).
					Str("shardChar", string(prefix)).
					Msg("failed to sync to coordinator db")
			}
		}
	}

	close(resultsChan)

	return errr
}

func GenerateKeyRangeFrom(
	ctx context.Context,
	keyStart byte,
	keyEnd byte,
	lowers []string,
	last uint64,
	batchSize int,
	seedSize uint64,
	repo *models.URLRepo,
) error {

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
