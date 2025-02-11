package seed

import (
	"context"
	"fmt"

	"math/big"

	"math/rand"

	"github.com/mr-tron/base58"
	"github.com/rs/zerolog/log"
)

type Generator interface {
	NextBatch(ctx context.Context, start, end uint64) <-chan []string
}

type Base58Generator struct {
	last, offset uint64
	prefix       string
}

func NewBase58Generator(last, offset uint64, prefix string) *Base58Generator {
	return &Base58Generator{last: last, offset: offset, prefix: prefix}
}

func (bg *Base58Generator) NextBatch(ctx context.Context, totalCount, batchSize uint64) chan chan []string {
	resultChan := make(chan chan []string, 1)
	lastIdx := bg.last

	// totalCount = bg.last + bg.offset

	go func() {
		defer close(resultChan)

		for {
			select {
			case <-ctx.Done():
				return
			case job := <-resultChan:
				log.Debug().Str("prefix", bg.prefix).Msg("received channel")

				batch := []string{}
				nextLastIdx := lastIdx + batchSize

				for i := lastIdx; i < nextLastIdx; i++ {
					result := base58.Encode(
						big.NewInt(0).Add(
							big.NewInt(int64(bg.last)),
							big.NewInt(int64(i)),
						).Bytes(),
					)

					batch = append(batch, fmt.Sprintf("%s%s", bg.prefix, result))
				}

				lastIdx = nextLastIdx

				if len(batch) > 0 {
					log.Debug().Str("prefix", bg.prefix).Msgf("sending remaining batch %d", len(batch))
					job <- Shuffle(batch, int(len(batch)/3))
				}

				if lastIdx == totalCount {
					log.Info().Int("lasIdx", int(lastIdx)).Int("totalCount", int(totalCount)).Msg("completed")
					return
				}

			}
		}
	}()

	return resultChan
}

func Shuffle[E any](stuff []E, times int) []E {
	for i := 0; i < times; i++ {
		rand.Shuffle(len(stuff), func(i, j int) {
			stuff[i], stuff[j] = stuff[j], stuff[i]
		})
	}

	return stuff
}

type PermuteGenerator struct {
	seeder     *Seeder
	totalCount uint64
}

func NewPermuteGenerator(seeder *Seeder) *PermuteGenerator {
	return &PermuteGenerator{seeder: seeder}
}

// NextBatch generates all keys of a given length
func (pg *PermuteGenerator) NextBatch(ctx context.Context, length uint64, batchSize uint64) <-chan []string {
	seeder := pg.seeder

	resultChan := make(chan []string, 100)
	alphas := append(seeder.lowers, seeder.uppers...)

	go func() {
		defer close(resultChan)

		var chars = append(alphas, seeder.nums...)
		totalCombinations := len(alphas) * pow(len(chars), int(length-1))

		batch := []string{}
		for i := 0; i < totalCombinations; i++ {
			key := generateKey(alphas, chars, i, int(length))
			batch = append(batch, key)

			if len(batch) == int(batchSize) {
				resultChan <- batch
				batch = []string{}
			}
		}

		if len(batch) > 0 {
			resultChan <- batch
		}
	}()

	return resultChan

}

// Generate a single key based on its index
func generateKey(alphas, chars []string, index int, length int) string {
	key := ""
	charSetSize := len(chars)
	alphaSize := len(alphas)

	// First character is from alphas
	key += alphas[index%alphaSize]
	index /= alphaSize

	// Remaining characters
	for i := 1; i < length; i++ {
		key += chars[index%charSetSize]
		index /= charSetSize
	}

	return key
}

// Efficient power function
func pow(base, exp int) int {
	result := 1
	for exp > 0 {
		result *= base
		exp--
	}
	return result
}
