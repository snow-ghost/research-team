package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func main() {
	dir := flag.String("dir", "", "Completed private comparison directory (read-only)")
	flag.Parse()
	report, err := researchcompare.AuditComparison(*dir)
	if err == nil {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
