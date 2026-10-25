//go:build linux

package researchweb

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func TestComparisonBDD_SimulatedReportCannotBeImportedAsLiveExperiment(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	if err := s.ImportComparison(ComparisonRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Confirm: true, Report: researchcompare.Report{Version: 4, Simulation: true}}); err == nil {
		t.Fatal("simulation imported as live comparison")
	}
}

func TestPostgresBDD_BackupRejectsUnsupportedSchemaBeforeWritingManifest(t *testing.T) {
	s := pgServiceFor(t, "http://127.0.0.1:1/v1")
	o := s.Options
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup")
	_, err := BackupPostgres(context.Background(), o.Config, o.Lookup, out, func(context.Context, io.Reader, io.Writer, ...string) error {
		t.Fatal("dump started for unsupported schema")
		return nil
	})
	if err == nil {
		t.Fatal("unsupported backup schema accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("incomplete manifest directory created", err)
	}
}
