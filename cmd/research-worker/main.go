package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

type config struct {
	Server   string `json:"server"`
	Worker   string `json:"worker"`
	TokenEnv string `json:"token_env"`
	Profile  string `json:"profile"`
}
type client struct {
	config config
	key    string
	http   *http.Client
}

func (c client) post(ctx context.Context, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.config.Server, "/")+"/api/workers/"+url.PathEscape(c.config.Worker)+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid worker request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.key)
	response, err := c.http.Do(request)
	if err != nil {
		return errors.New("worker transport failed; outcome unknown")
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("worker response limit")
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("worker API status %d", response.StatusCode)
	}
	if result != nil {
		return json.Unmarshal(data, result)
	}
	return nil
}
func requestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "worker-" + hex.EncodeToString(b[:])
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "", "Local worker configuration")
	flag.Parse()
	var conf config
	if err := researchweb.ReadJSON(*path, &conf); err != nil {
		return err
	}
	u, err := url.Parse(conf.Server)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || conf.Worker == "" {
		return errors.New("invalid server origin")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()) {
		return errors.New("use HTTPS or a numeric loopback SSH tunnel")
	}
	key := os.Getenv(conf.TokenEnv)
	if len(key) < 32 {
		return errors.New("worker key missing")
	}
	var profile execution.Profile
	profilePath := conf.Profile
	if !filepath.IsAbs(profilePath) {
		profilePath = filepath.Join(filepath.Dir(*path), profilePath)
	}
	if err := researchweb.ReadJSON(profilePath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	c := client{conf, key, &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var packet *researchweb.WorkerPacket
	if err := c.post(ctx, "/claim", researchweb.WorkerRequest{RequestID: requestID()}, &packet); err != nil {
		return err
	}
	if packet == nil {
		fmt.Println("No assigned task")
		return nil
	}
	if packet.Profile != profile.ID {
		return errors.New("assigned profile differs from local profile")
	}
	// Backend credentials and command permissions remain local, never supplied by the coordinator.
	profile.Skills, profile.Limits = packet.Skills, packet.Limits
	if err := profile.Validate(); err != nil {
		return err
	}
	executor, err := execution.Build(profile, os.LookupEnv)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "research-worker-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var material struct {
		Materials []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"materials"`
	}
	if json.Unmarshal([]byte(packet.Task.Context), &material) != nil {
		return errors.New("invalid task context")
	}
	for _, m := range material.Materials {
		if (m.Path != "TASK.md" && m.Path != "Goal.lean") || len(m.Content) > 65536 {
			return errors.New("unsupported material")
		}
		matched := false
		for _, file := range packet.Files {
			if file.Path == m.Path {
				sum := sha256.Sum256([]byte(m.Content))
				if file.Bytes != len(m.Content) || file.SHA256 != hex.EncodeToString(sum[:]) {
					return errors.New("material digest mismatch")
				}
				matched = true
			}
		}
		if !matched {
			return errors.New("material has no pinned digest")
		}
		if err := os.WriteFile(filepath.Join(dir, m.Path), []byte(m.Content), 0600); err != nil {
			return err
		}
	}
	task := packet.Task
	task.Workspace = dir
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(packet.Limits.TimeoutSeconds)*time.Second)
	defer cancel()
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				err := c.post(runCtx, "/attempts/"+task.AttemptID+"/heartbeat", researchweb.WorkerRequest{Epoch: task.LeaseEpoch, Snapshot: task.Snapshot}, nil)
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	result, runErr := executor.Run(runCtx, task)
	close(done)
	<-heartbeatDone
	result.ExecutionProfileSHA256 = result.ProfileSHA256
	result.ProfileSHA256 = packet.ProfileSHA256
	request := researchweb.WorkerRequest{RequestID: requestID(), Epoch: task.LeaseEpoch, Snapshot: task.Snapshot, Result: &result}
	if err := c.post(ctx, "/attempts/"+task.AttemptID+"/result", request, nil); err != nil {
		return err
	}
	fmt.Printf("Attempt %s: %s\n", task.AttemptID, result.Status)
	return runErr
}
