package seed

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func Test_GeneratePermutationsLength5(t *testing.T) {
	t.Skip()

	seeder := RegisterUrlSeeder()
	length := 5
	batchSize := 1000

	generator := NewPermuteGenerator(seeder)
	permutationChan := generator.NextBatch(context.Background(), uint64(length), uint64(batchSize))

	totalCount := 0
	alphas := append(seeder.lowers, seeder.uppers...)

	charSet := append(alphas, seeder.nums...)
	expectedCount := len(alphas) * pow(len(charSet), length-1)

	worker := func(w *sync.WaitGroup) {
		defer w.Done()

		for batch := range permutationChan {
			for _, key := range batch {
				totalCount++

				if len(key) != length {
					t.Errorf("Generated key %s has incorrect length: got %d, want %d", key, len(key), length)
				}

				if !contains(alphas, string(key[0])) {
					t.Errorf("Generated key %s starts with non-alpha character", key)
				}

				for _, char := range key {
					if !contains(charSet, string(char)) {
						t.Errorf("Generated key %s contains invalid character: %c", key, char)
					}
				}
			}
		}
	}

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		worker(&wg)
	}

	fmt.Println("permutations ", totalCount, expectedCount)

	if totalCount != expectedCount {
		t.Errorf("Incorrect total key count: got %d, want %d", totalCount, expectedCount)
	}
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
