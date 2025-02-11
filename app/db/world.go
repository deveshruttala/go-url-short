package db

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

type Shard[E cmp.Ordered] interface {
	ID() string
	Conn() *sql.DB
	ShardKey() E
}

type ShardingPolicy[E cmp.Ordered] interface {
	RoutedShard(shardKey string) (Shard[E], error)
}

// Sharding Policies
type KeyBasedPolicy[E cmp.Ordered] struct {
	Shards map[string]Shard[E]
}

func (p *KeyBasedPolicy[E]) RoutedShard(shardKey string) (Shard[E], error) {
	firstChar := unicode.ToLower(rune(shardKey[0]))

	for keyRange, db := range p.Shards {
		splits := strings.Split(keyRange, "-")
		start, end := rune(splits[0][0]), rune(splits[1][0])

		if firstChar >= start && firstChar <= end {
			return db, nil
		}
	}

	return nil, errors.New("not_found")
}

type HashBasedPolicy[E cmp.Ordered] struct {
	Shards []Shard[E]
}

func (p *HashBasedPolicy[E]) RoutedShard(shardKey string) (Shard[E], error) {
	hash := fnv.New32a()
	hash.Write([]byte(shardKey))

	shardIndex := int(hash.Sum32()) % len(p.Shards)
	return p.Shards[shardIndex], nil
}

type RoundRobinPolicy[E cmp.Ordered] struct {
	Shards []Shard[E]
	mu     sync.Mutex
	index  int
}

func (p *RoundRobinPolicy[E]) RoutedShard(shardKey string) (Shard[E], error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	shard := p.Shards[p.index]
	p.index = (p.index + 1) % len(p.Shards)

	return shard, nil
}

type Router[E cmp.Ordered] interface {
	AddShard(shard Shard[E])
	SetPolicy(policy ShardingPolicy[E])
	GetShard(key E) (Shard[E], error)
	GetShards() ([]Shard[E], bool)
}

type DBShard[E cmp.Ordered] struct {
	id       string
	conn     *sql.DB
	shardKey E
}

func (shard *DBShard[E]) ID() string {
	return shard.id
}

func (shard *DBShard[E]) Conn() *sql.DB {
	return shard.conn
}

func (shard *DBShard[E]) ShardKey() E {
	return shard.shardKey
}

type DBRouter[E ~string] struct {
	shards map[string]Shard[E]
	policy ShardingPolicy[E]
}

func (r *DBRouter[E]) AddShard(shard Shard[E]) {
	if r.shards == nil {
		r.shards = make(map[string]Shard[E])
	}
	r.shards[shard.ID()] = shard
}

func (r *DBRouter[E]) SetPolicy(policy ShardingPolicy[E]) {
	r.policy = policy
}

func (r *DBRouter[E]) GetShard(key E) (Shard[E], error) {
	shardKey := string(key)
	return r.policy.RoutedShard(shardKey)
}

func (r *DBRouter[E]) GetShards() ([]Shard[E], bool) {
	if len(r.shards) == 0 {
		return nil, false
	}

	shards := []Shard[E]{}

	for _, shard := range r.shards {
		shards = append(shards, shard)
	}

	return shards, true
}

type Coordinator[E cmp.Ordered] interface {
	DeInit()
	UpdateDbNameDeriver(func(E) string) error
	GetShards() ([]Shard[E], bool)
	GetShard(E) (Shard[E], error)
	RegisterShards(context.Context) error
}

type SqliteCoordinator[E cmp.Ordered] struct {
	ToDbName  func(keyRange E) string
	router    Router[E]
	policy    ShardingPolicy[E]
	keyRanges []E

	CoordinatorDB *sql.DB
}

func DefaultSqliteDBNameBuilder[E ~string](keyRange E) string {
	return fmt.Sprintf("db_%s", strings.ReplaceAll(string(keyRange), "-", "_"))
}

func NewSqliteCoordinator[E ~string](keyRanges []E) *SqliteCoordinator[E] {
	return &SqliteCoordinator[E]{
		ToDbName:  DefaultSqliteDBNameBuilder[E],
		router:    &DBRouter[E]{},
		keyRanges: keyRanges,
	}
}

func (ss *SqliteCoordinator[E]) SetPolicy(policy ShardingPolicy[E]) {
	ss.router.SetPolicy(policy)
}

