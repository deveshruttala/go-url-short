package models

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/shortner/app/seed"
	"github.com/rs/zerolog/log"
)

type Prober struct {
	Name  string
	Query string
	conn  *sql.DB
}

func NewProber(name string, conn *sql.DB, query string) *Prober {
	return &Prober{
		Name:  name,
		conn:  conn,
		Query: query,
	}
}

/*
ANALYZE;
SELECT tbl AS table_name, CAST(substr(stat, 1, instr(stat, ',') - 1) AS INTEGER) AS estimated_rows
FROM sqlite_stat1
WHERE idx IS NULL AND tbl = 'users';
*/

// we have an index on this
const URLKeysProberQuery = `SELECT COUNT(1) AS empty_records FROM urls WHERE url IS NULL;`

type Stats struct {
	EmptyRecords int64  `json:"empty_records"`
	ShardKey     string `json:"-"`
	Error        error  `json:"error,omitempty"`
}

type FullResult map[string]interface{}

func (p *Prober) GetStatsFull(ctx context.Context, args ...any) ([]FullResult, error) {
	rows, err := p.conn.QueryContext(ctx, p.Query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var results = []FullResult{}

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		// We do this, to create a Box (interface), which will
		// hold the value and the type
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		rowMap := make(FullResult)

		for i, col := range columns {
			var v interface{}
			rawValue := values[i]

			b, ok := rawValue.([]byte)
			if ok {
				v = string(b)
			} else {
				v = rawValue
			}
			rowMap[col] = v
		}

		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, err
}

func (p *Prober) GetStats(ctx context.Context, args ...any) (*Stats, error) {
	row := p.conn.QueryRowContext(ctx, p.Query, args...)
	if err := row.Err(); err != nil {
		return nil, err
	}

	stats := &Stats{}

	err := row.Scan(&stats.EmptyRecords)
	return stats, err
}

type ShardProber struct {
	seeder *seed.Seeder
}

func NewShardedProber(seeder *seed.Seeder) *ShardProber {
	return &ShardProber{seeder: seeder}
}

func (p *ShardProber) GetStatsForKeyRange(ctx context.Context, keyRange string) ([]*Stats, error) {
	start, end, ok := ExplodeKeyRange(keyRange)
	if !ok {
		return nil, fmt.Errorf("invalid key range. expected format start-end. keyRange: %s", keyRange)
	}

	keyRanges := p.seeder.Shards(5)

	// this is an interval problem
	// given keyranges, a-e, f-p, q-s
	// and provided range b-g
	// find the intervals that intersect

	filteredRanges := []string{}

	for _, keyRange := range keyRanges {
		s, e, _ := ExplodeKeyRange(keyRange)

		startInRange := start >= s && start <= e
		endInRange := end >= s && end <= e

		if startInRange || endInRange {
			filteredRanges = append(filteredRanges, keyRange)
		}
	}

	database := db.NewSqliteCoordinator(filteredRanges)

	err := database.ConnectShards(ctx, db.DBReadOnlyMode)
	if err != nil {
		fmt.Errorf("failed to create databases")
	}

	shards, ok := database.GetShards()
	if !ok {
		return nil, fmt.Errorf("should not have failed to create shards")
	}

	statss := make(chan *Stats, len(shards))

	for _, shard := range shards {
		go func(s db.Shard[string]) {
			repo := NewProber(shard.ShardKey(), shard.Conn(), URLKeysProberQuery)
			stats, err := repo.GetStats(ctx)
			statss <- &Stats{EmptyRecords: stats.EmptyRecords, Error: err, ShardKey: shard.ShardKey()}
		}(shard)
	}

	validStats := []*Stats{}

	for stats := range statss {
		if stats.Error != nil {
			log.Error().Err(stats.Error).Str("shard", stats.ShardKey).Msg("failed to get status for shard")
		} else {
			validStats = append(validStats, stats)
		}
	}

	return validStats, nil
}
