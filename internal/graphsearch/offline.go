package graphsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

const OfflineMaxStates = 25000000

type OfflineReport struct {
	Status       string `json:"status"`
	Degree       int    `json:"degree"`
	Copies       int    `json:"copies"`
	Explored     int    `json:"explored"`
	Eccentricity int    `json:"eccentricity"`
	Farthest     []Path `json:"farthest"`
	Flip         Path   `json:"flip"`
	ReportSHA256 string `json:"report_sha256"`
}

type offlineIndex struct {
	keys   []uint64
	values []uint32
	mask   uint64
}

func newOfflineIndex(capacity int) *offlineIndex {
	values := make([]uint32, capacity)
	for i := range values {
		values[i] = math.MaxUint32
	}
	return &offlineIndex{keys: make([]uint64, capacity), values: values, mask: uint64(capacity - 1)}
}

func offlineMix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func (i *offlineIndex) slot(key uint64) uint64 {
	s := offlineMix(key) & i.mask
	for i.values[s] != math.MaxUint32 && i.keys[s] != key {
		s = (s + 1) & i.mask
	}
	return s
}

func (i *offlineIndex) contains(key uint64) bool {
	return i.values[i.slot(key)] != math.MaxUint32
}

func (i *offlineIndex) insert(key uint64, value uint32) {
	s := i.slot(key)
	i.keys[s], i.values[s] = key, value
}

func offlineEncode(state []int) uint64 {
	var out uint64
	for _, value := range state {
		out = out<<4 | uint64(value)
	}
	return out
}

func offlineDecode(state uint64, degree int) []int {
	out := make([]int, degree)
	for i := degree - 1; i >= 0; i-- {
		out[i] = int(state & 15)
		state >>= 4
	}
	return out
}

func offlineStep(state uint64, degree, start int, inverse bool) uint64 {
	var shifts [4]uint
	var block [4]uint64
	for j := 0; j < 4; j++ {
		position := (start + j) % degree
		shifts[j] = uint(4 * (degree - 1 - position))
		block[j] = (state >> shifts[j]) & 15
	}
	for j := 0; j < 4; j++ {
		state &^= 15 << shifts[j]
		shift := 1
		if inverse {
			shift = 3
		}
		state |= block[(j+shift)%4] << shifts[j]
	}
	return state
}

func offlinePath(states []uint64, parents []uint32, via []int16, distances []uint8, degree int, index uint32) Path {
	word := []string{}
	for current := index; current != 0; current = parents[current] {
		move := via[current]
		name := "left" + strconv.Itoa(int(move/2))
		if move%2 == 1 {
			name += "^-1"
		}
		word = append(word, name)
	}
	for left, right := 0, len(word)-1; left < right; left, right = left+1, right-1 {
		word[left], word[right] = word[right], word[left]
	}
	return Path{State: offlineDecode(states[index], degree), Distance: int(distances[index]), Word: word}
}

func OfflineRepeatedWrapped4(ctx context.Context, degree, copies, maxStates int) (OfflineReport, error) {
	report := OfflineReport{Degree: degree, Copies: copies, Farthest: []Path{}}
	if degree < 5 || degree > 12 || copies < 2 || copies >= degree || maxStates < 1 || maxStates > OfflineMaxStates {
		return report, errors.New("invalid offline repeated-graph limits")
	}
	expected := 1
	for i := 2; i <= degree; i++ {
		expected *= i
	}
	for i := 2; i <= copies; i++ {
		expected /= i
	}
	if expected > maxStates {
		return report, errors.New("offline state limit is below the exact component size")
	}
	capacity := 1
	for capacity < expected*2 {
		capacity <<= 1
	}
	index := newOfflineIndex(capacity)
	start := make([]int, degree)
	for i := 0; i < degree-copies; i++ {
		start[i] = i
	}
	for i := degree - copies; i < degree; i++ {
		start[i] = degree - copies
	}
	states := make([]uint64, 1, expected)
	distances := make([]uint8, 1, expected)
	parents := make([]uint32, 1, expected)
	via := make([]int16, 1, expected)
	states[0] = offlineEncode(start)
	index.insert(states[0], 0)
	farthest := uint32(0)
	for queue := uint32(0); queue < uint32(len(states)); queue++ {
		if queue%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return report, err
			}
		}
		for i := 0; i < degree; i++ {
			for inverse := 0; inverse < 2; inverse++ {
				next := offlineStep(states[queue], degree, i, inverse == 1)
				if index.contains(next) {
					continue
				}
				id := uint32(len(states))
				index.insert(next, id)
				states = append(states, next)
				distances = append(distances, distances[queue]+1)
				parents = append(parents, queue)
				via = append(via, int16(2*i+inverse))
				if distances[id] > distances[farthest] {
					farthest = id
				}
			}
		}
	}
	for id, distance := range distances {
		if int(distance) == int(distances[farthest]) && len(report.Farthest) < MaxResults {
			report.Farthest = append(report.Farthest, offlinePath(states, parents, via, distances, degree, uint32(id)))
		}
	}
	for i, j := 0, len(start)-1; i < j; i, j = i+1, j-1 {
		start[i], start[j] = start[j], start[i]
	}
	flip := index.values[index.slot(offlineEncode(start))]
	report.Flip = offlinePath(states, parents, via, distances, degree, flip)
	report.Status, report.Explored, report.Eccentricity = "completed", len(states), int(distances[farthest])
	body, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	digest := sha256.Sum256(body)
	report.ReportSHA256 = hex.EncodeToString(digest[:])
	return report, nil
}
