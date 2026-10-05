package researchweb

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"
)

type StorageHealth struct {
	Driver      string     `json:"driver"`
	Available   bool       `json:"available"`
	Generation  int        `json:"generation"`
	LastFailure string     `json:"last_failure,omitempty"`
	RecoveredAt *time.Time `json:"recovered_at,omitempty"`
}

// Never replay an ambiguous write. Reconnection is performed before a new transaction.
func (p *postgresStore) ensureConnection(ctx context.Context) error {
	if p.closed {
		return errors.New("storage closed")
	}
	if p.conn != nil && p.conn.PingContext(ctx) == nil {
		return nil
	}
	p.lastFailure = "connection_lost"
	if p.conn != nil {
		_ = p.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = p.conn.Close()
		p.conn = nil
	}
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return errors.New("storage connection unavailable")
	}
	var owned bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", postgresOwnerLock).Scan(&owned); err != nil || !owned {
		_ = conn.Close()
		p.lastFailure = "ownership_unavailable"
		return errors.New("storage coordinator ownership unavailable")
	}
	p.conn = conn
	if err := p.migrate(ctx); err != nil {
		p.discardConnection()
		p.lastFailure = "schema_unavailable"
		return errors.New("storage schema unavailable")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		p.discardConnection()
		return errors.New("storage recovery unavailable")
	}
	defer tx.Rollback()
	fail := func(message string) error { _ = tx.Rollback(); p.discardConnection(); return errors.New(message) }
	before, err := readCurrent(ctx, tx)
	if err != nil {
		return fail("storage recovery state unavailable")
	}
	after := before
	// readCurrent returns owned values; copy the slices that recovery changes.
	after.Attempts = append([]Attempt(nil), before.Attempts...)
	after.Tasks = append([]Task(nil), before.Tasks...)
	after.Teams = append([]ResearchTeam(nil), before.Teams...)
	after.Cycles = append([]Cycle(nil), before.Cycles...)
	after.Verifications = append([]Verification(nil), before.Verifications...)
	after.Library = append([]LibraryEntry(nil), before.Library...)
	interruptStorageWork(&after)
	after.Revision++
	if err = writeDelta(ctx, tx, before, after, eventFor(&after, "Восстановлено соединение PostgreSQL; продолжение требует подтверждения", "", "", "server")); err != nil {
		return fail("storage recovery journal unavailable")
	}
	if err = tx.Commit(); err != nil {
		return fail("storage recovery outcome unknown")
	}
	p.generation++
	now := time.Now().UTC()
	p.recoveredAt = &now
	return nil
}

func (p *postgresStore) discardConnection() {
	if p.conn != nil {
		_ = p.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = p.conn.Close()
		p.conn = nil
	}
}

func interruptStorageWork(d *Data) {
	d.Paused = true
	for i := range d.Attempts {
		a := &d.Attempts[i]
		if active(a.Status) {
			a.Status = "interrupted"
			a.StopCause = "storage_recovery"
			if a.RemoteOutcome != "not_started" {
				a.RemoteOutcome = "unknown"
			}
			if task := d.task(a.TaskID); task != nil {
				task.State = "interrupted"
			}
		}
	}
	for i := range d.Teams {
		if teamActive(d.Teams[i].Status) {
			d.Teams[i].Status = "interrupted"
			d.Teams[i].Reason = "Подключение базы восстановлено; проверьте исходы и разрешите продолжение."
		}
	}
	for i := range d.Cycles {
		if cycleActive(d.Cycles[i].Status) {
			d.Cycles[i].Status = "interrupted"
			d.Cycles[i].Reason = "Подключение базы восстановлено; проверьте исходы и разрешите продолжение."
		}
	}
	for i := range d.Verifications {
		if d.Verifications[i].Status == "queued" || d.Verifications[i].Status == "running" {
			d.Verifications[i].Status = "interrupted"
		}
	}
	for i := range d.Library {
		if d.Library[i].Status == "queued" || d.Library[i].Status == "running" {
			d.Library[i].Status = "interrupted"
		}
	}
}

func (s *Store) Health() StorageHealth {
	if s.pg == nil {
		return StorageHealth{Driver: "sqlite", Available: s.db.Ping() == nil, Generation: 1}
	}
	p := s.pg
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.ensureConnection(ctx)
	return StorageHealth{Driver: "postgres", Available: err == nil, Generation: p.generation, LastFailure: p.lastFailure, RecoveredAt: p.recoveredAt}
}
