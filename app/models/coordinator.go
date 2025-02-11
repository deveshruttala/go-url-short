package models

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

type ShardStatus struct {
	ShardID    string    `db:"shard_id"`
	ShardChar  string    `db:"shard_char"`
	Start      uint64    `db:"start"`
	End        uint64    `db:"end"`
	Generation int64     `db:"generation"`
	Status     string    `db:"status"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

const (
	StatusProcessing = "processing"
	StatusProcessed  = "processed"
	StatusFailed     = "failed"
)

const DefaultSeedStart uint64 = 1000000000

const (
	ShardStatusInsertCreateQuery = `INSERT INTO shard_status (shard_id, shard_char, start, end, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	ShardStatusSelectQuery       = `SELECT shard_id, shard_char, start, end, status, generation, updated_at FROM shard_status WHERE shard_id = ? AND shard_char = ?`
	ShardStatusUpdateStatusQuery = `UPDATE shard_status SET end = ?, updated_at = ?, generation = generation + 1, status = ? WHERE shard_id = ? AND shard_char = ? AND generation = ? AND status = ?`
)

func ExplodeKeyRange(shardID string) (byte, byte, bool) {
	splits := strings.Split(shardID, "-")
	var noop byte

	if len(splits) < 2 {
		return noop, noop, false
	}

	return splits[0][0], splits[1][0], true
}

type ShardStatusRepo struct {
	db *sql.DB
}

func NewShardStatusRepo(db *sql.DB) *ShardStatusRepo {
	return &ShardStatusRepo{db: db}
}

func (repo *ShardStatusRepo) GetLastState(ctx context.Context, shardID, shardChar string) (*ShardStatus, error) {
	res := repo.db.QueryRowContext(ctx, ShardStatusSelectQuery, shardID, shardChar)
	if err := res.Err(); err != nil {
		return nil, err
	}

	shardStatus := &ShardStatus{}

	err := res.Scan(
		&shardStatus.ShardID,
		&shardStatus.ShardChar,
		&shardStatus.Start,
		&shardStatus.End,
		&shardStatus.Generation,
		&shardStatus.UpdatedAt,
	)

	return shardStatus, err
}

func (repo *ShardStatusRepo) UpdateState(ctx context.Context, status *ShardStatus) error {
	log.Println("updating shard generation info", status.ShardID, status.ShardChar)

	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		ShardStatusUpdateStatusQuery,
		status.End,
		time.Now().UTC(),
		status.Status,
		status.ShardID,
		status.ShardChar,
		status.Generation,
		status.Status,
	)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (repo *ShardStatusRepo) Create(ctx context.Context, status *ShardStatus) error {
	log.Println("creating shard status entries")

	now := time.Now().UTC()

	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		ShardStatusInsertCreateQuery,
		status.ShardID,
		status.ShardChar,
		status.Start,
		status.End,
		status.Status,
		now,
		now,
	)

	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
