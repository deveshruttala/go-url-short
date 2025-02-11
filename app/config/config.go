package config

import (
	"log"
	"strconv"
)

type AppConfig struct {
	AppPort    string
	SeedSize   string
	DomainName string
	CacheAddrs []string
}

var sizeMap = map[string]uint64{
	"U": 1,
	"K": 1000,
	"M": 1000000,
	"B": 1000000000,
}

func MustParseSeedSize(sizeStr string, defaults ...string) uint64 {
	hasDefaults := len(defaults) > 0

	val, err := ParseSeedSize(sizeStr)
	if err != nil && !hasDefaults {
		log.Fatal(err)
	}

	if err != nil {
		val, err = ParseSeedSize(defaults[0])
	}

	if err != nil {
		log.Fatal("failed to convert sizeStr to int", err)
	}

	return val
}

func ParseSeedSize(sizeStr string) (uint64, error) {
	l := len(sizeStr)
	numberStr, sizeNotation := sizeStr[:l-1], string(sizeStr[l-1])

	v, ok := sizeMap[sizeNotation]
	if !ok {
		log.Println("no size notation provided, taking value as is")

		v = sizeMap["U"]
		numberStr = sizeStr
	}

	number, err := strconv.ParseInt(numberStr, 10, 64)
	if err != nil {
		log.Fatal("invalid size value provided, expected in the form <number>K, <number>M, <number>B")
	}

	if number < 1 {
		log.Fatal("value cannot be less than 1")
	}

	if number >= 1000 {
		log.Fatal("way too much size request", sizeStr)
	}

	return uint64(number) * v, nil
}
