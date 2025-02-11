# shortner

URL shortner, obviously.


## Requirements
- go v1.23
- sqlite

## Internals

The initial idea was to shard the database by keyranges.
`KeyRange`s are determined by the starting character.

Pre-seed the database with the records, to allow optimistic
locking, to get avoid concurrent writes of the same key.

This would also reduce the time for computing `sha` sum of
urls, trimming them, and checking if the key exists.
We could use a bloom filter, but the system is meant to run
on minimal dependency. Having bloom filters would increase
our storage requirements.

We will come back to this later.

### Key Generation

Initially:

- had a character-set from `a-zA-Z0-9`, skipping a few characters like
`0`, `o`, `l`, `I` etc.
- generate 7 character length strings, starting with character.
- use a GenerationFunction which uses some custom logic to combine numbers and characters

Base58 does have the same characterset I was trying to use. And the base58
representation of the numbers looks random.
To have 6 characters. We need to start with `1 billion`,for 7 characters, its `*100`.(Found by derivation).
For key sizes `< 6`, its `/10`

The formula in our case, comes down to base of `1 billion`, and `*100*n`,
where `n = K - 6`, where `K` is key-size.

Sequentially generating the numbers would still make it predictible. To mitigate this
I can:

- generate the base58 representations
- shuffle the values
- insert them in batches
- prefix the generated key with the shard id
- maintain a table to track the last number of the batch for each shard character

So, `2ric1A`, gets prefixed with `a2ric1A`, giving us more data to work with.
This allows us to have non-integer shards, so between `a-e`, the first shard, we can
choose how many `6 character` urls to generate for each of `a`, `b`, `c`...`e`.
