package graphsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxStates     = 20000
	MaxDegree     = 64
	MaxGenerators = 16
	MaxDepth      = 256
	MaxResults    = 16
)

var generatorName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

type Generator struct {
	Name        string `json:"name"`
	Permutation []int  `json:"permutation"`
}

type Input struct {
	AllowRepeatedStart bool        `json:"allow_repeated_start,omitempty"`
	Degree             int         `json:"degree"`
	Generators         []Generator `json:"generators"`
	Start              []int       `json:"start"`
	Bidirectional      bool        `json:"bidirectional"`
	MaxStates          int         `json:"max_states"`
	MaxDepth           int         `json:"max_depth"`
}

type Path struct {
	State    []int    `json:"state"`
	Distance int      `json:"distance"`
	Word     []string `json:"word"`
}

type Report struct {
	Status            string `json:"status"`
	LimitReason       string `json:"limit_reason,omitempty"`
	Exhausted         bool   `json:"exhausted"`
	Explored          int    `json:"explored"`
	MaxDepthReached   int    `json:"max_depth_reached"`
	Eccentricity      int    `json:"eccentricity,omitempty"`
	MissingTargets    int    `json:"missing_targets,omitempty"`
	Targets           []Path `json:"targets,omitempty"`
	Farthest          []Path `json:"farthest,omitempty"`
	CertificateSHA256 string `json:"certificate_sha256"`
}

type Verification struct {
	Matches           bool   `json:"matches"`
	State             []int  `json:"state"`
	Distance          int    `json:"distance"`
	Shortest          *bool  `json:"shortest,omitempty"`
	CertificateSHA256 string `json:"certificate_sha256"`
}

type transition struct {
	name        string
	permutation []int
}

type visit struct {
	parent int
	via    string
	depth  int
}

func Validate(in Input) error {
	if in.Degree < 1 || in.Degree > MaxDegree || len(in.Generators) < 1 || len(in.Generators) > MaxGenerators || in.MaxStates < 1 || in.MaxStates > MaxStates || in.MaxDepth < 0 || in.MaxDepth > MaxDepth {
		return errors.New("invalid permutation graph limits")
	}
	if !validState(in, in.Start) {
		return errors.New("invalid start state")
	}
	seen := map[string]bool{}
	for _, generator := range in.Generators {
		if !generatorName.MatchString(generator.Name) || seen[generator.Name] || !validPermutation(generator.Permutation, in.Degree) {
			return errors.New("invalid or duplicate generator")
		}
		seen[generator.Name] = true
	}
	return nil
}

func validState(in Input, state []int) bool {
	if in.AllowRepeatedStart {
		if len(state) != in.Degree {
			return false
		}
		for _, value := range state {
			if value < 0 || value >= in.Degree {
				return false
			}
		}
		return true
	}
	return validPermutation(state, in.Degree)
}