// WithOpts to override the ToDbName
// Not concurrent safe
func (ss *SqliteCoordinator[E]) UpdateDbNameDeriver(fn func(E) string) error {
	ss.ToDbName = fn
	return ss.RegisterShards(context.Background())
}

func (ss *SqliteCoordinator[E]) DeInit() {
	shards, ok := ss.router.GetShards()
	if !ok {
		return
	}

	for _, shard := range shards {
		shard.Conn().Close()
	}
}

func (ss *SqliteCoordinator[E]) GetShards() ([]Shard[E], bool) {
	return ss.router.GetShards()
}

func (ss *SqliteCoordinator[E]) GetShard(key E) (Shard[E], error) {
	return ss.router.GetShard(key)
}

func (ss *SqliteCoordinator[E]) RegisterCoordinator(cx context.Context) (*sql.DB, error) {
	// Create shard status table
	if ss.CoordinatorDB == nil {
		ss.ConnectCoordinatorDB(cx)
	}

	_, err := ss.CoordinatorDB.ExecContext(cx, CREATE_SHARD_STATUS_QUERY)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables")
	}

	return ss.CoordinatorDB, nil
}

func (ss *SqliteCoordinator[E]) ConnectCoordinatorDB(ctx context.Context) (*sql.DB, error) {
	conn, err := sql.Open("sqlite3", "db_shard_coordinator.db")
	if err != nil {
		return nil, fmt.Errorf("failed to create coordinator db")
	}

	ss.CoordinatorDB = conn
	return conn, nil
}

type DBmode string

const (
	DBReadWriteMode DBmode = "rw"
	DBReadOnlyMode  DBmode = "ro"
)

// TODO: pass a mode to indicate read or write
func (ss *SqliteCoordinator[E]) ConnectShards(cx context.Context, mode DBmode) error {
	shards := []*DBShard[E]{}
	keyRanges := ss.keyRanges

	for _, keyRange := range keyRanges {
		shard := &DBShard[E]{id: ss.ToDbName(keyRange), shardKey: keyRange}
		shards = append(shards, shard)
	}

	connQuery := "cache=shared&_threadsafe=1"

	if mode == DBReadOnlyMode || mode == DBReadOnlyMode {
		connQuery = fmt.Sprintf("%s&mode=%s", connQuery, mode)
	}

	for _, shard := range shards {
		log.Printf("connecting to %s.db", shard.id)

		conn, err := sql.Open("sqlite3", fmt.Sprintf("%s.db?%s", shard.id, connQuery))
		if err != nil {
			return fmt.Errorf("Error connecting to database %s: %v", shard.id, err)
		}

		shard.conn = conn
		ss.router.AddShard(shard)
	}

	return nil
}

func (ss *SqliteCoordinator[E]) RegisterShards(cx context.Context) error {
	shards := []*DBShard[E]{}
	keyRanges := ss.keyRanges

	for _, keyRange := range keyRanges {
		shard := &DBShard[E]{id: ss.ToDbName(keyRange), shardKey: keyRange}
		shards = append(shards, shard)
	}

	for _, shard := range shards {
		log.Printf("creating database %s.db", shard.id)

		conn, err := sql.Open("sqlite3", fmt.Sprintf("%s.db?cache=shared&mode=rwc&_threadsafe=1", shard.id))
		if err != nil {
			log.Fatalf("Error connecting to database %s: %v", shard.id, err)
		}

		shard.conn = conn
		ss.router.AddShard(shard)
	}

	ss.router.SetPolicy(ss.policy)

	ctx, cancel := context.WithTimeout(cx, 2*time.Minute)
	defer cancel()

	var errChan = make(chan error, len(shards))

	for _, shard := range shards {
		go func(d Shard[E]) {
			_, err := d.Conn().ExecContext(ctx, CREATE_TABLE_QUERY)
			errChan <- err
		}(shard)
	}

	var errr error

	for i := 0; i < len(shards); i++ {
		err, ok := <-errChan
		if !ok {
			log.Println("channel closed")
			break
		}

		if err != nil {
			errr = err
			log.Println("failed to create table", err)
			continue
		}
	}

	close(errChan)

	if errr != nil {
		ss.DeInit()
		return fmt.Errorf("failed to bootstrap databases")
	}

	return nil
}
