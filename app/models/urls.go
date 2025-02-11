package models

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/go-batteries/shortner/app/db"
)

type URL struct {
	ShortKey  string     `db:"short_key"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`
	Link      *string    `db:"url"`
	Malicious *int       `db:"malicious"`
}

func (u *URL) Hash() string {
	h := sha1.New()
	h.Write([]byte(*u.Link))
	return hex.EncodeToString(h.Sum(nil))
}

func (URL) TableName() string {
	return "urls"
}

func NewURLFromKey(shortKey string) *URL {
	now := time.Now().UTC()

	return &URL{
		ShortKey:  shortKey,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

type URLRepo struct {
	sharder db.Coordinator[string]
}

func NewURLRepo(sharder db.Coordinator[string]) *URLRepo {
	return &URLRepo{sharder: sharder}
}

const CreateBatchesQuery = `INSERT INTO urls (
	url
	,short_key
	,malicious
	,created_at
	,updated_at
) VALUES %s;
`

const FindURLByShortKey = `
	SELECT url
		,short_key
		,updated_at
	FROM urls
	WHERE short_key = ?
	AND (malicious IS NULL or malicious = 0)
	AND deleted_at IS NULL
	LIMIT 1
`

const DeleteEntryQuery = `UPDATE urls SET deleted_at = ? WHERE short_key = ?`

// const AssignKeyToURLQuery = `
// UPDATE urls SET url = ?, updated_at = ? WHERE short_key = (SELECT short_key FROM urls WHERE url IS NULL LIMIT 1);
// `
const SelectEmptyShortKey = `SELECT short_key FROM urls WHERE url IS NULL LIMIT 1;`
const AssignURLQuery = `UPDATE urls SET url = ?, updated_at = ? WHERE short_key = ? AND url IS NULL;`

// DeleteEntry, marks the entry as deleted by setting deleted_at
func (repo *URLRepo) Delete(ctx context.Context, shortKey string) error {
	db, err := repo.sharder.GetShard(shortKey)
	if err != nil {
		return err
	}

	log.Println("deleting", shortKey, "from shard", db.ShardKey())

	tx, err := db.Conn().BeginTx(ctx, nil)
	if err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, DeleteEntryQuery, time.Now().UTC(), shortKey)
	return err
}

func (repo *URLRepo) AssignURL(ctx context.Context, urlStr string) (*URL, error) {
	db, err := repo.sharder.GetShard("")
	if err != nil {
		return nil, err
	}

	tx, err := db.Conn().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	rows := tx.QueryRowContext(ctx, SelectEmptyShortKey)
	if rows.Err() != nil {
		tx.Rollback()
		return nil, err
	}

	var shortKey string

	if err := rows.Scan(&shortKey); err != nil {
		tx.Rollback()
		return nil, err
	}

	now := time.Now().UTC()

	_, err = tx.ExecContext(ctx, AssignURLQuery, url.QueryEscape(urlStr), now, shortKey)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return &URL{
		Link:      &urlStr,
		UpdatedAt: now,
		ShortKey:  shortKey,
	}, nil
}

// Find find an URL by shortKey
func (repo *URLRepo) Find(ctx context.Context, shortKey string) (*URL, error) {
	db, err := repo.sharder.GetShard(shortKey)
	if err != nil {
		return nil, err
	}

	rows := db.Conn().QueryRowContext(ctx, FindURLByShortKey, shortKey)
	if err := rows.Err(); err != nil {
		return nil, err
	}

	data := &URL{}

	err = rows.Scan(
		&data.Link,
		&data.ShortKey,
		&data.UpdatedAt,
	)

	return data, err
}

// CreateBatches creates a batch of records
// they are supposed to go to the same database
func (repo *URLRepo) CreateBatches(ctx context.Context, urls []*URL) error {
	var connQueryMap = map[db.Shard[string]][]*URL{}

	for _, u := range urls {
		db, err := repo.sharder.GetShard(u.ShortKey)
		if err != nil {
			log.Println("failed to get shard key for", u.ShortKey)
			return err
		}

		queries, ok := connQueryMap[db]
		if ok {
			queries = append(queries, u)
		} else {
			queries = []*URL{u}
		}

		connQueryMap[db] = queries
	}

	errChan := make(chan error, len(connQueryMap))

	for database, urlObjs := range connQueryMap {
		go func(d db.Shard[string]) {
			placeholders := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?),", len(urlObjs)), ",")
			query := fmt.Sprintf(CreateBatchesQuery, placeholders)
			values := []interface{}{}

			for _, u := range urlObjs {
				values = append(values, []interface{}{u.Link, u.ShortKey, u.Malicious, u.CreatedAt, u.UpdatedAt}...)
			}

			tx, err := d.Conn().BeginTx(ctx, nil)
			if err != nil {
				errChan <- err
				return

			}
			_, err = tx.ExecContext(ctx, query, values...)
			if err != nil {
				tx.Rollback()
				errChan <- err
				return
			}

			errChan <- tx.Commit()
			// log.Println("inserted", len(values)/5, database.ShardKey()) // for 5 columns
		}(database)
	}

	var errr error

	for i := 0; i < len(connQueryMap); i++ {
		select {
		case err := <-errChan:
			if err != nil {
				errr = fmt.Errorf("%v\n", err)
			}
		case <-time.After(1 * time.Second):
			log.Println("timeout")
			errr = fmt.Errorf("timeout\n")
		}
	}

	close(errChan)

	if errr != nil {
		log.Println("failed to write all records to db", errr)
		return errr
	}

	fmt.Printf("=")

	// log.Println("added", len(urls), "records to dbs")
	return nil
}
