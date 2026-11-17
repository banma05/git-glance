package model

import (
	"hash/fnv"
	"sort"
)

// PaletteSize is the number of distinct colours.
const PaletteSize = 12

// AssignColors gives each distinct topic a palette slot: the hash of the
// topic picks the start, and a taken slot moves on to the next free one.
// When all slots are taken, topics share colours.
func AssignColors(topics []string) map[string]int {
	uniq := map[string]bool{}
	for _, t := range topics {
		uniq[t] = true
	}
	sorted := make([]string, 0, len(uniq))
	for t := range uniq {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)
	var used [PaletteSize]bool
	res := make(map[string]int, len(sorted))
	for _, t := range sorted {
		h := fnv.New32a()
		h.Write([]byte(t))
		start := int(h.Sum32() % PaletteSize)
		slot := start
		for i := 0; i < PaletteSize; i++ {
			cand := (start + i) % PaletteSize
			if !used[cand] {
				slot = cand
				break
			}
		}
		used[slot] = true
		res[t] = slot
	}
	return res
}
