package incident

import (
	"context"
	"sort"
	"time"
)

type EvidenceExtractorAgent struct{}

func (EvidenceExtractorAgent) Name() string { return "evidence-extractor" }

func (EvidenceExtractorAgent) Run(_ context.Context, c *Case) error {
	c.Evidence = ExtractEvidence(c.Input)
	return nil
}

type HypothesisAgent struct{}

func (HypothesisAgent) Name() string { return "hypothesis-builder" }

func (HypothesisAgent) Run(_ context.Context, c *Case) error {
	c.Hypotheses = BuildHypotheses(c.Evidence)
	return nil
}

type RoutingAgent struct{}

func (RoutingAgent) Name() string { return "incident-router" }

func (RoutingAgent) Run(_ context.Context, c *Case) error {
	c.Report = BuildReport(c.Input, c.Evidence, c.Hypotheses, c.EventLog)
	return nil
}

func BuildHypotheses(evidence []Evidence) []Hypothesis {
	hypotheses := []Hypothesis{
		buildDNSHypothesis(evidence),
		buildNetworkHypothesis(evidence),
		buildEtcdHypothesis(evidence),
		buildComputeHypothesis(evidence),
		buildRuntimeHypothesis(evidence),
		buildStorageHypothesis(evidence),
	}
	sort.SliceStable(hypotheses, func(i, j int) bool {
		if hypotheses[i].Score == hypotheses[j].Score {
			return hypotheses[i].ID < hypotheses[j].ID
		}
		return hypotheses[i].Score > hypotheses[j].Score
	})
	return hypotheses
}

func buildDNSHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalDNSLookupTimeout, SignalCoreDNSTimeoutAPI, SignalCoreDNSCrash)
	counter := evidenceWithAnySignal(evidence, SignalAPIServerEtcdTimeout, SignalEtcdFsyncLatency, SignalEtcdUnhealthy, SignalDiskIOError)
	score := weightedScore(support)
	status := statusFromScore(score, len(counter) > 0)
	explanation := "Ошибки DNS оцениваются как первичная причина только при отсутствии более ранних признаков отказа уровня управления, сети или хранилища."
	return Hypothesis{
		ID:          "H1",
		Title:       "Первичная проблема в DNS",
		Owner:       TeamNetwork,
		Status:      status,
		Score:       score,
		Support:     support,
		Counter:     counter,
		Checks:      dnsChecks(),
		Explanation: explanation,
	}
}

func buildNetworkHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalClusterIPTimeout, SignalCNIError, SignalKubeProxyError, SignalPodToPodFailure)
	counter := evidenceWithAnySignal(evidence, SignalAPIServerEtcdTimeout, SignalEtcdFsyncLatency, SignalDiskIOError, SignalRuntimeDown)
	score := weightedScore(support)
	status := statusFromScore(score, len(counter) > 0 && score < 4)
	return Hypothesis{
		ID:          "H2",
		Title:       "Первичная проблема в сети",
		Owner:       TeamNetwork,
		Status:      status,
		Score:       score,
		Support:     support,
		Counter:     counter,
		Checks:      networkChecks(),
		Explanation: "Сетевая гипотеза поддерживается признаками отказа CNI, правил служб, межузловой связности или ClusterIP.",
	}
}

func buildEtcdHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalAPIServerEtcdTimeout, SignalEtcdFsyncLatency, SignalEtcdUnhealthy)
	score := weightedScore(support)
	status := statusFromScore(score, false)
	return Hypothesis{
		ID:          "H3",
		Title:       "Первичная проблема в etcd",
		Owner:       TeamStorage,
		Status:      status,
		Score:       score,
		Support:     support,
		Checks:      etcdChecks(),
		Explanation: "etcd рассматривается как первичная причина, если apiserver фиксирует тайм-ауты к etcd или сам etcd сообщает о задержке записи, отсутствии лидера или потере кворума.",
	}
}

func buildComputeHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalNodeNotReady, SignalMemoryPressure, SignalKubeletFailure)
	counter := evidenceWithAnySignal(evidence, SignalDiskIOError, SignalRuntimeDown, SignalCNIError)
	score := weightedScore(support)
	status := statusFromScore(score, len(counter) > 0 && score < 4)
	return Hypothesis{
		ID:          "H4",
		Title:       "Первичная проблема на вычислительном узле",
		Owner:       TeamCompute,
		Status:      status,
		Score:       score,
		Support:     support,
		Counter:     counter,
		Checks:      computeChecks(),
		Explanation: "Вычислительная гипотеза поддерживается отказом узла, kubelet, нехваткой памяти или давлением на системные ресурсы.",
	}
}

func buildRuntimeHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalRuntimeDown, SignalPodSandboxFailure, SignalImagePullFailure)
	counter := evidenceWithAnySignal(evidence, SignalCNIError, SignalDiskIOError)
	score := weightedScore(support)
	status := statusFromScore(score, len(counter) > 0 && score < 4)
	return Hypothesis{
		ID:          "H5",
		Title:       "Первичная проблема в среде запуска контейнеров",
		Owner:       TeamRuntime,
		Status:      status,
		Score:       score,
		Support:     support,
		Counter:     counter,
		Checks:      runtimeChecks(),
		Explanation: "Среда запуска является владельцем, если отказ связан с containerd, CRI-O, созданием pod sandbox или загрузкой образов.",
	}
}

func buildStorageHypothesis(evidence []Evidence) Hypothesis {
	support := evidenceWithAnySignal(evidence, SignalDiskIOError, SignalReadOnlyFilesystem, SignalDiskFull, SignalVolumeMountFailure, SignalEtcdFsyncLatency)
	score := weightedScore(support)
	status := statusFromScore(score, false)
	return Hypothesis{
		ID:          "H6",
		Title:       "Первичная проблема в хранилище",
		Owner:       TeamStorage,
		Status:      status,
		Score:       score,
		Support:     support,
		Checks:      storageChecks(),
		Explanation: "Хранилище является владельцем, если есть ошибки устройства, файловой системы, тома, CSI или задержки записи etcd.",
	}
}

func weightedScore(evidence []Evidence) int {
	score := 0
	for _, item := range evidence {
		switch item.Strength {
		case StrengthHigh:
			score += 3
		case StrengthMedium:
			score += 2
		default:
			score++
		}
	}
	return score
}

func statusFromScore(score int, hasCounter bool) HypothesisStatus {
	switch {
	case score == 0:
		return HypothesisRejected
	case hasCounter:
		return HypothesisWeakened
	case score >= 3:
		return HypothesisSupported
	default:
		return HypothesisProposed
	}
}

func dnsChecks() []Check {
	return []Check{
		{ID: "DNS-1", Description: "Проверить поды CoreDNS и их журналы.", Signals: []Signal{SignalCoreDNSCrash, SignalCoreDNSTimeoutAPI}},
		{ID: "DNS-2", Description: "Сравнить запрос имени службы и прямой запрос ClusterIP.", Signals: []Signal{SignalDNSLookupTimeout, SignalClusterIPTimeout}},
	}
}

func networkChecks() []Check {
	return []Check{
		{ID: "NET-1", Description: "Проверить pod-to-pod и node-to-node связность.", Signals: []Signal{SignalPodToPodFailure}},
		{ID: "NET-2", Description: "Проверить CNI и правила служб.", Signals: []Signal{SignalCNIError, SignalKubeProxyError}},
	}
}

func etcdChecks() []Check {
	return []Check{
		{ID: "ETCD-1", Description: "Проверить health и status участников etcd.", Signals: []Signal{SignalEtcdUnhealthy}},
		{ID: "ETCD-2", Description: "Сопоставить журналы apiserver с журналами etcd.", Signals: []Signal{SignalAPIServerEtcdTimeout, SignalEtcdFsyncLatency}},
	}
}

func computeChecks() []Check {
	return []Check{
		{ID: "CMP-1", Description: "Проверить NodeReady, MemoryPressure, DiskPressure и журналы kubelet.", Signals: []Signal{SignalNodeNotReady, SignalMemoryPressure, SignalKubeletFailure}},
	}
}

func runtimeChecks() []Check {
	return []Check{
		{ID: "RUN-1", Description: "Проверить containerd или CRI-O, crictl и ошибки pod sandbox.", Signals: []Signal{SignalRuntimeDown, SignalPodSandboxFailure, SignalImagePullFailure}},
	}
}

func storageChecks() []Check {
	return []Check{
		{ID: "STO-1", Description: "Проверить ошибки диска, режим файловой системы и заполнение томов.", Signals: []Signal{SignalDiskIOError, SignalReadOnlyFilesystem, SignalDiskFull}},
		{ID: "STO-2", Description: "Проверить CSI, подключение томов и задержку записи etcd.", Signals: []Signal{SignalVolumeMountFailure, SignalEtcdFsyncLatency}},
	}
}

func newEvent(agent, action, message string) EventRecord {
	return EventRecord{Time: time.Now().UTC(), Agent: agent, Action: action, Message: message}
}
