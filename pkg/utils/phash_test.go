package utils

import (
	"reflect"
	"sort"
	"testing"
)

func TestFindDuplicatesContainingMatchesFilteredFindDuplicates(t *testing.T) {
	tests := []struct {
		name          string
		hashes        []*Phash
		matchingIDs   map[int]struct{}
		distance      int
		durationDiff  float64
		expectedGroup [][]int
	}{
		{
			name: "exact duplicate groups",
			hashes: []*Phash{
				{ID: 1, Hash: 0x1111, Duration: -1},
				{ID: 2, Hash: 0x1111, Duration: -1},
				{ID: 3, Hash: 0x2222, Duration: -1},
				{ID: 4, Hash: 0x2222, Duration: -1},
				{ID: 5, Hash: 0x3333, Duration: -1},
			},
			matchingIDs:   map[int]struct{}{1: {}, 4: {}},
			distance:      0,
			durationDiff:  -1,
			expectedGroup: [][]int{{1, 2}, {3, 4}},
		},
		{
			name: "transitive fuzzy component",
			hashes: []*Phash{
				{ID: 1, Hash: 0b0000, Duration: -1},
				{ID: 2, Hash: 0b0001, Duration: -1},
				{ID: 3, Hash: 0b0011, Duration: -1},
				{ID: 4, Hash: 0b1111, Duration: -1},
			},
			matchingIDs:   map[int]struct{}{1: {}},
			distance:      1,
			durationDiff:  -1,
			expectedGroup: [][]int{{1, 2, 3}},
		},
		{
			name: "duration limit excludes far match",
			hashes: []*Phash{
				{ID: 1, Hash: 0x4444, Duration: 10},
				{ID: 2, Hash: 0x4444, Duration: 11},
				{ID: 3, Hash: 0x4444, Duration: 20},
			},
			matchingIDs:   map[int]struct{}{1: {}, 3: {}},
			distance:      0,
			durationDiff:  2,
			expectedGroup: [][]int{{1, 2}},
		},
		{
			name: "negative duration disables duration limit",
			hashes: []*Phash{
				{ID: 1, Hash: 0x4444, Duration: 10},
				{ID: 2, Hash: 0x4444, Duration: 11},
				{ID: 3, Hash: 0x4444, Duration: 20},
			},
			matchingIDs:   map[int]struct{}{3: {}},
			distance:      0,
			durationDiff:  -1,
			expectedGroup: [][]int{{1, 2, 3}},
		},
		{
			name: "matching isolated hash returns no duplicate group",
			hashes: []*Phash{
				{ID: 1, Hash: 0x1111, Duration: -1},
				{ID: 2, Hash: 0x2222, Duration: -1},
				{ID: 3, Hash: 0x2222, Duration: -1},
			},
			matchingIDs:   map[int]struct{}{1: {}},
			distance:      0,
			durationDiff:  -1,
			expectedGroup: nil,
		},
		{
			name: "duplicate file entries for same object do not create a group",
			hashes: []*Phash{
				{ID: 1, Hash: 0x1111, Duration: -1},
				{ID: 1, Hash: 0x1111, Duration: -1},
			},
			matchingIDs:   map[int]struct{}{1: {}},
			distance:      0,
			durationDiff:  -1,
			expectedGroup: nil,
		},
		{
			name: "duplicate file entries for same object collapse with other objects",
			hashes: []*Phash{
				{ID: 1, Hash: 0x1111, Duration: -1},
				{ID: 1, Hash: 0x1111, Duration: -1},
				{ID: 2, Hash: 0x1111, Duration: -1},
			},
			matchingIDs:   map[int]struct{}{1: {}},
			distance:      0,
			durationDiff:  -1,
			expectedGroup: [][]int{{1, 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			full := FindDuplicates(clonePhashes(tt.hashes), tt.distance, tt.durationDiff)
			want := filterDuplicateGroupsForTest(full, tt.matchingIDs)
			got := FindDuplicatesContaining(clonePhashes(tt.hashes), tt.matchingIDs, tt.distance, tt.durationDiff)

			assertDuplicateGroupsEqual(t, got, want)
			assertDuplicateGroupsEqual(t, got, tt.expectedGroup)
		})
	}
}

func TestFindDuplicatesContainingEmptyInput(t *testing.T) {
	hashes := []*Phash{
		{ID: 1, Hash: 0x1111, Duration: -1},
		{ID: 2, Hash: 0x1111, Duration: -1},
	}

	if got := FindDuplicatesContaining(nil, map[int]struct{}{1: {}}, 0, -1); got != nil {
		t.Fatalf("FindDuplicatesContaining(nil) = %#v, want nil", got)
	}

	if got := FindDuplicatesContaining(clonePhashes(hashes), nil, 0, -1); got != nil {
		t.Fatalf("FindDuplicatesContaining(nil matchingIDs) = %#v, want nil", got)
	}

	if got := FindDuplicatesContaining(clonePhashes(hashes), map[int]struct{}{}, 0, -1); got != nil {
		t.Fatalf("FindDuplicatesContaining(empty matchingIDs) = %#v, want nil", got)
	}
}

func clonePhashes(hashes []*Phash) []*Phash {
	ret := make([]*Phash, len(hashes))
	for i, hash := range hashes {
		ret[i] = &Phash{
			ID:       hash.ID,
			Hash:     hash.Hash,
			Duration: hash.Duration,
			Bucket:   -1,
		}
	}
	return ret
}

func filterDuplicateGroupsForTest(groups [][]int, matchingIDs map[int]struct{}) [][]int {
	var ret [][]int
	for _, group := range groups {
		for _, id := range group {
			if _, ok := matchingIDs[id]; ok {
				ret = append(ret, group)
				break
			}
		}
	}
	return ret
}

func assertDuplicateGroupsEqual(t *testing.T, got [][]int, want [][]int) {
	t.Helper()

	normalizedGot := normalizeDuplicateGroups(got)
	normalizedWant := normalizeDuplicateGroups(want)
	if !reflect.DeepEqual(normalizedGot, normalizedWant) {
		t.Fatalf("duplicate groups = %#v, want %#v", got, want)
	}
}

func normalizeDuplicateGroups(groups [][]int) [][]int {
	if len(groups) == 0 {
		return nil
	}

	ret := make([][]int, len(groups))
	for i, group := range groups {
		ret[i] = append([]int(nil), group...)
		sort.Ints(ret[i])
	}
	sort.Slice(ret, func(i, j int) bool {
		a := ret[i]
		b := ret[j]
		for idx := 0; idx < len(a) && idx < len(b); idx++ {
			if a[idx] != b[idx] {
				return a[idx] < b[idx]
			}
		}
		return len(a) < len(b)
	})

	return ret
}
