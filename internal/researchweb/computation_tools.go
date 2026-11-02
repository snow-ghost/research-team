package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/graphsearch"
)

const computationArgumentLimit = 128000

type ComputationGraphConfig struct {
	Kind               string `json:"kind"`
	Degree             int    `json:"degree"`
	CycleLength        int    `json:"cycle_length"`
	Start              []int  `json:"start,omitempty"`
	AllowRepeatedStart bool   `json:"allow_repeated_start,omitempty"`
	MaxStates          int    `json:"max_states"`
	MaxDepth           int    `json:"max_depth"`
}

func buildComputationGraph(config ComputationGraphConfig) (graphsearch.Input, error) {
	var out graphsearch.Input
	if config.Kind != "wrapped_k_cycles" || config.Degree < 1 || config.Degree > graphsearch.MaxDegree || config.CycleLength < 1 || config.CycleLength > config.Degree {
		return out, RuleError("Некорректная конфигурация wrapped-графа.")
	}
	out = graphsearch.Input{Degree: config.Degree, Start: append([]int(nil), config.Start...), AllowRepeatedStart: config.AllowRepeatedStart, Bidirectional: true, MaxStates: config.MaxStates, MaxDepth: config.MaxDepth}
	if len(out.Start) == 0 {
		out.Start = make([]int, config.Degree)
		for i := range out.Start {
			out.Start[i] = i
		}
	}
	for i := 0; i < config.Degree; i++ {
		permutation := make([]int, config.Degree)
		for j := range permutation {
			permutation[j] = j
		}
		for j := 0; j < config.CycleLength-1; j++ {
			permutation[(i+j)%config.Degree] = (i + j + 1) % config.Degree
		}
		permutation[(i+config.CycleLength-1)%config.Degree] = i
		out.Generators = append(out.Generators, graphsearch.Generator{Name: "left" + strconv.Itoa(i), Permutation: permutation})
	}
	return out, graphsearch.Validate(out)
}

func computationGraphFor(config Config, workspace string) (*graphsearch.Input, error) {
	for _, w := range config.Workspaces {
		if w.ID == workspace && w.Graph != nil {
			graph, err := buildComputationGraph(*w.Graph)
			if err != nil {
				return nil, err
			}
			return &graph, nil
		}
	}
	return nil, nil
}

type ComputationValidationError struct{ Expected *graphsearch.Input }

func (e *ComputationValidationError) Error() string {
	return "Описание графа не совпадает с закрепленной конфигурацией сервера. Выберите другой каталог или используйте закрепленный граф."
}

func (e *ComputationValidationError) ExpectedGraph() any { return e.Expected }

func validatePinnedGraph(pinned *graphsearch.Input, actual graphsearch.Input) error {
	if pinned != nil && !reflect.DeepEqual(*pinned, actual) {
		return &ComputationValidationError{Expected: pinned}
	}
	return nil
}

type runBFSArguments struct {
	Graph   graphsearch.Input `json:"graph"`
	Targets [][]int           `json:"targets"`
}

type longestStatesArguments struct {
	Graph graphsearch.Input `json:"graph"`
}

type verifyWordArguments struct {
	Graph          graphsearch.Input `json:"graph"`
	Word           []string          `json:"word"`
	Target         []int             `json:"target"`
	VerifyShortest bool              `json:"verify_shortest"`
}

type computationSummary struct {
	Verification           *graphsearch.Verification `json:"verification,omitempty"`
	Targets                []graphsearch.Path        `json:"targets,omitempty"`
	Farthest               []graphsearch.Path        `json:"farthest,omitempty"`
	ID                     string                    `json:"id"`
	Attempt                string                    `json:"attempt"`
	Tool                   string                    `json:"tool"`
	Status                 string                    `json:"status"`
	LimitReason            string                    `json:"limit_reason,omitempty"`
	CertificateSHA256      string                    `json:"certificate_sha256"`
	ReportSHA256           string                    `json:"report_sha256"`
	Explored               int                       `json:"explored,omitempty"`
	MaxDepthReached        int                       `json:"max_depth_reached,omitempty"`
	Eccentricity           int                       `json:"eccentricity,omitempty"`
	MissingTargets         int                       `json:"missing_targets,omitempty"`
	OmittedCertificates    int                       `json:"omitted_certificates,omitempty"`
	Accepted               bool                      `json:"accepted"`
	MathematicalValidation bool                      `json:"mathematical_verification"`
}

