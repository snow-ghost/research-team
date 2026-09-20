package incident

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type signalRule struct {
	signal   Signal
	category Category
	strength Strength
	patterns []*regexp.Regexp
}

func newRule(signal Signal, category Category, strength Strength, patterns ...string) signalRule {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		compiled = append(compiled, regexp.MustCompile(pattern))
	}
	return signalRule{signal: signal, category: category, strength: strength, patterns: compiled}
}

var defaultRules = []signalRule{
	newRule(SignalDNSLookupTimeout, CategoryDNS, StrengthHigh,
		`(?i)dns.*(timeout|timed out)`,
		`(?i)lookup.*(timeout|timed out)`,
		`(?i)temporary failure in name resolution`,
		`(?i)server misbehaving`,
	),
	newRule(SignalCoreDNSTimeoutAPI, CategoryDNS, StrengthMedium,
		`(?i)coredns.*(api|kubernetes).*(timeout|timed out)`,
		`(?i)plugin/kubernetes.*(timeout|timed out)`,
		`(?i)coredns.*failed to list.*kubernetes`,
	),
	newRule(SignalCoreDNSCrash, CategoryDNS, StrengthHigh,
		`(?i)coredns.*crash`,
		`(?i)crashloopbackoff.*coredns`,
	),
	newRule(SignalClusterIPTimeout, CategoryNetwork, StrengthMedium,
		`(?i)clusterip.*(timeout|timed out|unreachable)`,
		`(?i)service.*(timeout|timed out).*cluster`,
	),
	newRule(SignalCNIError, CategoryNetwork, StrengthHigh,
		`(?i)cni.*(error|failed)`,
		`(?i)network plugin.*(error|failed)`,
		`(?i)(calico|cilium|flannel).*(error|failed|unreachable)`,
	),
	newRule(SignalKubeProxyError, CategoryNetwork, StrengthHigh,
		`(?i)kube-proxy.*(error|failed)`,
		`(?i)iptables.*restore.*failed`,
		`(?i)ipvs.*(error|failed)`,
	),
	newRule(SignalPodToPodFailure, CategoryNetwork, StrengthHigh,
		`(?i)pod-to-pod.*(fail|unreachable|packet loss)`,
		`(?i)node-to-node.*(fail|unreachable|packet loss)`,
		`(?i)inter-node.*(fail|unreachable|packet loss)`,
	),
	newRule(SignalAPIServerEtcdTimeout, CategoryEtcd, StrengthHigh,
		`(?i)(kube-)?apiserver.*etcd.*(timeout|timed out|context deadline exceeded)`,
		`(?i)context deadline exceeded.*etcd`,
	),
	newRule(SignalEtcdFsyncLatency, CategoryEtcd, StrengthHigh,
		`(?i)etcd.*fsync`,
		`(?i)wal.*fsync`,
		`(?i)apply request took too long`,
		`(?i)commit.*latency`,
	),
	newRule(SignalEtcdUnhealthy, CategoryEtcd, StrengthHigh,
		`(?i)etcd.*unhealthy`,
		`(?i)lost quorum`,
		`(?i)no leader`,
		`(?i)raft.*unavailable`,
	),
	newRule(SignalDiskIOError, CategoryStorage, StrengthHigh,
		`(?i)i/o error`,
		`(?i)input/output error`,
		`(?i)buffer i/o error`,
		`(?i)blk_update_request`,
	),
	newRule(SignalReadOnlyFilesystem, CategoryStorage, StrengthHigh,
		`(?i)read-only file system`,
	),
	newRule(SignalDiskFull, CategoryStorage, StrengthMedium,
		`(?i)no space left`,
		`(?i)diskpressure`,
	),
	newRule(SignalVolumeMountFailure, CategoryStorage, StrengthHigh,
		`(?i)multi-attach`,
		`(?i)mount.*(timeout|timed out|failed)`,
		`(?i)failedmount`,
		`(?i)unable to attach`,
	),
	newRule(SignalNodeNotReady, CategoryCompute, StrengthHigh,
		`(?i)nodenotready`,
		`(?i)node.*notready`,
		`(?i)node.*not ready`,
	),
	newRule(SignalMemoryPressure, CategoryCompute, StrengthHigh,
		`(?i)memorypressure`,
		`(?i)out of memory`,
		`(?i)\\boom\\b`,
	),
	newRule(SignalKubeletFailure, CategoryCompute, StrengthMedium,
		`(?i)kubelet.*(failed|not responding|unhealthy)`,
	),
	newRule(SignalRuntimeDown, CategoryRuntime, StrengthHigh,
		`(?i)container runtime is down`,
		`(?i)containerd.*(down|failed|unhealthy)`,
		`(?i)cri-o.*(down|failed|unhealthy)`,
	),
	newRule(SignalPodSandboxFailure, CategoryRuntime, StrengthHigh,
		`(?i)failed to create pod sandbox`,
		`(?i)runpodsandbox.*(failed|error)`,
	),
	newRule(SignalImagePullFailure, CategoryRuntime, StrengthMedium,
		`(?i)imagepullbackoff`,
		`(?i)errimagepull`,
		`(?i)failed to pull image`,
	),
}

func ExtractEvidence(input IncidentInput) []Evidence {
	evidence := make([]Evidence, 0, len(input.Events))
	nextID := 1
	for _, event := range input.Events {
		for _, rule := range defaultRules {
			if !rule.matches(event) {
				continue
			}
			evidence = append(evidence, Evidence{
				ID:        fmt.Sprintf("E-%03d", nextID),
				Time:      event.Time,
				Source:    event.Source,
				Node:      event.Node,
				Component: event.Component,
				Message:   strings.TrimSpace(event.Message),
				Category:  rule.category,
				Signal:    rule.signal,
				Strength:  rule.strength,
			})
			nextID++
		}
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Time.Equal(evidence[j].Time) {
			return evidence[i].ID < evidence[j].ID
		}
		return evidence[i].Time.Before(evidence[j].Time)
	})
	return evidence
}

func (r signalRule) matches(event RawEvent) bool {
	text := strings.Join([]string{event.Source, event.Node, event.Component, event.Message}, " ")
	for _, pattern := range r.patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func evidenceWithSignal(evidence []Evidence, signal Signal) []Evidence {
	var out []Evidence
	for _, item := range evidence {
		if item.Signal == signal {
			out = append(out, item)
		}
	}
	return out
}

func evidenceWithAnySignal(evidence []Evidence, signals ...Signal) []Evidence {
	var out []Evidence
	allowed := make(map[Signal]struct{}, len(signals))
	for _, signal := range signals {
		allowed[signal] = struct{}{}
	}
	for _, item := range evidence {
		if _, ok := allowed[item.Signal]; ok {
			out = append(out, item)
		}
	}
	return out
}

func hasSignal(evidence []Evidence, signal Signal) bool {
	return len(evidenceWithSignal(evidence, signal)) > 0
}
