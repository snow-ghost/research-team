package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchcompare"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	path := flag.String("cases", "examples/evaluation-cases.json", "Evaluation set; no model is called")
	flag.Parse()
	var set researchcompare.EvaluationSet
	if err := researchweb.ReadJSON(*path, &set); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	plan, err := researchcompare.PrepareEvaluation(set)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(plan); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