type computationRecord struct {
	CreatedAt time.Time       `json:"created_at"`
	Request   json.RawMessage `json:"request"`
	Report    any             `json:"report"`
	ID        string          `json:"id"`
	Attempt   string          `json:"attempt"`
	Tool      string          `json:"tool"`
	Accepted  bool            `json:"accepted"`
}

func computationToolName(name string) bool {
	return name == "run_bfs" || name == "verify_word" || name == "search_longest_states"
}

func permutationSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0, "maximum": graphsearch.MaxDegree - 1}, "minItems": 1, "maxItems": graphsearch.MaxDegree}
}

func computationParameters(name string) json.RawMessage {
	graph := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"degree":               map[string]any{"type": "integer", "minimum": 1, "maximum": graphsearch.MaxDegree},
			"allow_repeated_start": map[string]any{"type": "boolean"},
			"start":                permutationSchema(),
			"bidirectional":        map[string]any{"type": "boolean"},
			"max_states":           map[string]any{"type": "integer", "minimum": 1, "maximum": graphsearch.MaxStates},
			"max_depth":            map[string]any{"type": "integer", "minimum": 0, "maximum": graphsearch.MaxDepth},
			"generators": map[string]any{"type": "array", "minItems": 1, "maxItems": graphsearch.MaxGenerators, "items": map[string]any{
				"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_]{0,31}$"}, "permutation": permutationSchema()},
				"required": []string{"name", "permutation"}, "additionalProperties": false,
			}},
		},
		"required": []string{"degree", "generators", "start", "bidirectional", "max_states", "max_depth"}, "additionalProperties": false,
	}
	properties := map[string]any{"graph": graph}
	required := []string{"graph"}
	switch name {
	case "run_bfs":
		properties["targets"] = map[string]any{"type": "array", "items": permutationSchema(), "minItems": 1, "maxItems": graphsearch.MaxResults}
		required = append(required, "targets")
	case "verify_word":
		properties["word"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": graphsearch.MaxDepth}
		properties["target"] = permutationSchema()
		properties["verify_shortest"] = map[string]any{"type": "boolean"}
		required = append(required, "word", "target", "verify_shortest")
	}
	body, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false})
	return body
}

func computationDescription(name string) string {
	switch name {
	case "run_bfs":
		return "Run bounded BFS on a permutation Cayley graph and return shortest words for explicit targets. Composition is next[i] = current[generator[i]]. This computes certificates, not a proof or acceptance."
	case "search_longest_states":
		return "Exhaust a bounded permutation Cayley graph and return the start eccentricity with farthest-state words. The result is not the graph diameter without a separate symmetry argument."
	case "verify_word":
		return "Verify a permutation word and optionally its shortestness with an independent bounded BFS. A matching word is a path certificate, not a lower-bound proof by itself."
	}
	return ""
}

func decodeComputationArguments(raw string, out any) error {
	if len(raw) > computationArgumentLimit {
		return execution.ErrLimit
	}
	return decodeJSON([]byte(raw), out)
}

func (s *Service) computationTool(a Attempt, name string, calls *int, mu *sync.Mutex, pinned *graphsearch.Input) (execution.RuntimeTool, error) {
	if !computationToolName(name) {
		return execution.RuntimeTool{}, execution.ErrUnsupported
	}
	return execution.RuntimeTool{Name: name, Description: computationDescription(name), Parameters: computationParameters(name), Validate: func(raw string) error {
		switch name {
		case "run_bfs":
			var args runBFSArguments
			if err := decodeComputationArguments(raw, &args); err != nil {
				return err
			}
			if len(args.Targets) < 1 || len(args.Targets) > graphsearch.MaxResults {
				return execution.ErrLimit
			}
			if err := graphsearch.Validate(args.Graph); err != nil {
				return err
			}
			return validatePinnedGraph(pinned, args.Graph)
		case "search_longest_states":
			var args longestStatesArguments
			if err := decodeComputationArguments(raw, &args); err != nil {
				return err
			}
			if err := graphsearch.Validate(args.Graph); err != nil {
				return err
			}
			return validatePinnedGraph(pinned, args.Graph)
		case "verify_word":
			var args verifyWordArguments
			if err := decodeComputationArguments(raw, &args); err != nil {
				return err
			}
			if len(args.Word) > graphsearch.MaxDepth {
				return execution.ErrLimit
			}
			if err := graphsearch.Validate(args.Graph); err != nil {
				return err
			}
			return validatePinnedGraph(pinned, args.Graph)
		}
		return execution.ErrUnsupported
	}, Run: func(ctx context.Context, raw string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if *calls >= 4 {
			return `{"status":"denied","reason":"computation_limit"}`, execution.ErrLimit
		}
		*calls++
		limited, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return s.runComputation(limited, a, name, raw)
	}}, nil
}

