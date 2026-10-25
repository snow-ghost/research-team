package researchcompare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestComparisonBDD_ExplicitAliasResolutionKeepsOriginalConfiguration(t *testing.T) {
	c := comparisonFixture(t)
	c.Version = 3
	for i := range c.Cases {
		c.Cases[i].Class = fmt.Sprint(i)
	}
	original := c.Lean.Toolchain
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(original, alias); err != nil {
		t.Fatal(err)
	}
	c.Lean.Toolchain = alias
	dir := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunComparison(ctx, c, dir, Runner{}); err == nil {
		t.Fatal("implicit resolution accepted")
	}
	report, err := RunComparison(ctx, c, dir, Runner{ResolveLeanPaths: true, Checker: checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source), AuditSHA256: leancheck.AuditDigest()}, nil
	})})
	if err == nil || report.ConfigurationSHA256 != leancheck.Digest(c) || len(report.EnvironmentResolutions) != 1 || c.Lean.Toolchain != alias {
		t.Fatal("configuration was rewritten", report, err)
	}
}
