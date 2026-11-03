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

type workspaceClient struct {
	options researchweb.Options
	key     string
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
}

func main() {
	config := flag.String("config", "", "Private server configuration")
	release := flag.Bool("release", false, "Release maintenance and resume the prior queue")
	flag.Parse()
	o, err := researchweb.LoadMaintenanceOptions(*config, nil)
	if err == nil {
		if *release {
			err = releaseMaintenance(o)
		} else {
			err = quiesce(o)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *release {
		fmt.Println("Server maintenance released; prior queue resumed.")
	} else {
		fmt.Println("Server paused; attempts, checks and library builds are idle.")
	}
}

func newWorkspaceClient(o researchweb.Options) (*workspaceClient, error) {
	key, err := researchweb.AccessKey(o.Config, o.Lookup)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	client := &workspaceClient{options: o, key: key, client: &http.Client{Timeout: 30 * time.Second}, ctx: ctx, cancel: cancel}
	return client, nil
}

func (c *workspaceClient) close() {
	c.cancel()
}

func (c *workspaceClient) request(method, route string, body any) (researchweb.View, error) {
	var view struct {
		State researchweb.View `json:"state"`
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(c.ctx, method, "http://"+c.options.Config.Listen+"/api/"+route, bytes.NewReader(data))
	if err != nil {
		return view.State, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return view.State, errors.New("server unavailable; no backup shutdown performed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return view.State, researchweb.ErrConflict
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&view) != nil {
		return view.State, fmt.Errorf("server command rejected: %s /api/%s", method, route)
	}
	return view.State, nil
}

func requestID(prefix string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(id[:]), nil
}

func quiesce(o researchweb.Options) error {
	client, err := newWorkspaceClient(o)
	if err != nil {
		return err
	}
	defer client.close()
	for client.ctx.Err() == nil {
		v, err := client.request("GET", "bootstrap", nil)
		if err != nil {
			return err
		}
		if !v.Paused {
			id, err := requestID("backup-")
			if err != nil {
				return err
			}
			_, err = client.request("POST", "actions", researchweb.Action{Type: "PAUSE", ExpectedRevision: v.Revision, RequestID: id})
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
			id, err := requestID("hold-")
			if err != nil {
				return err
			}
			_, err = client.request("POST", "maintenance", researchweb.MaintenanceRequest{ExpectedRevision: v.Revision, RequestID: id, Confirm: true})
			if errors.Is(err, researchweb.ErrConflict) {
				continue
			}
			return err
		}
		select {
		case <-client.ctx.Done():
			return client.ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return client.ctx.Err()
}

func releaseMaintenance(o researchweb.Options) error {
	client, err := newWorkspaceClient(o)
	if err != nil {
		return err
	}
	defer client.close()
	for client.ctx.Err() == nil {
		v, err := client.request("GET", "bootstrap", nil)
		if err != nil {
			return err
		}
		if v.Maintenance != nil {
			id, err := requestID("release-")
			if err != nil {
				return err
			}
			_, err = client.request("POST", "maintenance", researchweb.MaintenanceRequest{Kind: "release", ExpectedRevision: v.Revision, RequestID: id, Confirm: true})
			if errors.Is(err, researchweb.ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			continue
		}
		if v.Paused {
			id, err := requestID("resume-")
			if err != nil {
				return err
			}
			_, err = client.request("POST", "actions", researchweb.Action{Type: "PAUSE", ExpectedRevision: v.Revision, RequestID: id})
			if errors.Is(err, researchweb.ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			continue
		}
		return nil
	}
	return client.ctx.Err()
}
