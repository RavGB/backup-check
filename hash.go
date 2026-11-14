package main

import (
	"crypto/sha256"
	"io"
	"os"
	"sync"
)

// hashOutcome is the comparison result for one pair of files: equal
// is true when both SHA-256 digests match, and err is set when either
// side could not be read (the caller turns that into an Unreadable
// problem rather than guessing at the content).
type hashOutcome struct {
	equal bool
	err   error
}

// hashFile returns the SHA-256 digest of one file.
func hashFile(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// hashPairs compares every (source, backup) pair with a small worker
// pool. Each worker writes only to the outcome slots of the indexes
// it picked up, so results stay in input order and no locking is
// needed around them.
func hashPairs(pairs [][2]string, workers int) []hashOutcome {
	outcomes := make([]hashOutcome, len(pairs))
	if workers < 1 {
		workers = 1
	}
	if workers > len(pairs) && len(pairs) > 0 {
		workers = len(pairs)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				srcSum, err := hashFile(pairs[i][0])
				if err != nil {
					outcomes[i] = hashOutcome{err: err}
					continue
				}
				bakSum, err := hashFile(pairs[i][1])
				if err != nil {
					outcomes[i] = hashOutcome{err: err}
					continue
				}
				outcomes[i] = hashOutcome{equal: srcSum == bakSum}
			}
		}()
	}
	for i := range pairs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return outcomes
}
