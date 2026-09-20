package incident

import (
	"context"
	"strings"
	"testing"
	"time"
)

type bddContext struct {
	t      *testing.T
	events []RawEvent
	report Report
}

func TestClusterIncidentInvestigationBDD(t *testing.T) {
	t.Run("ошибка диска etcd приводит к маршруту storage", func(t *testing.T) {
		ctx := givenClusterLogs(t,
			"kube-apiserver etcd request timeout: context deadline exceeded",
			"etcd wal fsync took too long on member cp-2",
			"kernel Buffer I/O error on device vdb for etcd data",
			"CoreDNS kubernetes plugin timeout to API",
			"dns lookup timeout for service.default.svc.cluster.local",
		)

		report := whenTheTeamInvestigates(ctx)

		thenPrimaryTeamIs(t, report, TeamStorage)
		thenHypothesisStatusIs(t, report, "H1", HypothesisWeakened)
		thenHypothesisStatusIs(t, report, "H3", HypothesisSupported)
		thenHypothesisStatusIs(t, report, "H6", HypothesisSupported)
		thenRecommendationMentions(t, report, "кворум etcd")
	})

	t.Run("ошибка CNI приводит к маршруту network", func(t *testing.T) {
		ctx := givenClusterLogs(t,
			"cni failed to setup network for pod: calico error",
			"pod-to-pod packet loss between worker-1 and worker-2",
			"ClusterIP service timeout from diagnostic pod",
		)

		report := whenTheTeamInvestigates(ctx)

		thenPrimaryTeamIs(t, report, TeamNetwork)
		thenHypothesisStatusIs(t, report, "H2", HypothesisSupported)
	})

	t.Run("отказ среды запуска приводит к маршруту runtime", func(t *testing.T) {
		ctx := givenClusterLogs(t,
			"kubelet reports container runtime is down",
			"RunPodSandbox failed: failed to create pod sandbox",
		)

		report := whenTheTeamInvestigates(ctx)

		thenPrimaryTeamIs(t, report, TeamRuntime)
		thenHypothesisStatusIs(t, report, "H5", HypothesisSupported)
	})

	t.Run("NodeNotReady и MemoryPressure приводят к маршруту compute", func(t *testing.T) {
		ctx := givenClusterLogs(t,
			"NodeNotReady event for worker-7",
			"node worker-7 reports MemoryPressure and OOM",
			"kubelet unhealthy on worker-7",
		)

		report := whenTheTeamInvestigates(ctx)

		thenPrimaryTeamIs(t, report, TeamCompute)
		thenHypothesisStatusIs(t, report, "H4", HypothesisSupported)
	})

	t.Run("отказ тома приложения приводит к маршруту storage", func(t *testing.T) {
		ctx := givenClusterLogs(t,
			"FailedMount: mount timeout for pvc app-data",
			"CSI reports Multi-Attach error for volume pvc-123",
		)

		report := whenTheTeamInvestigates(ctx)

		thenPrimaryTeamIs(t, report, TeamStorage)
		thenHypothesisStatusIs(t, report, "H6", HypothesisSupported)
	})
}

func givenClusterLogs(t *testing.T, lines ...string) bddContext {
	t.Helper()
	base := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	events := make([]RawEvent, 0, len(lines))
	for i, line := range lines {
		events = append(events, RawEvent{
			Time:    base.Add(time.Duration(i) * time.Second),
			Source:  "test-log",
			Message: line,
		})
	}
	return bddContext{t: t, events: events}
}

func whenTheTeamInvestigates(ctx bddContext) Report {
	ctx.t.Helper()
	report, err := DefaultOrchestrator().Investigate(context.Background(), IncidentInput{
		ID:      "INC-BDD",
		Summary: "BDD scenario",
		Events:  ctx.events,
	})
	if err != nil {
		ctx.t.Fatalf("investigation failed: %v", err)
	}
	return report
}

func thenPrimaryTeamIs(t *testing.T, report Report, want Team) {
	t.Helper()
	if report.PrimaryTeam != want {
		t.Fatalf("primary team = %q, want %q; cause: %s", report.PrimaryTeam, want, report.PrimaryCause)
	}
}

func thenHypothesisStatusIs(t *testing.T, report Report, id string, want HypothesisStatus) {
	t.Helper()
	for _, hypothesis := range allHypotheses(report) {
		if hypothesis.ID == id {
			if hypothesis.Status != want {
				t.Fatalf("hypothesis %s status = %q, want %q", id, hypothesis.Status, want)
			}
			return
		}
	}
	t.Fatalf("hypothesis %s not found", id)
}

func thenRecommendationMentions(t *testing.T, report Report, fragment string) {
	t.Helper()
	for _, action := range report.RecommendedActions {
		if strings.Contains(action, fragment) {
			return
		}
	}
	t.Fatalf("no recommendation mentions %q: %#v", fragment, report.RecommendedActions)
}

func allHypotheses(report Report) []Hypothesis {
	out := make([]Hypothesis, 0, len(report.SupportedHypotheses)+len(report.WeakenedHypotheses)+len(report.RejectedHypotheses))
	out = append(out, report.SupportedHypotheses...)
	out = append(out, report.WeakenedHypotheses...)
	out = append(out, report.RejectedHypotheses...)
	return out
}
