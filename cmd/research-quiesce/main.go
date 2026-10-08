package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func main() {
	config := flag.String("config", "", "Private server configuration")
	flag.Parse()
	o, err := researchweb.LoadOptions(*config)
	if err == nil {
		err = quiesce(o)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Server paused; attempts, checks and library builds are idle.")
}
func quiesce(o researchweb.Options) error {
	key, err := researchweb.AccessKey(o.Config, o.Lookup)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	request := func(method, route string, body any) (researchweb.View, error) {
		var view struct {
			State researchweb.View `json:"state"`
		}
		data, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, "http://"+o.Config.Listen+"/api/"+route, bytes.NewReader(data))
		if err != nil {
			return view.State, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return view.State, errors.New("server unavailable; no backup shutdown performed")
		}
		defer resp.Body.Close()
		if resp.StatusCode == 409 {
			return view.State, researchweb.ErrConflict
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&view) != nil {
			return view.State, errors.New("server command rejected")
		}
		return view.State, nil
	}
	for ctx.Err() == nil {
		v, err := request("GET", "bootstrap", nil)
		if err != nil {
			return err
		}
		if !v.Paused {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return err
			}
			_, err = request("POST", "actions", researchweb.Action{Type: "PAUSE", ExpectedRevision: v.Revision, RequestID: "backup-" + hex.EncodeToString(id[:])})
			if errors.Is(err, researchweb.ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			continue
		}
		idle := true
		for _, a := range v.Attempts {
			switch a.Status {
			case "preparing", "running", "awaiting_worker", "cancelling":
				idle = false
			}
			if a.Status == "queued" && (a.RemoteOutcome != "not_started" || a.InputSHA256 != "") {
				idle = false
			}
		}
		for _, check := range v.Verifications {
			if check.Status == "running" {
				idle = false
			}
		}
		for _, l := range v.Library {
			if l.Status == "running" {
				idle = false
			}
		}
		if idle {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return err
			}
			_, err := request("POST", "maintenance", researchweb.MaintenanceRequest{ExpectedRevision: v.Revision, RequestID: "hold-" + hex.EncodeToString(id[:]), Confirm: true})
			if errors.Is(err, researchweb.ErrConflict) {
				continue
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return ctx.Err()
}
