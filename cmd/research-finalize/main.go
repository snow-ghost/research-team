package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	report := flag.String("report", "", "Private report.json")
	out := flag.String("out", "", "Private durable delivery directory")
	server := flag.String("server", "", "HTTPS origin or numeric loopback origin")
	token := flag.String("token-env", "RESEARCH_API_TOKEN", "Environment variable containing operator API key")
	pin := flag.String("configuration-sha256", "", "Approved configuration digest")
	watch := flag.Bool("watch", false, "Wait for complete report; retry only delivery, never model execution")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		r, err := researchcompare.ReadReport(*report)
		if err == nil {
			f := researchcompare.Finalizer{Server: *server, Token: os.Getenv(*token), Pin: *pin, Output: *out, Artifacts: filepath.Dir(*report), Client: client}
			var d researchcompare.Delivery
			d, err = f.Deliver(ctx, r)
			if err == nil && d.State == "acknowledged" {
				b, _ := json.Marshal(map[string]any{"state": d.State, "comparison": d.Comparison, "report_sha256": d.ReportSHA256, "model_calls": 0, "mathematical_acceptance": false})
				fmt.Println(string(b))
				return nil
			}
		}
		if !*watch {
			if err == nil {
				err = errors.New("report delivery is pending; rerun to reconcile, without model execution")
			}
			return err
		}
		if err != nil && err != researchcompare.ErrIncomplete {
			fmt.Fprintln(os.Stderr, "Finalization pending; no model work started")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
