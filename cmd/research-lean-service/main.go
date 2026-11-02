package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

type leanHandler struct {
	checker leancheck.DockerChecker
	token   string
}

func main() {
	config := flag.String("config", ".research-server/lean-check.json", "Pinned Lean checker configuration")
	listen := flag.String("listen", "127.0.0.1:4189", "Loopback listen address")
	tokenEnv := flag.String("token-env", "RESEARCH_LEAN_SERVICE_TOKEN", "Service token environment variable")
	flag.Parse()
	var cfg leancheck.Config
	if err := researchweb.ReadJSON(*config, &cfg); err != nil {
		fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		fatal(err)
	}
	token := os.Getenv(*tokenEnv)
	if len(token) < 32 || len(token) > 256 {
		fatal(errors.New("service token must contain 32 to 256 characters"))
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fatal(err)
	}
	h := &leanHandler{checker: leancheck.DockerChecker{Config: cfg}, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("POST /v1/check", h.check)
	mux.HandleFunc("POST /v1/match", h.match)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Printf("Lean service: http://%s\n", listener.Addr())
	fatal(server.Serve(listener))
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (h *leanHandler) authorized(r *http.Request) bool {
	expected := "Bearer " + h.token
	return len(r.Header.Get("Authorization")) == len(expected) && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) == 1
}

func (h *leanHandler) serve(w http.ResponseWriter, r *http.Request, input, output any, run func() error) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128000))
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := run(); err != nil {
		http.Error(w, "check failed", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(output)
}

func (h *leanHandler) check(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Goal   leancheck.Goal `json:"goal"`
		Source string         `json:"source"`
	}
	var report leancheck.Report
	h.serve(w, r, &input, &report, func() error {
		dir, err := os.MkdirTemp("", "lean-service-check-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		var checkErr error
		report, checkErr = h.checker.Check(r.Context(), input.Goal, input.Source, dir)
		return checkErr
	})
}

func (h *leanHandler) match(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Goal      leancheck.Goal         `json:"goal"`
		ModuleDir string                 `json:"module_dir"`
		Module    leancheck.ModuleReport `json:"module"`
		Candidate string                 `json:"candidate"`
	}
	var report leancheck.MatchReport
	h.serve(w, r, &input, &report, func() error {
		dir, err := os.MkdirTemp("", "lean-service-match-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		var checkErr error
		report, checkErr = h.checker.MatchModule(r.Context(), input.Goal, filepath.Clean(input.ModuleDir), input.Module, input.Candidate, dir)
		return checkErr
	})
}
