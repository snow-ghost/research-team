package graphsearch

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func symmetricGroupInput() Input {
	return Input{
		Degree: 3,
		Generators: []Generator{
			{Name: "swap", Permutation: []int{1, 0, 2}},
			{Name: "rotate", Permutation: []int{1, 2, 0}},
		},
		Start:         []int{0, 1, 2},
		Bidirectional: true,
		MaxStates:     16,
		MaxDepth:      8,
	}
}

func TestFindTargetsReturnsShortestReproducibleWords(t *testing.T) {
	in := symmetricGroupInput()
	target := []int{2, 1, 0}
	report, err := FindTargets(context.Background(), in, [][]int{target})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "completed" || report.Explored != 6 || len(report.Targets) != 1 || report.Targets[0].Distance != 2 || report.CertificateSHA256 == "" {
		t.Fatalf("unexpected target report: %+v", report)
	}
	verification, err := VerifyWord(context.Background(), in, report.Targets[0].Word, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Matches || verification.Shortest == nil || !*verification.Shortest {
		t.Fatalf("shortest word not independently verified: %+v", verification)
	}
}

func TestLongestStatesReportsEccentricityWithoutCallingItDiameter(t *testing.T) {
	report, err := LongestStates(context.Background(), symmetricGroupInput())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "completed" || !report.Exhausted || report.Explored != 6 || report.Eccentricity != 2 || len(report.Farthest) != 2 || report.Farthest[0].Distance != 2 {
		t.Fatalf("unexpected exhaustive report: %+v", report)
	}
}

func TestVerifyWordRejectsNonShortestAndMalformedInput(t *testing.T) {
	in := symmetricGroupInput()
	verification, err := VerifyWord(context.Background(), in, []string{"swap", "swap"}, in.Start, true)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Matches || verification.Shortest == nil || *verification.Shortest {
		t.Fatalf("non-shortest word accepted: %+v", verification)
	}
	empty, err := VerifyWord(context.Background(), in, nil, in.Start, true)
	if err != nil || !empty.Matches || empty.Shortest == nil || !*empty.Shortest {
		t.Fatalf("empty word rejected: %+v %v", empty, err)
	}
	if _, err := VerifyWord(context.Background(), in, []string{"unknown"}, in.Start, false); err == nil {
		t.Fatal("unknown generator accepted")
	}
	in.Start = []int{0, 0, 2}
	if _, err := LongestStates(context.Background(), in); err == nil {
		t.Fatal("malformed permutation accepted")
	}
}

func TestLimitsAndCancellationCannotProduceExhaustiveCertificate(t *testing.T) {
	in := symmetricGroupInput()
	in.MaxStates = 2
	report, err := LongestStates(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "limit_reached" || report.LimitReason != "max_states" || report.Exhausted || report.Explored != 2 {
		t.Fatalf("limit hidden: %+v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LongestStates(ctx, symmetricGroupInput()); err == nil {
		t.Fatal("cancelled search produced a result")
	}
}

func wrappedFourInput(n int) Input {
	generators := make([]Generator, 0, n)
	for i := 0; i < n; i++ {
		permutation := make([]int, n)
		for j := 0; j < n; j++ {
			permutation[j] = j
		}
		for j := 0; j < 3; j++ {
			permutation[(i+j)%n] = (i + j + 1) % n
		}
		permutation[(i+3)%n] = i
		generators = append(generators, Generator{Name: "left" + strconv.Itoa(i), Permutation: permutation})
	}
	start := make([]int, n)
	for i := range start {
		start[i] = i
	}
	return Input{Degree: n, Generators: generators, Start: start, Bidirectional: true, MaxStates: 6000, MaxDepth: 8}
}

func TestWrappedFourCyclesBDD_FullFlipIsNotDiameterWitness(t *testing.T) {
	in := wrappedFourInput(7)
	longest, err := LongestStates(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if longest.Status != "completed" || !longest.Exhausted || longest.Explored != 5040 || longest.Eccentricity != 6 {
		t.Fatalf("unexpected wrapped eccentricity: %+v", longest)
	}
	target := []int{6, 5, 4, 3, 2, 1, 0}
	flip, err := FindTargets(context.Background(), in, [][]int{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(flip.Targets) != 1 || flip.Targets[0].Distance != 5 {
		t.Fatalf("unexpected full-flip certificate: %+v", flip.Targets)
	}
	state := append([]int(nil), in.Start...)
	for _, name := range flip.Targets[0].Word {
		state = referenceWrappedStep(state, name)
	}
	if !reflect.DeepEqual(state, target) {
		t.Fatalf("returned word does not reproduce target with block rotation: %v -> %v", flip.Targets[0].Word, state)
	}
	verification, err := VerifyWord(context.Background(), in, flip.Targets[0].Word, target, true)
	if err != nil || !verification.Matches || verification.Shortest == nil || !*verification.Shortest {
		t.Fatalf("full-flip word not independently verified: %+v %v", verification, err)
	}
	if flip.Targets[0].Distance >= longest.Eccentricity {
		t.Fatal("test does not distinguish flip from diameter")
	}
}

func referenceWrappedStep(state []int, name string) []int {
	inverse := strings.HasSuffix(name, "^-1")
	index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "left"), "^-1"))
	if err != nil {
		panic(err)
	}
	out := append([]int(nil), state...)
	block := make([]int, 4)
	for j := 0; j < 4; j++ {
		block[j] = state[(index+j)%len(state)]
	}
	for j := 0; j < 4; j++ {
		if inverse {
			out[(index+j)%len(state)] = block[(j+3)%4]
		} else {
			out[(index+j)%len(state)] = block[(j+1)%4]
		}
	}
	return out
}

func referenceWrappedAnalysis(n int) (int, int, int) {
	start := make([]int, n)
	for i := range start {
		start[i] = i
	}
	return referenceWrappedAnalysisFor(start)
}

func referenceWrappedAnalysisFor(start []int) (int, int, int) {
	n := len(start)
	states := [][]int{start}
	indices := map[string]int{stateKey(start): 0}
	distances := []int{0}
	eccentricity := 0
	for queue := 0; queue < len(states); queue++ {
		state := states[queue]
		for i := 0; i < n; i++ {
			for _, inverse := range []bool{false, true} {
				name := "left" + strconv.Itoa(i)
				if inverse {
					name += "^-1"
				}
				next := referenceWrappedStep(state, name)
				key := stateKey(next)
				if _, ok := indices[key]; ok {
					continue
				}
				indices[key] = len(states)
				states = append(states, next)
				distances = append(distances, distances[queue]+1)
				if distances[queue]+1 > eccentricity {
					eccentricity = distances[queue] + 1
				}
			}
		}
	}
	target := make([]int, n)
	for i := range target {
		target[i] = start[n-1-i]
	}
	return len(states), eccentricity, distances[indices[stateKey(target)]]
}

func TestWrappedFourCyclesBDD_IndependentReferenceBFSCrossCheck(t *testing.T) {
	for _, n := range []int{5, 6, 7} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			in := wrappedFourInput(n)
			longest, err := LongestStates(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			target := make([]int, n)
			for i := range target {
				target[i] = n - 1 - i
			}
			flip, err := FindTargets(context.Background(), in, [][]int{target})
			if err != nil {
				t.Fatal(err)
			}
			explored, eccentricity, distance := referenceWrappedAnalysis(n)
			if longest.Explored != explored || longest.Eccentricity != eccentricity || len(flip.Targets) != 1 || flip.Targets[0].Distance != distance {
				t.Fatalf("reference mismatch: explored=%d eccentricity=%d distance=%d; longest=%+v flip=%+v", explored, eccentricity, distance, longest, flip.Targets)
			}
		})
	}
	in := wrappedFourInput(8)
	report, err := LongestStates(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "limit_reached" || report.Exhausted || report.LimitReason != "max_states" {
		t.Fatalf("partial S8 component was presented as exhaustive: %+v", report)
	}
}

func repeatedFourInput(n, copies int) Input {
	in := wrappedFourInput(n)
	in.AllowRepeatedStart = true
	in.Start = make([]int, n)
	for i := 0; i < n-copies; i++ {
		in.Start[i] = i
	}
	for i := n - copies; i < n; i++ {
		in.Start[i] = n - copies
	}
	in.MaxStates = MaxStates
	return in
}

func TestRepeatedSymbolsBDD_MultisetSearchMatchesReference(t *testing.T) {
	for _, copies := range []int{2, 3} {
		in := repeatedFourInput(7, copies)
		longest, err := LongestStates(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		target := make([]int, 7)
		for i := range target {
			target[i] = in.Start[6-i]
		}
		flip, err := FindTargets(context.Background(), in, [][]int{target})
		if err != nil {
			t.Fatal(err)
		}
		explored, eccentricity, distance := referenceWrappedAnalysisFor(in.Start)
		if longest.Explored != explored || longest.Eccentricity != eccentricity || len(flip.Targets) != 1 || flip.Targets[0].Distance != distance {
			t.Fatalf("multiset reference mismatch: explored=%d eccentricity=%d distance=%d; longest=%+v flip=%+v", explored, eccentricity, distance, longest, flip.Targets)
		}
	}
	in := repeatedFourInput(7, 2)
	in.AllowRepeatedStart = false
	if _, err := LongestStates(context.Background(), in); err == nil {
		t.Fatal("repeated state accepted without explicit opt-in")
	}
}

func TestRepeatedSymbolsBDD_TenPositionIndependentCertificates(t *testing.T) {
	in := repeatedFourInput(10, 2)
	flip := make([]int, 10)
	for i := range flip {
		flip[i] = in.Start[9-i]
	}
	flipWord := []string{"left1", "left2^-1", "left8^-1", "left5", "left3", "left1^-1", "left9^-1", "left2^-1"}
	verification, err := VerifyWord(context.Background(), in, flipWord, flip, false)
	if err != nil || !verification.Matches || verification.Distance != 8 {
		t.Fatalf("two-copy flip certificate failed: %+v %v", verification, err)
	}
	far := []int{8, 7, 8, 6, 5, 4, 3, 2, 1, 0}
	farWord := []string{"left0", "left0", "left1", "left7^-1", "left9^-1", "left2^-1", "left0^-1", "left3^-1", "left8"}
	verification, err = VerifyWord(context.Background(), in, farWord, far, false)
	if err != nil || !verification.Matches || verification.Distance != 9 {
		t.Fatalf("two-copy farther-state certificate failed: %+v %v", verification, err)
	}
	state := append([]int(nil), in.Start...)
	for _, name := range farWord {
		state = referenceWrappedStep(state, name)
	}
	if !reflect.DeepEqual(state, far) {
		t.Fatalf("reference mismatch for farther state: %v", state)
	}
}

func TestRepeatedSymbolsBDD_PartialSearchIsNotExhaustive(t *testing.T) {
	in := repeatedFourInput(8, 2)
	report, err := LongestStates(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "limit_reached" || report.Exhausted || report.LimitReason != "max_states" {
		t.Fatalf("partial multiset component was presented as exhaustive: %+v", report)
	}
}
