package db

const CREATE_SHARD_STATUS_QUERY = `
CREATE TABLE shard_status (
    shard_id VARCHAR(255) NOT NULL,
    shard_char VARCHAR(255) NOT NULL,
    start INTEGER NOT NULL,
    end INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (shard_id, shard_char)
);

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA temp_store=MEMORY;
PRAGMA cache_size=-2000;
PRAGMA busy_timeout=5000;
PRAGMA mmap_size = 30000000000;
PRAGMA journal_size_limit = 104857600;
`

const CREATE_TABLE_QUERY = `
CREATE TABLE IF NOT EXISTS urls (
	url TEXT DEFAULT NULL,
	short_key TEXT NOT NULL,
	malicious INTEGER DEFAULT NULL,
	generation INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,
	deleted_at TIMESTAMP 
);

CREATE INDEX IF NOT EXISTS idx_short_key ON urls (short_key);
CREATE INDEX IF NOT EXISTS idx_null_url ON urls(url) WHERE url is NULL;

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA temp_store=MEMORY;
PRAGMA cache_size=-2000;
PRAGMA busy_timeout=5000;
PRAGMA mmap_size = 30000000000;
PRAGMA journal_size_limit = 104857600;
PRAGMA threads = 10
`

const DROP_TABLE_QUERY = `
	DROP INDEX IF EXISTS urls.idx_null_url;
	DROP INDEX IF EXISTS urls.idx_short_key;
	DROP TABLE IF EXISTS urls;
`

const CREATE_INDEX_QUERY = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_short_key ON urls (short_key);
CREATE INDEX IF NOT EXISTS idx_null_url ON urls(url) WHERE url is NULL;
`
