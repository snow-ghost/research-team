package incident

import (
	"context"
	"time"
)

type Team string

const (
	TeamUnknown Team = "unknown"
	TeamCompute Team = "compute"
	TeamNetwork Team = "network"
	TeamStorage Team = "storage"
	TeamRuntime Team = "runtime"
)

type Category string

const (
	CategoryDNS          Category = "dns"
	CategoryNetwork      Category = "network"
	CategoryEtcd         Category = "etcd"
	CategoryStorage      Category = "storage"
	CategoryCompute      Category = "compute"
	CategoryRuntime      Category = "runtime"
	CategoryControlPlane Category = "control-plane"
)

type Strength string

const (
	StrengthLow    Strength = "low"
	StrengthMedium Strength = "medium"
	StrengthHigh   Strength = "high"
)

type Signal string

const (
	SignalDNSLookupTimeout     Signal = "dns_lookup_timeout"
	SignalCoreDNSTimeoutAPI    Signal = "coredns_api_timeout"
	SignalCoreDNSCrash         Signal = "coredns_crash"
	SignalClusterIPTimeout     Signal = "cluster_ip_timeout"
	SignalCNIError             Signal = "cni_error"
	SignalKubeProxyError       Signal = "kube_proxy_error"
	SignalPodToPodFailure      Signal = "pod_to_pod_failure"
	SignalAPIServerEtcdTimeout Signal = "apiserver_etcd_timeout"
	SignalEtcdFsyncLatency     Signal = "etcd_fsync_latency"
	SignalEtcdUnhealthy        Signal = "etcd_unhealthy"
	SignalDiskIOError          Signal = "disk_io_error"
	SignalReadOnlyFilesystem   Signal = "read_only_filesystem"
	SignalDiskFull             Signal = "disk_full"
	SignalVolumeMountFailure   Signal = "volume_mount_failure"
	SignalNodeNotReady         Signal = "node_not_ready"
	SignalMemoryPressure       Signal = "memory_pressure"
	SignalKubeletFailure       Signal = "kubelet_failure"
	SignalRuntimeDown          Signal = "runtime_down"
	SignalPodSandboxFailure    Signal = "pod_sandbox_failure"
	SignalImagePullFailure     Signal = "image_pull_failure"
)

type RawEvent struct {
	Time      time.Time `json:"time"`
	Source    string    `json:"source"`
	Node      string    `json:"node,omitempty"`
	Component string    `json:"component,omitempty"`
	Message   string    `json:"message"`
}

type Evidence struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Source    string    `json:"source"`
	Node      string    `json:"node,omitempty"`
	Component string    `json:"component,omitempty"`
	Message   string    `json:"message"`
	Category  Category  `json:"category"`
	Signal    Signal    `json:"signal"`
	Strength  Strength  `json:"strength"`
}

type HypothesisStatus string

const (
	HypothesisProposed  HypothesisStatus = "proposed"
	HypothesisSupported HypothesisStatus = "supported"
	HypothesisWeakened  HypothesisStatus = "weakened"
	HypothesisRejected  HypothesisStatus = "rejected"
)

type Check struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Signals     []Signal `json:"signals"`
}

type Hypothesis struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Owner       Team             `json:"owner"`
	Status      HypothesisStatus `json:"status"`
	Score       int              `json:"score"`
	Support     []Evidence       `json:"support,omitempty"`
	Counter     []Evidence       `json:"counter,omitempty"`
	Checks      []Check          `json:"checks,omitempty"`
	Explanation string           `json:"explanation"`
}

type Finding struct {
	Title    string   `json:"title"`
	Details  string   `json:"details"`
	Evidence []string `json:"evidence"`
}

type EventRecord struct {
	Time    time.Time `json:"time"`
	Agent   string    `json:"agent"`
	Action  string    `json:"action"`
	Message string    `json:"message"`
}

type Report struct {
	IncidentID            string        `json:"incident_id"`
	Summary               string        `json:"summary"`
	PrimaryTeam           Team          `json:"primary_team"`
	SecondaryTeams        []Team        `json:"secondary_teams,omitempty"`
	PrimaryCause          string        `json:"primary_cause"`
	Confidence            string        `json:"confidence"`
	SupportedHypotheses   []Hypothesis  `json:"supported_hypotheses,omitempty"`
	WeakenedHypotheses    []Hypothesis  `json:"weakened_hypotheses,omitempty"`
	RejectedHypotheses    []Hypothesis  `json:"rejected_hypotheses,omitempty"`
	Findings              []Finding     `json:"findings,omitempty"`
	RecommendedActions    []string      `json:"recommended_actions,omitempty"`
	NotRecommendedActions []string      `json:"not_recommended_actions,omitempty"`
	EventLog              []EventRecord `json:"event_log,omitempty"`
}

type IncidentInput struct {
	ID      string     `json:"id"`
	Summary string     `json:"summary"`
	Events  []RawEvent `json:"events"`
}

type Case struct {
	Input      IncidentInput
	Evidence   []Evidence
	Hypotheses []Hypothesis
	Report     Report
	EventLog   []EventRecord
}

type Agent interface {
	Name() string
	Run(ctx context.Context, c *Case) error
}