func validPermutation(permutation []int, degree int) bool {
	if len(permutation) != degree {
		return false
	}
	seen := make([]bool, degree)
	for _, value := range permutation {
		if value < 0 || value >= degree || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func stateKey(state []int) string {
	parts := make([]string, len(state))
	for i, value := range state {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

func inverse(permutation []int) []int {
	out := make([]int, len(permutation))
	for i, value := range permutation {
		out[value] = i
	}
	return out
}

func apply(state, permutation []int) []int {
	out := make([]int, len(state))
	for i, image := range permutation {
		out[i] = state[image]
	}
	return out
}

func transitions(in Input) []transition {
	out := []transition{}
	for _, generator := range in.Generators {
		out = append(out, transition{name: generator.Name, permutation: generator.Permutation})
		if in.Bidirectional {
			out = append(out, transition{name: generator.Name + "^-1", permutation: inverse(generator.Permutation)})
		}
	}
	return out
}

func search(ctx context.Context, in Input, targets [][]int, longest bool) (Report, error) {
	report := Report{Status: "completed", Targets: []Path{}, Farthest: []Path{}}
	if err := Validate(in); err != nil {
		return report, err
	}
	if len(targets) > MaxResults {
		return report, errors.New("too many targets")
	}
	targetSet := map[string]bool{}
	for _, target := range targets {
		if !validState(in, target) {
			return report, errors.New("invalid target permutation")
		}
		key := stateKey(target)
		if targetSet[key] {
			return report, errors.New("duplicate target")
		}
		targetSet[key] = true
	}
	digest := sha256.New()
	encoded, _ := json.Marshal(struct {
		Input   Input   `json:"input"`
		Targets [][]int `json:"targets,omitempty"`
		Longest bool    `json:"longest"`
	}{in, targets, longest})
	_, _ = digest.Write(encoded)
	moves := transitions(in)
	states := [][]int{append([]int(nil), in.Start...)}
	indices := map[string]int{stateKey(in.Start): 0}
	visits := []visit{{parent: -1}}
	found := map[string]bool{}
	if targetSet[stateKey(in.Start)] {
		found[stateKey(in.Start)] = true
	}
	queue := 0
	for queue < len(states) && (longest || len(found) < len(targetSet)) {
		if queue%64 == 0 {
			if err := ctx.Err(); err != nil {
				return report, err
			}
		}
		index := queue
		queue++
		current := visits[index]
		if current.depth > report.MaxDepthReached {
			report.MaxDepthReached = current.depth
		}
		if current.depth >= in.MaxDepth {
			report.LimitReason = "max_depth"
			continue
		}
		for _, move := range moves {
			next := apply(states[index], move.permutation)
			key := stateKey(next)
			if _, ok := indices[key]; ok {
				continue
			}
			if len(states) >= in.MaxStates {
				report.Status, report.LimitReason = "limit_reached", "max_states"
				report.Explored = len(states)
				report.CertificateSHA256 = hex.EncodeToString(digest.Sum(nil))
				return report, nil
			}
			nextIndex := len(states)
			states = append(states, next)
			indices[key] = nextIndex
			visits = append(visits, visit{parent: index, via: move.name, depth: current.depth + 1})
			_, _ = fmt.Fprintf(digest, "\n%s;%d;%d;%s", key, nextIndex, current.depth+1, move.name)
			if targetSet[key] {
				found[key] = true
			}
		}
		if !longest && len(found) == len(targetSet) {
			break
		}
	}
	report.Explored = len(states)
	report.Exhausted = report.LimitReason == "" && queue == len(states)
	if report.LimitReason != "" {
		report.Status = "limit_reached"
	}
	for _, target := range targets {
		key := stateKey(target)
		index, ok := indices[key]
		if !ok {
			report.MissingTargets++
			continue
		}
		report.Targets = append(report.Targets, pathFor(states, visits, index))
	}
	if longest {
		maxDepth := 0
		for _, current := range visits {
			if current.depth > maxDepth {
				maxDepth = current.depth
			}
		}
		report.Eccentricity = maxDepth
		for index, current := range visits {
			if current.depth == maxDepth && len(report.Farthest) < MaxResults {
				report.Farthest = append(report.Farthest, pathFor(states, visits, index))
			}
		}
	}
	report.CertificateSHA256 = hex.EncodeToString(digest.Sum(nil))
	return report, nil
}

func pathFor(states [][]int, visits []visit, index int) Path {
	word := []string{}
	for current := index; visits[current].parent >= 0; current = visits[current].parent {
		word = append(word, visits[current].via)
	}
	for left, right := 0, len(word)-1; left < right; left, right = left+1, right-1 {
		word[left], word[right] = word[right], word[left]
	}
	return Path{State: append([]int(nil), states[index]...), Distance: visits[index].depth, Word: word}
}

func FindTargets(ctx context.Context, in Input, targets [][]int) (Report, error) {
	if len(targets) == 0 {
		return Report{}, errors.New("at least one target required")
	}
	return search(ctx, in, targets, false)
}

func LongestStates(ctx context.Context, in Input) (Report, error) {
	return search(ctx, in, nil, true)
}

func VerifyWord(ctx context.Context, in Input, word []string, target []int, verifyShortest bool) (Verification, error) {
	out := Verification{State: append([]int(nil), in.Start...), Distance: len(word)}
	if err := Validate(in); err != nil {
		return out, err
	}
	if len(word) > MaxDepth || !validState(in, target) {
		return out, errors.New("invalid word or target")
	}
	moves := map[string][]int{}
	for _, move := range transitions(in) {
		moves[move.name] = move.permutation
	}
	digest := sha256.New()
	encoded, _ := json.Marshal(struct {
		Input  Input    `json:"input"`
		Word   []string `json:"word"`
		Target []int    `json:"target"`
	}{in, word, target})
	_, _ = digest.Write(encoded)
	for _, name := range word {
		move, ok := moves[name]
		if !ok {
			return out, errors.New("unknown generator in word")
		}
		out.State = apply(out.State, move)
		_, _ = fmt.Fprintf(digest, "\n%s;%s", name, stateKey(out.State))
	}
	out.Matches = stateKey(out.State) == stateKey(target)
	if out.Matches && verifyShortest {
		if len(word) == 0 {
			value := true
			out.Shortest = &value
		} else {
			bounded := in
			bounded.MaxDepth = len(word) - 1
			report, err := FindTargets(ctx, bounded, [][]int{target})
			if err != nil {
				return out, err
			}
			if len(report.Targets) == 1 {
				value := false
				out.Shortest = &value
			} else if report.LimitReason != "max_states" {
				value := true
				out.Shortest = &value
			} else {
				return out, errors.New("state limit prevented shortest-word verification")
			}
		}
	}
	out.CertificateSHA256 = hex.EncodeToString(digest.Sum(nil))
	return out, nil
}
