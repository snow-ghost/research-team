package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/proofreview"
)

func readJSON(file string, value any) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func run() error {
	caseID := flag.String("case", "", "Reference case ID")
	report := flag.String("report", "", "Decoded proof-review report JSON")
	annotation := flag.String("annotation", "", "Complete source-bound annotations JSON")
	flag.Parse()
	if *caseID == "" || *report == "" || *annotation == "" {
		return fmt.Errorf("require -case, -report and -annotation; no model calls are made")
	}
	var review proofreview.Report
	var labels proofreview.ReviewAnnotation
	if err := readJSON(*report, &review); err != nil {
		return err
	}
	if err := readJSON(*annotation, &labels); err != nil {
		return err
	}
	for _, c := range proofreview.CalibrationCases() {
		if c.ID != *caseID {
			continue
		}
		score, err := proofreview.ScoreCalibration(c, review, labels)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(score)
	}
	return fmt.Errorf("unknown reference case")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
