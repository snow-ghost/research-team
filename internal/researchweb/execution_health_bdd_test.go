//go:build linux

package researchweb

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestExecutionHealthBDD_FirstFailureIsSanitizedAndSurvivesRestart(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	s.stopExecution("team_step", errors.New("private credential and database connection string"))
	s.stopExecution("library_completion", &pgconn.PgError{Code: "57P01", Message: "private database details"})
	h := s.ExecutionHealth()
	if h.Operational || h.CurrentStop == nil || h.CurrentStop.Stage != "team_step" || h.CurrentStop.Class != "internal_error" {
		t.Fatal("first failure lost", h)
	}
	body, _ := json.Marshal(h)
	if strings.Contains(string(body), "private") {
		t.Fatal("error details exposed")
	}
	stored, err := os.ReadFile(filepath.Join(s.Options.Config.DataDir, "execution-stop.json"))
	if err != nil || strings.Contains(string(stored), "private") {
		t.Fatal("stop record missing or unsanitized", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	next := serviceFor(t, s.Options)
	after := next.ExecutionHealth()
	if !after.Operational || after.CurrentStop != nil || after.LastStop == nil || after.LastStop.Stage != "team_step" {
		t.Fatal("restart conflated history with current status", after)
	}
	if safeErrorClass(&pgconn.PgError{Code: "57P01", Message: "private"}) != "sqlstate_57P01" {
		t.Fatal("SQLSTATE not retained")
	}
}
