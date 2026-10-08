package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snow-ghost/research-team/internal/researchcompare"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	config := flag.String("config", "", "Private comparison configuration")
	server := flag.String("server-config", "", "Trusted server configuration for credential references")
	out := flag.String("out", "", "New private result directory")
	confirm := flag.Bool("confirm", false, "Authorize the configured comparison (v1: 96, v2: 216 requests)")
	resume := flag.Bool("resume", false, "Continue only unstarted runs; interrupted requests are held and never replayed")
	flag.Parse()
	if !*confirm || *config == "" || *server == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "Pass -config, -server-config, -out and -confirm.")
		os.Exit(2)
	}
	var c researchcompare.Config
	err := researchweb.ReadJSON(*config, &c)
	o, loadErr := researchweb.LoadOptions(*server)
	if err == nil {
		err = loadErr
	}
	if err == nil {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		r, runErr := researchcompare.RunComparison(ctx, c, *out, researchcompare.Runner{Lookup: o.Lookup, Resume: *resume})
		err = runErr
		if err == nil {
			fmt.Printf("Completed runs: %d. Maximum requests: %d. Results are not operator acceptance.\n", len(r.Runs), r.MaxRequests)
			return
		}
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
