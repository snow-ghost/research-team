package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/incident"
)

func main() {
	events, err := readEvents(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(events) == 0 {
		events = sampleEvents()
	}
	input := incident.IncidentInput{
		ID:      "INC-LOCAL",
		Summary: "Локальное расследование по строкам журналов.",
		Events:  events,
	}
	report, err := incident.DefaultOrchestrator().Investigate(context.Background(), input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readEvents(input *os.File) ([]incident.RawEvent, error) {
	stat, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return nil, nil
	}
	scanner := bufio.NewScanner(input)
	var events []incident.RawEvent
	base := time.Now().UTC()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		events = append(events, incident.RawEvent{
			Time:    base.Add(time.Duration(len(events)) * time.Second),
			Source:  "stdin",
			Message: line,
		})
	}
	return events, scanner.Err()
}

func sampleEvents() []incident.RawEvent {
	base := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	return []incident.RawEvent{
		{Time: base, Source: "kube-apiserver", Component: "apiserver", Message: "kube-apiserver etcd request timeout: context deadline exceeded"},
		{Time: base.Add(5 * time.Second), Source: "etcd", Node: "cp-2", Component: "etcd", Message: "etcd wal fsync took too long"},
		{Time: base.Add(8 * time.Second), Source: "kernel", Node: "cp-2", Message: "Buffer I/O error on device vdb"},
		{Time: base.Add(14 * time.Second), Source: "coredns", Component: "coredns", Message: "CoreDNS kubernetes plugin timeout to API"},
		{Time: base.Add(20 * time.Second), Source: "app", Message: "dns lookup timeout for service.default.svc.cluster.local"},
	}
}
