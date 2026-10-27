//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/reportdelivery"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func run() error {
	config := flag.String("server-config", "", "Trusted local server configuration")
	outbox := flag.String("outbox", "", "Private delivery directory outside the server state")
	noteFile := flag.String("enqueue-note", "", "JSON technical note: target, content, conditions")
	deliveryKey := flag.String("delivery-key", "", "Stable producer identity for one immutable report")
	enqueueOnly := flag.Bool("enqueue-only", false, "Persist a note without contacting the server")
	watch := flag.Bool("watch", false, "Deliver pending technical notes until interrupted")
	flag.Parse()
	if *outbox == "" || *enqueueOnly && *noteFile == "" {
		return errors.New("require -outbox and a note for -enqueue-only")
	}
	if *noteFile != "" {
		var note reportdelivery.Note
		if err := researchweb.ReadJSON(*noteFile, &note); err != nil {
			return err
		}
		var entry reportdelivery.Entry
		var err error
		if *deliveryKey == "" {
			entry, err = reportdelivery.Queue(*outbox, note)
		} else {
			entry, err = reportdelivery.QueueOnce(*outbox, *deliveryKey, note)
		}
		if err != nil {
			return err
		}
		fmt.Printf("{\"queued\":%q}\n", entry.ID)
	}
	if *enqueueOnly {
		return nil
	}
	if *config == "" {
		return errors.New("require -server-config for delivery")
	}
	o, err := researchweb.LoadOptions(*config)
	if err != nil {
		return err
	}
	key := ""
	if o.Config.TokenEnv != "" {
		key, _ = o.Lookup(o.Config.TokenEnv)
	} else {
		body, readErr := os.ReadFile(filepath.Join(o.Config.DataDir, "access-token"))
		if readErr != nil {
			return errors.New("operator credential unavailable")
		}
		key = strings.TrimSpace(string(body))
	}
	host, port, err := net.SplitHostPort(o.Config.Listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "localhost" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	client, err := reportdelivery.NewClient("http://"+net.JoinHostPort(host, port), key, nil)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	last := map[string]string{}
	for {
		entries, err := reportdelivery.Pending(*outbox)
		if err != nil {
			return err
		}
		pending, rejected := false, false
		for _, entry := range entries {
			if ctx.Err() != nil {
				return nil
			}
			result, deliveryErr := client.Deliver(ctx, *outbox, entry.ID)
			if deliveryErr != nil && !errors.Is(deliveryErr, reportdelivery.ErrPending) && !errors.Is(deliveryErr, reportdelivery.ErrRejected) {
				return deliveryErr
			}
			if errors.Is(deliveryErr, reportdelivery.ErrPending) {
				pending = true
			}
			rejected = rejected || errors.Is(deliveryErr, reportdelivery.ErrRejected)
			if result.ID == "" {
				result = entry
			}
			state := result.Status + ":" + result.LastClass
			if last[entry.ID] != state {
				body, _ := json.Marshal(map[string]any{"id": entry.ID, "status": result.Status, "class": result.LastClass})
				fmt.Println(string(body))
				last[entry.ID] = state
			}
		}
		if !*watch {
			if rejected {
				return reportdelivery.ErrRejected
			}
			if pending {
				return reportdelivery.ErrPending
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
