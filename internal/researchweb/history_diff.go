package researchweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type HistoryPage struct {
	Events    []ScopedHistory `json:"events"`
	NextAfter int             `json:"next_after"`
	HasMore   bool            `json:"has_more"`
	Revision  int             `json:"revision"`
}

type ScopedHistory struct {
	History
	ScopeBasis string `json:"scope_basis"`
}

func (s *Store) historyRange(after, through, limit int) ([]History, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var rows *sql.Rows
	var err error
	if s.pg != nil {
		p := s.pg
		p.mu.Lock()
		defer p.mu.Unlock()
		if err = p.ensureConnection(ctx); err != nil {
			return nil, err
		}
		rows, err = p.conn.QueryContext(ctx, "SELECT metadata FROM events WHERE revision>$1 AND revision<=$2 ORDER BY revision LIMIT $3", after, through, limit)
	} else {
		s.mu.Lock()
		defer s.mu.Unlock()
		rows, err = s.db.QueryContext(ctx, "SELECT metadata FROM history WHERE revision>? AND revision<=? ORDER BY revision LIMIT ?", after, through, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []History{}
	for rows.Next() {
		var body []byte
		var e History
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func studyObjectIDs(d Data, study string) map[string]bool {
	ids := map[string]bool{study: true}
	for _, e := range d.Entities {
		if e.Study == study {
			ids[e.ID] = true
		}
	}
	for _, t := range d.Tasks {
		if ids[t.Target] {
			ids[t.ID] = true
		}
	}
	for _, a := range d.Attempts {
		if ids[a.Target] {
			ids[a.ID] = true
		}
	}
	for _, b := range d.Branches {
		if b.Study == study {
			ids[b.ID] = true
		}
	}
	for _, t := range d.Teams {
		if t.Study == study {
			ids[t.ID] = true
		}
	}
	for _, q := range d.Questions {
		if ids[q.Target] {
			ids[q.ID] = true
		}
	}
	for _, f := range d.Findings {
		if ids[f.Target] {
			ids[f.ID] = true
		}
	}
	for _, v := range d.Verifications {
		if ids[v.Target] {
			ids[v.ID] = true
		}
	}
	for _, l := range d.Library {
		if ids[l.Lemma] {
			ids[l.ID] = true
		}
	}
	for _, m := range d.LemmaMatches {
		if ids[m.Target] {
			ids[m.ID] = true
		}
	}
	for _, a := range d.Applications {
		if ids[a.Target] {
			ids[a.ID] = true
		}
	}
	for _, p := range d.Proposals {
		if ids[p.Target] {
			ids[p.ID] = true
		}
	}
	for _, c := range d.Cycles {
		if c.Study == study {
			ids[c.ID] = true
		}
	}
	return ids
}

func (s *Service) StudyHistory(study string, after, through int) (HistoryPage, error) {
	v, err := s.Store.Read()
	if err != nil {
		return HistoryPage{}, err
	}
	if after < 0 || through < 0 || v.study(study) == nil {
		return HistoryPage{}, RuleError("Нужны исследование и корректные границы истории.")
	}
	if through == 0 {
		through = v.Revision
	}
	if through > v.Revision || after > through {
		return HistoryPage{}, RuleError("Неверный диапазон истории.")
	}
	rows, err := s.Store.historyRange(after, through, 201)
	if err != nil {
		return HistoryPage{}, err
	}
	out := HistoryPage{Events: []ScopedHistory{}, NextAfter: after, Revision: through, HasMore: len(rows) > 200}
	if out.HasMore {
		rows = rows[:200]
	}
	ids := studyObjectIDs(v.Data, study)
	for _, e := range rows {
		out.NextAfter = e.ID
		if ids[e.Target] {
			out.Events = append(out.Events, ScopedHistory{History: e, ScopeBasis: "recorded_target"})
		} else if e.Target == "" && e.ID > 1 {
			// Legacy creation events did not record the new study id. Attribute
			// only an actual appearance between the two immutable snapshots.
			a, err1 := s.Store.Snapshot(e.ID - 1)
			b, err2 := s.Store.Snapshot(e.ID)
			if err1 == nil && err2 == nil && a.study(study) == nil && b.study(study) != nil {
				out.Events = append(out.Events, ScopedHistory{History: e, ScopeBasis: "snapshot_creation"})
			}
		}
	}
	return out, nil
}

type FieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}
type EntityChange struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Kind           string        `json:"kind"`
	BeforeRevision int           `json:"before_revision"`
	AfterRevision  int           `json:"after_revision"`
	Changes        []FieldChange `json:"changes"`
}
type CheckChange struct {
	ID            string `json:"id"`
	Target        string `json:"target"`
	BeforeCurrent bool   `json:"before_current"`
	AfterCurrent  bool   `json:"after_current"`
	GoalSHA256    string `json:"goal_sha256"`
	SourceSHA256  string `json:"source_sha256"`
}
type StudyDiff struct {
	Study    string         `json:"study"`
	From     int            `json:"from"`
	To       int            `json:"to"`
	Entities []EntityChange `json:"entities"`
	Checks   []CheckChange  `json:"checks"`
}

func entityFields(e *Entity) map[string]any {
	if e == nil {
		return map[string]any{}
	}
	return map[string]any{"title": e.Title, "statement": e.Statement, "assumptions": e.Assumptions, "formal_goal": e.FormalGoal, "dependencies": e.Dependencies, "required_libraries": e.RequiredLibraries, "status": e.Status, "proof": e.Proof, "proof_author": e.ProofAuthor, "proof_attempt": e.ProofAttempt, "proof_verification": e.ProofVerification, "research_result": e.ResearchResult}
}

func (s *Service) DiffStudy(study, target string, from, to int) (StudyDiff, error) {
	out := StudyDiff{Study: study, From: from, To: to, Entities: []EntityChange{}, Checks: []CheckChange{}}
	if from < 1 || to < from || !requestPattern.MatchString(study) || target != "" && !requestPattern.MatchString(target) {
		return out, RuleError("Нужны корректные границы сравнения и исследование.")
	}
	before, err := s.Store.Snapshot(from)
	if err != nil {
		return out, RuleError("Исходный снимок не найден.")
	}
	after, err := s.Store.Snapshot(to)
	if err != nil {
		return out, RuleError("Конечный снимок не найден.")
	}
	if before.study(study) == nil && after.study(study) == nil {
		return out, RuleError("Исследование отсутствует в выбранных снимках.")
	}
	ids := map[string]bool{}
	for _, d := range []*Data{&before, &after} {
		for _, e := range d.Entities {
			if e.Study == study && (target == "" || target == e.ID) {
				ids[e.ID] = true
			}
		}
	}
	if target != "" && !ids[target] {
		return out, RuleError("Утверждение не относится к исследованию.")
	}
	ordered := []string{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	slices.Sort(ordered)
	for _, id := range ordered {
		a, b := before.entity(id), after.entity(id)
		if a != nil && a.Study != study {
			a = nil
		}
		if b != nil && b.Study != study {
			b = nil
		}
		left, right := entityFields(a), entityFields(b)
		e := EntityChange{ID: id, Changes: []FieldChange{}}
		if a != nil {
			e.BeforeRevision = a.Revision
			e.Title = a.Title
			e.Kind = a.Kind
		}
		if b != nil {
			e.AfterRevision = b.Revision
			e.Title = b.Title
			e.Kind = b.Kind
		}
		fields := map[string]bool{}
		for k := range left {
			fields[k] = true
		}
		for k := range right {
			fields[k] = true
		}
		keys := []string{}
		for k := range fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if hash(left[k]) != hash(right[k]) {
				x, _ := json.Marshal(left[k])
				y, _ := json.Marshal(right[k])
				e.Changes = append(e.Changes, FieldChange{Field: k, Before: x, After: y})
			}
		}
		if len(e.Changes) > 0 {
			out.Entities = append(out.Entities, e)
		}
	}
	checks := map[string]Verification{}
	for _, d := range []*Data{&before, &after} {
		for _, v := range d.Verifications {
			if ids[v.Target] {
				checks[v.ID] = v
			}
		}
	}
	checkIDs := []string{}
	for id := range checks {
		checkIDs = append(checkIDs, id)
	}
	slices.Sort(checkIDs)
	for _, id := range checkIDs {
		v := checks[id]
		a, b := before.verification(id), after.verification(id)
		ac := a != nil && verifiedReportMatches(*a) && verificationMatches(&before, *a)
		bc := b != nil && verifiedReportMatches(*b) && verificationMatches(&after, *b)
		if ac != bc || a == nil || b == nil {
			x := CheckChange{ID: id, Target: v.Target, BeforeCurrent: ac, AfterCurrent: bc, GoalSHA256: leancheck.Digest(v.Goal), SourceSHA256: leancheck.Digest(v.Source)}
			out.Checks = append(out.Checks, x)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if len(data) > 4<<20 {
		return StudyDiff{}, RuleError(fmt.Sprintf("Сравнение слишком велико; выберите одно утверждение (%d изменений).", len(out.Entities)))
	}
	return out, nil
}
