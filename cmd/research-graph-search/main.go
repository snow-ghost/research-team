package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snow-ghost/research-team/internal/graphsearch"
)

func main() {
	degree := flag.Int("degree", 11, "Number of positions")
	copies := flag.Int("copies", 2, "Number of copies of the final symbol")
	maxStates := flag.Int("max-states", graphsearch.OfflineMaxStates, "Maximum exact component size")
	out := flag.String("out", "", "New JSON artifact path; stdout when empty")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := graphsearch.OfflineRepeatedWrapped4(ctx, *degree, *copies, *maxStates)
	if err != nil {
		fatal(err)
	}
	if *out == "" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		fatal(encoder.Encode(report))
		return
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fatal(err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_ = file.Close()
		fatal(err)
	}
	fatal(file.Close())
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
