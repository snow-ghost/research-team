package researchweb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

const maxResultPackage = 16 << 20

type PackageAttempt struct {
	SourceModel         string           `json:"source_model,omitempty"`
	SourceExecutorKind  string           `json:"source_executor_kind,omitempty"`
	Attempt             Attempt          `json:"attempt"`
	SourceProfileSHA256 string           `json:"source_profile_sha256"`
	Input               []byte           `json:"scoped_input"`
	InputSHA256         string           `json:"scoped_input_sha256"`
	Result              execution.Result `json:"result"`
	Journal             []byte           `json:"journal"`
	JournalSHA256       string           `json:"journal_sha256"`
}

type TransferredModule struct {
	ID             string `json:"id"`
	Module         string `json:"module"`
	SourceSHA256   string `json:"source_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

type ResultPackage struct {
	IncompleteAttempts []IncompleteAttempt `json:"incomplete_attempts,omitempty"`
	MethodBases        []MethodBasis       `json:"method_bases,omitempty"`
	TypedOperatorNotes []OperatorNote      `json:"typed_operator_notes,omitempty"`
	Version            int                 `json:"version"`
	SHA256             string              `json:"sha256"`
	ExportedAt         time.Time           `json:"exported_at"`
	SourceRevision     int                 `json:"source_revision"`
	SourceStateSHA256  string              `json:"source_state_sha256"`
	Study              Study               `json:"study"`
	Target             Entity              `json:"target"`
	Verification       Verification        `json:"verification"`
	Result             ResearchResult      `json:"result"`
	Attempts           []PackageAttempt    `json:"attempts"`
	Findings           []Finding           `json:"findings"`
	History            []History           `json:"history"`
	Decisions          []TeamDecision      `json:"decisions,omitempty"`
	Memory             []MemoryEntry       `json:"memory,omitempty"`
	OperatorNotes      []string            `json:"operator_notes,omitempty"`
	Module             *TransferredModule  `json:"module,omitempty"`
}

type ResultTransfer struct {
	IncompleteAttempts  []IncompleteAttempt `json:"incomplete_attempts,omitempty"`
	MethodBases         []MethodBasis       `json:"method_bases,omitempty"`
	TypedOperatorNotes  []OperatorNote      `json:"typed_operator_notes,omitempty"`
	PackageSHA256       string              `json:"package_sha256"`
	ExportedAt          time.Time           `json:"exported_at"`
	ImportedAt          time.Time           `json:"imported_at"`
	SourceRevision      int                 `json:"source_revision"`
	SourceStateSHA256   string              `json:"source_state_sha256"`
	SourceStatus        string              `json:"source_status"`
	OriginalReport      leancheck.Report    `json:"original_report"`
	OriginalInputSHA256 map[string]string   `json:"original_input_sha256"`
	ScopedInputSHA256   map[string]string   `json:"scoped_input_sha256"`
	JournalSHA256       map[string]string   `json:"journal_sha256"`
	SourceProfileSHA256 map[string]string   `json:"source_profile_sha256"`
	SourceModels        map[string]string   `json:"source_models,omitempty"`
	SourceExecutorKinds map[string]string   `json:"source_executor_kinds,omitempty"`
	Module              *TransferredModule  `json:"module,omitempty"`
	History             []History           `json:"history"`
	Decisions           []TeamDecision      `json:"decisions,omitempty"`
	Memory              []MemoryEntry       `json:"memory,omitempty"`
	OperatorNotes       []string            `json:"operator_notes,omitempty"`
}

type ImportResultPackageRequest struct {
	ExpectedRevision int           `json:"expected_revision"`
	RequestID        string        `json:"request_id"`
	Package          ResultPackage `json:"package"`
	Confirm          bool          `json:"confirm"`
}

func bytesDigest(body []byte) string       { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func packageDigest(p ResultPackage) string { p.SHA256 = ""; return hash(p) }

// Inputs are projected to this result. Original hashes remain in the transfer receipt.
func scopedPackageInput(d Data, target string, ids map[string]bool) Data {
	out := emptyData()
	out.Revision = d.Revision
	out.Paused = d.Paused
	if e := d.entity(target); e != nil {
		out.Entities = append(out.Entities, *e)
	}
	for _, a := range d.Attempts {
		if ids[a.ID] {
			a.ProfileConfiguration = nil
			out.Attempts = append(out.Attempts, a)
		}
	}
	for _, t := range d.Tasks {
		if ids[t.Attempt] {
			out.Tasks = append(out.Tasks, t)
		}
	}
	for _, m := range d.Memory {
		if m.Target == target {
			out.Memory = append(out.Memory, m)
		}
	}
	return out
}

func (s *Service) ExportResultPackage(id string) (ResultPackage, error) {
	v, err := s.Store.Read()
	if err != nil {
		return ResultPackage{}, err
	}
	return s.exportResultPackage(v, id)
}

func ExportResultPackageSnapshot(v View, dir, id string) (ResultPackage, error) {
	s := &Service{Options: Options{Config: Config{DataDir: dir}}}
	return s.exportResultPackage(v, id)
}

func (s *Service) exportResultPackage(v View, id string) (ResultPackage, error) {
	var err error
	var result *ResearchResult
	for i := range v.Results {
		if v.Results[i].ID == id {
			result = &v.Results[i]
		}
	}
	if result == nil || !slices.Contains([]string{"ready", "accepted"}, result.Status) || result.ReviewBindingMethod != "typed_binding" || !resultMatches(&v.Data, *result) || result.Transfer != nil {
		return ResultPackage{}, RuleError("Нужен действующий результат с точными исходными связями проверок.")
	}
	e := v.entity(result.Target)
	if counterHasObligations(result.Counter) {
		return ResultPackage{}, RuleError("Перенос результата с отдельным разбором вопросов контрпримеров пока требует полного архива области.")
	}
	if study := v.study(e.Study); study != nil && study.Definitions != nil {
		return ResultPackage{}, RuleError("Перенос отдельных результатов с общими определениями пока не поддерживается; используйте полный проверенный архив области.")
	}
	check := proofVerification(&v.Data, e)
	if check == nil || check.Report == nil || check.Report.AuditSHA256 != leancheck.AuditDigest() {
		return ResultPackage{}, RuleError("Нужна текущая проверка Lean.")
	}
	p := ResultPackage{Version: 1, ExportedAt: time.Now().UTC(), SourceRevision: v.Revision, SourceStateSHA256: hash(v.Data), Study: *v.study(e.Study), Target: *e, Verification: *check, Result: *result, Attempts: []PackageAttempt{}, Findings: []Finding{}, History: []History{}}
	p.Study.Budget = nil
	p.Verification.Environment = nil
	ids := map[string]bool{}
	incompleteIDs := map[string]bool{}
	required := map[string]bool{}
	for _, id := range []string{check.Attempt, result.ReviewAttempt, result.CounterAttempt} {
		if id != "" {
			required[id] = true
		}
	}
	var visit func(string) error
	visit = func(id string) error {
		if id == "" || ids[id] || incompleteIDs[id] {
			return nil
		}
		a := v.attempt(id)
		if a == nil || a.Target != e.ID || active(a.Status) || a.ImportedPackageSHA256 != "" {
			return RuleError("Все попытки пакета должны быть завершены в исходной области.")
		}
		if a.InputSHA256 == "" && a.RemoteOutcome == "not_started" {
			if len(incompleteIDs) >= 64 {
				return ErrLimit
			}
			x, err := s.incompleteAttempt(v, *a, required)
			if err != nil {
				return err
			}
			p.Version = 2
			p.IncompleteAttempts = append(p.IncompleteAttempts, x)
			incompleteIDs[id] = true
		} else {
			if len(ids) >= 8 {
				return ErrLimit
			}
			ids[id] = true
		}
		return visit(a.ParentAttempt)
	}
	for _, id := range []string{check.Attempt, result.ReviewAttempt, result.CounterAttempt} {
		if err = visit(id); err != nil {
			return p, err
		}
	}
	for _, a := range v.Attempts {
		if a.Target == e.ID && !active(a.Status) {
			if err = visit(a.ID); err != nil {
				return p, err
			}
		}
	}
	for _, a := range v.Attempts {
		if !ids[a.ID] {
			continue
		}
		if a.ProfileConfiguration == nil {
			return p, RuleError("Для экспорта нужен закрепленный исходный профиль попытки.")
		}
		input, readErr := s.readInput(a)
		if readErr != nil {
			return p, readErr
		}
		r, readErr := s.readResult(a)
		if readErr != nil || !s.safeResultContent(a, r) {
			return p, RuleError("Повреждено содержимое исходной попытки.")
		}
		body, _ := json.Marshal(struct {
			Research Data `json:"research"`
		}{scopedPackageInput(input, e.ID, ids)})
		journal, readErr := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "events.jsonl"), 4<<20)
		if readErr != nil {
			return p, readErr
		}
		profile := attemptProfile(s, a)
		profileHash := hash(profile)
		model := ""
		if profile.Model != nil {
			model = profile.Model.Model
		}
		if profile.External != nil {
			model = profile.External.Provider + ":" + profile.External.Model
		}
		a.ProfileConfiguration = nil
		p.Attempts = append(p.Attempts, PackageAttempt{Attempt: a, SourceProfileSHA256: profileHash, Input: body, InputSHA256: bytesDigest(body), Result: r, Journal: journal, JournalSHA256: bytesDigest(journal)})
		p.Attempts[len(p.Attempts)-1].SourceModel = model
		p.Attempts[len(p.Attempts)-1].SourceExecutorKind = profile.Kind
	}
	for _, f := range v.Findings {
		if f.Target == e.ID {
			p.Findings = append(p.Findings, f)
		}
	}
	allIDs := map[string]bool{}
	for id := range ids {
		allIDs[id] = true
	}
	for _, x := range p.IncompleteAttempts {
		allIDs[x.Attempt.ID] = true
	}
	for _, h := range v.History {
		if h.Target == e.ID || allIDs[h.Target] {
			p.History = append(p.History, h)
		}
	}
	for _, team := range v.Teams {
		included := false
		for _, decision := range team.Decisions {
			if ids[decision.Attempt] {
				included = true
				p.Decisions = append(p.Decisions, decision)
			}
		}
		if included && team.OperatorNote != "" {
			p.OperatorNotes = append(p.OperatorNotes, team.OperatorNote)
		}
		if included {
			if team.MethodBasis != nil {
				p.MethodBases = append(p.MethodBases, *team.MethodBasis)
			}
			p.TypedOperatorNotes = append(p.TypedOperatorNotes, team.Notes...)
		}
	}
	for _, m := range v.Memory {
		if m.Target == e.ID {
			p.Memory = append(p.Memory, m)
		}
	}
	for _, l := range v.Library {
		if l.Lemma == e.ID && l.Status == "ready" && l.Report != nil && libraryMatches(&v.Data, l) {
			p.Module = &TransferredModule{ID: l.ID, Module: l.Module, SourceSHA256: leancheck.Digest(l.Source), ArtifactSHA256: l.Report.ArtifactSHA256}
		}
	}
	if len(p.MethodBases) > 0 || len(p.TypedOperatorNotes) > 0 {
		p.Version = 2
	}
	p.SHA256 = packageDigest(p)
	return p, validateResultPackage(p)
}

func validateResultPackage(p ResultPackage) error {
	if counterHasObligations(p.Result.Counter) || validateCounterReport(p.Result.Counter) != nil {
		return RuleError("Отчет контрпримеров содержит непереносимый разбор вопросов или некорректные поля.")
	}
	if p.Study.Definitions != nil {
		return RuleError("Пакет отдельных результатов не содержит полного набора общих определений.")
	}
	body, err := json.Marshal(p)
	if err != nil || len(body) > maxResultPackage {
		return ErrLimit
	}
	if (p.Version != 1 && p.Version != 2) || p.SHA256 != packageDigest(p) || !requestPattern.MatchString(p.Target.ID) || !requestPattern.MatchString(p.Study.ID) || !requestPattern.MatchString(p.Result.ID) || !requestPattern.MatchString(p.Verification.ID) || p.Target.Study != p.Study.ID || p.SourceRevision < 1 || p.Target.FormalGoal == nil || p.Target.ResearchResult != p.Result.ID || p.Result.Target != p.Target.ID || p.Result.TargetRevision != p.Target.Revision || p.Result.ReviewBindingMethod != "typed_binding" || p.Result.Transfer != nil || !slices.Contains([]string{"ready", "accepted"}, p.Result.Status) || !verifiedReportMatches(p.Verification) || p.Verification.Report.AuditSHA256 != leancheck.AuditDigest() || p.Verification.Target != p.Target.ID || p.Verification.TargetRevision != p.Target.Revision || p.Result.Author != p.Target.ProofAuthor || leancheck.Digest(*p.Target.FormalGoal) != leancheck.Digest(p.Verification.Goal) || p.Result.Binding != (ProofBinding{p.Verification.ID, p.Verification.Report.GoalSHA256, p.Verification.Report.SourceSHA256}) || p.Result.EnvironmentSHA256 != p.Verification.Report.EnvironmentSHA256 || len(p.Attempts) < 2 || len(p.Attempts) > 8 || len(p.Findings) > 100 || len(p.History) > 100 || len(p.Decisions) > 100 || len(p.Target.Dependencies) > 16 {
		return RuleError("Нарушена структура или контрольная сумма пакета результата.")
	}
	if proofreview.Validate(p.Result.Review) != nil || p.Result.Counter.Outcome != "none_found" {
		return RuleError("Нужны завершенные проверки точного утверждения.")
	}
	if p.Module != nil && (!requestPattern.MatchString(p.Module.ID) || !strings.HasPrefix(p.Module.ID, "library-") || p.Module.Module != "ResearchLemma_"+strings.TrimPrefix(p.Module.ID, "library-") || !slices.Contains([]string{"accepted", "in_review"}, p.Target.Status)) {
		return RuleError("Некорректный модуль пакета.")
	}
	d := emptyData()
	d.Entities = []Entity{p.Target}
	d.Verifications = []Verification{p.Verification}
	seen := map[string]bool{}
	for _, packet := range p.Attempts {
		a := packet.Attempt
		if !requestPattern.MatchString(a.ID) || !requestPattern.MatchString(a.TaskID) || seen[a.ID] || a.Target != p.Target.ID || active(a.Status) || a.ResultSHA256 != hash(packet.Result) || packet.Result.AttemptID != a.ID || packet.Result.TaskID != a.TaskID || packet.InputSHA256 != bytesDigest(packet.Input) || packet.JournalSHA256 != bytesDigest(packet.Journal) || a.ProfileConfiguration != nil || a.ImportedPackageSHA256 != "" {
			return RuleError("Повреждены материалы попытки пакета.")
		}
		var input struct {
			Research Data `json:"research"`
		}
		if decodeJSON(packet.Input, &input) != nil || len(input.Research.Entities) > 1 || input.Research.entity(p.Target.ID) == nil || input.Research.entity(p.Target.ID).FormalGoal == nil || leancheck.Digest(*input.Research.entity(p.Target.ID).FormalGoal) != p.Result.Binding.GoalSHA256 || len(input.Research.Attempts) > 8 || input.Research.attempt(a.ID) == nil || input.Research.task(a.TaskID) == nil || input.Research.attempt(a.ID).ProofBinding != nil && (a.ProofBinding == nil || *input.Research.attempt(a.ID).ProofBinding != *a.ProofBinding) {
			return RuleError("Нарушена историческая привязка входного снимка.")
		}
		original := input.Research.attempt(a.ID)
		if (original.ProofBinding == nil) != (a.ProofBinding == nil) || original.Profile != a.Profile || original.TaskID != a.TaskID || original.Target != a.Target || original.TargetRevision != a.TargetRevision || input.Research.entity(p.Target.ID).Revision != a.TargetRevision {
			return RuleError("Подменена исходная привязка задания или рецензии.")
		}
		for _, m := range input.Research.Memory {
			if m.Target != p.Target.ID {
				return RuleError("Снимок содержит память другого утверждения.")
			}
		}
		if a.ID == p.Result.ReviewAttempt {
			if a.ProofBinding == nil || *a.ProofBinding != p.Result.Binding || a.Status != "candidate" || "executor:"+a.Profile == p.Target.ProofAuthor || a.ResultSHA256 != p.Result.ReviewResultSHA256 || input.Research.task(a.TaskID).Kind != "review" {
				return RuleError("Рецензия не привязана к исходнику отдельного автора.")
			}
			var report proofreview.Report
			if decodeAgentReport(packet.Result.Candidate, &report) != nil || !validAttemptReview(a, report) || hash(report) != hash(p.Result.Review) {
				return RuleError("Отчет рецензии заменен.")
			}
		}
		if a.ID == p.Result.CounterAttempt {
			var report CounterReport
			if a.Status != "candidate" || a.ResultSHA256 != p.Result.CounterResultSHA256 || input.Research.task(a.TaskID).Kind != "counterexample" || decodeAgentReport(packet.Result.Candidate, &report) != nil || hash(report) != hash(p.Result.Counter) {
				return RuleError("Отчет контрпримеров заменен.")
			}
		}
		seen[a.ID] = true
		d.Attempts = append(d.Attempts, a)
	}
	if !seen[p.Result.ReviewAttempt] || !seen[p.Result.CounterAttempt] || (p.Verification.Attempt != "" && !seen[p.Verification.Attempt]) {
		return RuleError("Пакет не содержит обязательные попытки.")
	}
	if err := validateIncomplete(p, seen); err != nil {
		return err
	}
	for _, m := range p.Memory {
		if m.Target != p.Target.ID {
			return RuleError("Пакет содержит память другого утверждения.")
		}
	}
	if p.Verification.Attempt != "" {
		a := d.attempt(p.Verification.Attempt)
		review := d.attempt(p.Result.ReviewAttempt)
		if a.Status != "candidate" || a.Profile == review.Profile || review.ReviewOf != a.ID || p.Target.ProofAuthor != "executor:"+a.Profile {
			return RuleError("Изменено авторство доказательства.")
		}
		for _, packet := range p.Attempts {
			if packet.Attempt.ID == a.ID {
				source, err := leancheck.ExtractSource(packet.Result.Candidate)
				if err != nil || source != p.Verification.Source || p.Target.ProofAttempt != a.ID || p.Target.Proof != packet.Result.Candidate {
					return RuleError("Исходник не совпадает с ответом автора.")
				}
			}
		}
	} else if p.Target.Proof != p.Verification.Source || p.Target.ProofAuthor != p.Verification.Author {
		return RuleError("Изменено авторство присланного исходника.")
	}
	for _, f := range p.Findings {
		if !requestPattern.MatchString(f.ID) || f.Target != p.Target.ID || f.Severity != "editorial" && f.State != "resolved" {
			return RuleError("Пакет содержит незакрытое математическое замечание.")
		}
	}
	for _, id := range p.Result.FindingIDs {
		found := false
		for _, f := range p.Findings {
			found = found || f.ID == id && f.ReviewAttempt == p.Result.ReviewAttempt
		}
		if !found {
			return RuleError("Утеряно замечание рецензии.")
		}
	}
	return nil
}

func (s *Service) ImportResultPackage(r ImportResultPackageRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || s.Options.Checker == nil {
		return RuleError("Подтвердите перенос; нужна новая местная проверка Lean.")
	}
	if err := validateResultPackage(r.Package); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = os.RemoveAll(path)
			}
		}
	}()
	err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Перенесен проверяемый пакет результата", r.Package.Target.ID, "operator", func(d *Data) error {
		p := r.Package
		if !d.Paused || d.Maintenance != nil {
			return RuleError("Перенос доступен в приостановленной области вне обслуживания.")
		}
		if d.entity(p.Target.ID) != nil || d.verification(p.Verification.ID) != nil {
			return RuleError("Идентификаторы пакета уже существуют; объединение записей не выполняется.")
		}
		for _, old := range d.Results {
			if old.ID == p.Result.ID {
				return RuleError("Результат уже существует.")
			}
		}
		for _, dep := range p.Target.Dependencies {
			e := d.entity(dep)
			if e == nil || e.Revision != p.Result.DependencyRevisions[dep] || d.effective(dep, map[string]bool{}) != "accepted" {
				return RuleError("Сначала перенесите и примите точные версии оснований.")
			}
		}
		for id, pin := range p.Verification.LibraryPins {
			found := false
			for _, l := range d.Library {
				found = found || l.ID == id && l.Report != nil && l.Report.ArtifactSHA256 == pin && l.Status == "ready" && libraryMatches(d, l)
			}
			if !found {
				return RuleError("Отсутствует точный модуль исходного основания.")
			}
		}
		if p.Module != nil {
			for _, l := range d.Library {
				if l.ID == p.Module.ID || l.Module == p.Module.Module {
					return RuleError("Модуль пакета конфликтует с библиотекой.")
				}
			}
		}
		for _, packet := range p.Attempts {
			if d.attempt(packet.Attempt.ID) != nil || d.task(packet.Attempt.TaskID) != nil {
				return RuleError("Идентификатор попытки уже существует.")
			}
		}
		for _, packet := range p.IncompleteAttempts {
			if d.attempt(packet.Attempt.ID) != nil || d.task(packet.Task.ID) != nil {
				return RuleError("Идентификатор неполной истории уже существует.")
			}
		}
		for _, f := range p.Findings {
			for _, old := range d.Findings {
				if old.ID == f.ID {
					return RuleError("Идентификатор замечания уже существует.")
				}
			}
		}
		if d.study(p.Study.ID) == nil {
			study := p.Study
			study.Budget = nil
			study.Goal = p.Target.ID
			study.Title = "Перенос: " + study.Title
			d.Studies = append(d.Studies, study)
		} else if p.Study.Goal == p.Target.ID {
			d.study(p.Study.ID).Goal = p.Target.ID
		}
		transfer := ResultTransfer{PackageSHA256: p.SHA256, ExportedAt: p.ExportedAt, ImportedAt: time.Now().UTC(), SourceRevision: p.SourceRevision, SourceStateSHA256: p.SourceStateSHA256, SourceStatus: p.Target.Status, OriginalReport: *p.Verification.Report, OriginalInputSHA256: map[string]string{}, ScopedInputSHA256: map[string]string{}, JournalSHA256: map[string]string{}, SourceProfileSHA256: map[string]string{}, Module: p.Module, History: p.History, Decisions: p.Decisions}
		transfer.IncompleteAttempts, transfer.MethodBases, transfer.TypedOperatorNotes = p.IncompleteAttempts, p.MethodBases, p.TypedOperatorNotes
		transfer.Memory = p.Memory
		transfer.OperatorNotes = p.OperatorNotes
		transfer.SourceModels = map[string]string{}
		transfer.SourceExecutorKinds = map[string]string{}
		for _, packet := range p.Attempts {
			a := packet.Attempt
			dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID)
			if err := os.Mkdir(dir, 0700); err != nil {
				return errors.New("destination attempt directory must be new")
			}
			created = append(created, dir)
			result, _ := json.Marshal(packet.Result)
			for name, body := range map[string][]byte{"input.json": packet.Input, "result.json": result, "events.jsonl": packet.Journal} {
				if err := atomicFile(filepath.Join(dir, name), body); err != nil {
					return err
				}
			}
			transfer.OriginalInputSHA256[a.ID] = a.InputSHA256
			transfer.ScopedInputSHA256[a.ID] = packet.InputSHA256
			transfer.JournalSHA256[a.ID] = packet.JournalSHA256
			transfer.SourceProfileSHA256[a.ID] = packet.SourceProfileSHA256
			transfer.SourceModels[a.ID] = packet.SourceModel
			transfer.SourceExecutorKinds[a.ID] = packet.SourceExecutorKind
			a.InputSHA256 = packet.InputSHA256
			a.ImportedPackageSHA256 = p.SHA256
			a.TeamID = ""
			a.CycleID = ""
			a.RemoteWorker = ""
			a.LeaseExpires = nil
			a.LeaseDeadline = nil
			a.ProfileConfiguration = nil
			d.Attempts = append(d.Attempts, a)
			var input struct {
				Research Data `json:"research"`
			}
			_ = json.Unmarshal(packet.Input, &input)
			task := *input.Research.task(a.TaskID)
			task.CycleID = ""
			task.State = a.Status
			d.Tasks = append(d.Tasks, task)
		}
		for _, packet := range p.IncompleteAttempts {
			a := packet.Attempt
			dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID)
			if err := os.Mkdir(dir, 0700); err != nil {
				return errors.New("incomplete history destination must be new")
			}
			created = append(created, dir)
			for name, body := range incompleteFiles(packet) {
				if err := atomicFile(filepath.Join(dir, name), body); err != nil {
					return err
				}
			}
			a.ImportedPackageSHA256, a.TeamID, a.CycleID, a.RemoteWorker = p.SHA256, "", "", ""
			a.LeaseExpires, a.LeaseDeadline = nil, nil
			a.MissingMaterials = slices.Clone(packet.Missing)
			d.Attempts = append(d.Attempts, a)
			task := packet.Task
			task.CycleID, task.State = "", a.Status
			d.Tasks = append(d.Tasks, task)
			transfer.SourceProfileSHA256[a.ID] = packet.SourceProfileSHA256
		}
		e := p.Target
		e.Status = "in_review"
		e.ReviewReason = ""
		e.ReviewedBy = ""
		d.Entities = append(d.Entities, e)
		verification := p.Verification
		verification.Status = "queued"
		verification.Report = nil
		verification.FinishedAt = nil
		verification.Environment, verification.LibraryPins = s.libraryEnvironment(d, d.entity(e.ID))
		d.Verifications = append(d.Verifications, verification)
		result := p.Result
		result.Transfer = &transfer
		result.Status = "stale"
		if verification.Environment != nil {
			result.EnvironmentSHA256 = leancheck.Digest(*verification.Environment)
		}
		d.Results = append(d.Results, result)
		d.Findings = append(d.Findings, p.Findings...)
		for _, dep := range e.Dependencies {
			d.WorkLinks = append(d.WorkLinks, [2]string{e.ID, dep})
		}
		return nil
	})
	committed = err == nil
	return err
}