func (s *Service) runComputation(ctx context.Context, a Attempt, name, raw string) (string, error) {
	summary := computationSummary{ID: identifier("computation"), Attempt: a.ID, Tool: name, Status: "completed", Targets: []graphsearch.Path{}, Farthest: []graphsearch.Path{}}
	var report any
	var toolErr error
	switch name {
	case "run_bfs":
		var args runBFSArguments
		if err := decodeComputationArguments(raw, &args); err != nil {
			return "", err
		}
		result, err := graphsearch.FindTargets(ctx, args.Graph, args.Targets)
		if err != nil {
			return "", err
		}
		report = result
		summary.Status, summary.LimitReason = result.Status, result.LimitReason
		summary.Explored, summary.MaxDepthReached, summary.MissingTargets = result.Explored, result.MaxDepthReached, result.MissingTargets
		summary.Targets, summary.CertificateSHA256 = result.Targets, result.CertificateSHA256
	case "search_longest_states":
		var args longestStatesArguments
		if err := decodeComputationArguments(raw, &args); err != nil {
			return "", err
		}
		result, err := graphsearch.LongestStates(ctx, args.Graph)
		if err != nil {
			return "", err
		}
		report = result
		summary.Status, summary.LimitReason = result.Status, result.LimitReason
		summary.Explored, summary.MaxDepthReached, summary.Eccentricity = result.Explored, result.MaxDepthReached, result.Eccentricity
		summary.Farthest, summary.CertificateSHA256 = result.Farthest, result.CertificateSHA256
	case "verify_word":
		var args verifyWordArguments
		if err := decodeComputationArguments(raw, &args); err != nil {
			return "", err
		}
		result, err := graphsearch.VerifyWord(ctx, args.Graph, args.Word, args.Target, args.VerifyShortest)
		if err != nil {
			return "", err
		}
		report = result
		summary.Verification, summary.CertificateSHA256 = &result, result.CertificateSHA256
	default:
		return "", execution.ErrUnsupported
	}
	if summary.Status != "completed" {
		toolErr = execution.ErrLimit
	}
	record := computationRecord{ID: summary.ID, Attempt: a.ID, Tool: name, CreatedAt: time.Now().UTC(), Request: json.RawMessage(raw), Report: report, Accepted: false}
	body, err := json.Marshal(record)
	if err != nil || len(body) > 512000 {
		return "", execution.ErrLimit
	}
	summary.ReportSHA256 = bytesDigest(body)
	dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "computations", summary.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
		return "", err
	}
	summaryBody, err := json.Marshal(summary)
	if err != nil {
		return "", err
	}
	if err := atomicFile(filepath.Join(dir, "summary.json"), summaryBody); err != nil {
		return "", err
	}
	compact := compactComputation(summary, 2)
	response, err := json.Marshal(compact)
	if err != nil || len(response) > 60000 {
		return "", execution.ErrLimit
	}
	return string(response), toolErr
}

func compactComputation(summary computationSummary, keep int) computationSummary {
	total := len(summary.Targets) + len(summary.Farthest)
	if len(summary.Targets) > keep {
		summary.Targets = summary.Targets[:keep]
	}
	if len(summary.Farthest) > keep {
		summary.Farthest = summary.Farthest[:keep]
	}
	summary.OmittedCertificates = total - len(summary.Targets) - len(summary.Farthest)
	return summary
}

func (s *Service) computationSummaries(attempt string) []computationSummary {
	root := filepath.Join(s.Options.Config.DataDir, "attempts", attempt, "computations")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []computationSummary{}
	for _, entry := range entries {
		if len(out) >= 4 || !entry.IsDir() {
			continue
		}
		summaryBody, summaryErr := readBoundedFile(filepath.Join(root, entry.Name(), "summary.json"), 256000)
		reportBody, reportErr := readBoundedFile(filepath.Join(root, entry.Name(), "report.json"), 512000)
		var summary computationSummary
		if summaryErr != nil || reportErr != nil || json.Unmarshal(summaryBody, &summary) != nil ||
			summary.ID != entry.Name() || summary.Attempt != attempt || !computationToolName(summary.Tool) ||
			summary.ReportSHA256 != bytesDigest(reportBody) || summary.Accepted || summary.MathematicalValidation {
			continue
		}
		out = append(out, compactComputation(summary, 1))
	}
	return out
}
