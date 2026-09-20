package incident

import (
	"context"
	"errors"
	"fmt"
)

type Orchestrator struct {
	agents []Agent
}

func NewOrchestrator(agents ...Agent) (*Orchestrator, error) {
	if len(agents) == 0 {
		return nil, errors.New("at least one agent is required")
	}
	return &Orchestrator{agents: append([]Agent(nil), agents...)}, nil
}

func DefaultOrchestrator() *Orchestrator {
	orch, err := NewOrchestrator(DefaultTeam()...)
	if err != nil {
		panic(err)
	}
	return orch
}

func (o *Orchestrator) Investigate(ctx context.Context, input IncidentInput) (Report, error) {
	if input.ID == "" {
		return Report{}, errors.New("incident id is required")
	}
	if len(input.Events) == 0 {
		return Report{}, errors.New("at least one raw event is required")
	}
	c := &Case{Input: input}
	for _, agent := range o.agents {
		c.EventLog = append(c.EventLog, newEvent(agent.Name(), "start", "agent started"))
		if err := agent.Run(ctx, c); err != nil {
			c.EventLog = append(c.EventLog, newEvent(agent.Name(), "error", err.Error()))
			return Report{}, fmt.Errorf("%s: %w", agent.Name(), err)
		}
		c.EventLog = append(c.EventLog, newEvent(agent.Name(), "done", "agent completed"))
	}
	c.Report.EventLog = append([]EventRecord(nil), c.EventLog...)
	return c.Report, nil
}
