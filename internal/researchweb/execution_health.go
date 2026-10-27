package researchweb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type ExecutionStop struct {
	Stage string    `json:"stage"`
	Class string    `json:"class"`
	At    time.Time `json:"at"`
}

type ExecutionHealth struct {
	Operational bool           `json:"operational"`
	CurrentStop *ExecutionStop `json:"current_stop,omitempty"`
	LastStop    *ExecutionStop `json:"last_stop,omitempty"`
}

func safeErrorClass(err error) string {
	var pg *pgconn.PgError
	var rule RuleError
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return "connection_closed"
	case errors.Is(err, sql.ErrTxDone):
		return "transaction_closed"
	case errors.Is(err, ErrLimit):
		return "data_limit"
	case errors.Is(err, ErrConflict):
		return "revision_conflict"
	case errors.As(err, &rule):
		return "rule_violation"
	case errors.As(err, &pg):
		if len(pg.Code) == 5 && strings.IndexFunc(pg.Code, func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) < 0 {
			return "sqlstate_" + pg.Code
		}
	case errors.As(err, &network):
		if network.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	return "internal_error"
}

func (s *Service) stopExecution(stage string, err error) {
	if !knownStopStage(stage) {
		stage = "unclassified"
	}
	stop := &ExecutionStop{Stage: stage, Class: safeErrorClass(err), At: time.Now().UTC()}
	if s.stopReason.CompareAndSwap(nil, stop) {
		s.lastStop.Store(stop)
		body, marshalErr := json.Marshal(stop)
		if marshalErr == nil {
			_ = atomicFile(filepath.Join(s.Options.Config.DataDir, "execution-stop.json"), body)
		}
		log.Printf("research execution stopped: stage=%s class=%s", stage, stop.Class)
	}
	s.cancel()
}

func knownStopStage(stage string) bool {
	switch stage {
	case "unclassified", "team_step", "cycle_read", "cycle_step", "cancellation_read", "attempt_completion", "verification_completion", "library_completion", "telegram_recovery", "telegram_receive", "telegram_delivery", "worker_expiry":
		return true
	}
	return false
}

func (s *Service) loadExecutionStop() {
	body, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "execution-stop.json"), 2048)
	var stop ExecutionStop
	if err != nil || decodeJSON(body, &stop) != nil || !knownStopStage(stop.Stage) || stop.At.IsZero() {
		return
	}
	known := false
	for _, class := range []string{"deadline_exceeded", "cancelled", "connection_closed", "transaction_closed", "data_limit", "revision_conflict", "rule_violation", "network_timeout", "network_error", "internal_error"} {
		known = known || class == stop.Class
	}
	if strings.HasPrefix(stop.Class, "sqlstate_") && len(stop.Class) == 13 && strings.IndexFunc(stop.Class[8:], func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) < 0 {
		known = true
	}
	if known {
		s.lastStop.Store(&stop)
	}
}

func (s *Service) ExecutionHealth() ExecutionHealth {
	return ExecutionHealth{Operational: s.ctx.Err() == nil, CurrentStop: s.stopReason.Load(), LastStop: s.lastStop.Load()}
}
