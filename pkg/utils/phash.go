package utils

import (
	"math"
	"math/bits"
	"runtime"
	"strconv"
	"sync"

	"github.com/stashapp/stash/pkg/sliceutil"
)

type Phash struct {
	ID        int     `db:"id"`
	Hash      int64   `db:"phash"`
	Duration  float64 `db:"duration"`
	Neighbors []int
	Bucket    int
}

func FindDuplicates(hashes []*Phash, distance int, durationDiff float64) [][]int {
	// Pre-calculate hash values to avoid allocations and method calls in the inner loop
	uintHashes := make([]uint64, len(hashes))
	for i, h := range hashes {
		uintHashes[i] = uint64(h.Hash)
	}

	numHashes := len(hashes)
	numWorkers := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Distribute work among workers
	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := workerID; i < numHashes; i += numWorkers {
				subject := hashes[i]
				subjectHash := uintHashes[i]

				for j := 0; j < numHashes; j++ {
					if i == j {
						continue
					}
					neighbor := hashes[j]
					if subject.ID == neighbor.ID {
						continue
					}

					neighborHash := uintHashes[j]
					// Hamming distance using native bit counting
					if phashMatches(subject, neighbor, subjectHash, neighborHash, distance, durationDiff) {
						subject.Neighbors = append(subject.Neighbors, j)
					}
				}
			}
		}(w)
	}

	wg.Wait()

	var buckets [][]int
	for _, subject := range hashes {
		if len(subject.Neighbors) > 0 && subject.Bucket == -1 {
			bucket := len(buckets)
			ids := []int{subject.ID}
			subject.Bucket = bucket
			findNeighbors(bucket, subject.Neighbors, hashes, &ids)

			if len(ids) > 1 {
				buckets = append(buckets, ids)
			}
		}
	}

	return buckets
}

func FindDuplicatesContaining(hashes []*Phash, matchingIDs map[int]struct{}, distance int, durationDiff float64) [][]int {
	if len(hashes) == 0 || len(matchingIDs) == 0 {
		return nil
	}

	uintHashes := make([]uint64, len(hashes))
	for i, h := range hashes {
		uintHashes[i] = uint64(h.Hash)
	}

	visited := make([]bool, len(hashes))
	var buckets [][]int
	for i, hash := range hashes {
		if visited[i] {
			continue
		}
		if _, ok := matchingIDs[hash.ID]; !ok {
			continue
		}

		ids, indexes := findDuplicateComponent(i, hashes, uintHashes, distance, durationDiff)
		for _, idx := range indexes {
			visited[idx] = true
		}
		if len(ids) > 1 {
			buckets = append(buckets, ids)
		}
	}

	return buckets
}

func findDuplicateComponent(seed int, hashes []*Phash, uintHashes []uint64, distance int, durationDiff float64) ([]int, []int) {
	queued := make([]bool, len(hashes))
	queue := []int{seed}
	queued[seed] = true

	for head := 0; head < len(queue); head++ {
		subjectIndex := queue[head]
		subject := hashes[subjectIndex]
		subjectHash := uintHashes[subjectIndex]

		for i, candidate := range hashes {
			if queued[i] || subjectIndex == i || subject.ID == candidate.ID {
				continue
			}
			if !phashMatches(subject, candidate, subjectHash, uintHashes[i], distance, durationDiff) {
				continue
			}

			queued[i] = true
			queue = append(queue, i)
		}
	}

	var ids []int
	for _, idx := range queue {
		ids = sliceutil.AppendUnique(ids, hashes[idx].ID)
	}

	return ids, queue
}

func phashMatches(subject *Phash, candidate *Phash, subjectHash uint64, candidateHash uint64, distance int, durationDiff float64) bool {
	if durationDiff >= 0 && subject.Duration > 0 && candidate.Duration > 0 {
		if math.Abs(subject.Duration-candidate.Duration) > durationDiff {
			return false
		}
	}

	return bits.OnesCount64(subjectHash^candidateHash) <= distance
}

func findNeighbors(bucket int, neighbors []int, hashes []*Phash, ids *[]int) {
	for _, id := range neighbors {
		hash := hashes[id]
		if hash.Bucket == -1 {
			hash.Bucket = bucket
			*ids = sliceutil.AppendUnique(*ids, hash.ID)
			findNeighbors(bucket, hash.Neighbors, hashes, ids)
		}
	}
}

func PhashToString(phash int64) string {
	return strconv.FormatUint(uint64(phash), 16)
}

func StringToPhash(s string) (int64, error) {
	ret, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, err
	}

	return int64(ret), nil
}
