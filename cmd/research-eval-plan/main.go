package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchcompare"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	path := flag.String("cases", "examples/evaluation-cases.json", "Evaluation set; no model is called")
	rehearse := flag.String("rehearse-out", "", "New private directory for a simulated full comparison; no model, Lean or credentials")
	resume := flag.Bool("resume", false, "Resume only an existing simulated rehearsal")
	flag.Parse()
	if *resume && *rehearse == "" {
		fmt.Fprintln(os.Stderr, "-resume requires -rehearse-out.")
		os.Exit(2)
	}
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
	if *rehearse != "" {
		value, err := researchcompare.RehearseEvaluation(context.Background(), set, *rehearse, *resume)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(plan); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
