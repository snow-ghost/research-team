package researchweb

import (
	"context"
	"time"
)

// Revision probes do not load entity collections or event history.
func (s *Store) Revision() (int, error) {
	s.probes.Add(1)
	var revision int
	if s.pg == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		err := s.db.QueryRow("SELECT json_extract(data, '$.revision') FROM current_state WHERE id=1").Scan(&revision)
		return revision, err
	}
	p := s.pg
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.ensureConnection(ctx); err != nil {
		return 0, err
	}
	err := p.conn.QueryRowContext(ctx, "SELECT revision FROM workspace WHERE id=1").Scan(&revision)
	return revision, err
}

func nextSchedulerDeadline(d Data, now time.Time) time.Time {
	var next time.Time
	add := func(t *time.Time) {
		if t != nil && t.After(now) && (next.IsZero() || t.Before(next)) {
			next = *t
		}
	}
	for _, study := range d.Studies {
		if study.Budget != nil {
			add(study.Budget.DeadlineAt)
		}
	}
	for _, a := range d.Attempts {
		if a.Status == "running" && a.RemoteWorker != "" {
			add(a.LeaseExpires)
			add(a.LeaseDeadline)
		}
	}
	return next
}

func (s *Service) SchedulerMetrics() map[string]int64 {
	return map[string]int64{"cycles": s.schedulerCycles.Load(), "revision_probes": s.Store.probes.Load(), "full_reads": s.Store.reads.Load()}
}
