package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func main() {
	input := flag.String("report", "", "Private report.json")
	out := flag.String("out", "", "New summary file")
	analysis := flag.Bool("analysis", false, "Describe task-clustered effects and native/review disagreements")
	artifacts := flag.String("artifacts-dir", "", "Private comparison directory for digest-bound review findings")
	flag.Parse()
	if err := writeOutput(*input, *out, *analysis, *artifacts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeSummary(input, out string) error {
	return writeOutput(input, out, false, "")
}

func writeOutput(input, out string, analysis bool, artifacts string) error {
	report, err := researchcompare.ReadReport(input)
	if err != nil {
		return err
	}
	var summary any
	if analysis {
		summary, err = researchcompare.Analyze(report, artifacts)
	} else {
		summary, err = researchcompare.Summarize(report)
	}
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if out == "" {
		_, err = fmt.Println(string(data))
		return err
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
