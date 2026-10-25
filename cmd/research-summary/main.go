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
	input := flag.String("report", "", "Private report.json")
	out := flag.String("out", "", "New summary file")
	flag.Parse()
	if err := writeSummary(*input, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeSummary(input, out string) error {
	var report researchcompare.Report
	if err := researchweb.ReadJSON(input, &report); err != nil {
		return err
	}
	summary, err := researchcompare.Summarize(report)
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
