package researchweb

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConflict = errors.New("Снимок изменился. Обновите данные перед повторным действием.")
var ErrLimit = errors.New("Достигнуто ограничение объема данных.")

type RuleError string

func (e RuleError) Error() string { return string(e) }

type Study struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Goal     string `json:"goal"`
	Category string `json:"category"`
	Formula  string `json:"formula"`
}
type Entity struct {
	ID                  string         `json:"id"`
	Title               string         `json:"title"`
	Kind                string         `json:"kind"`
	Study               string         `json:"study"`
	Status              string         `json:"status"`
	Statement           string         `json:"statement"`
	Revision            int            `json:"revision"`
	Author              string         `json:"author"`
	Domain              string         `json:"domain"`
	Assumptions         string         `json:"assumptions"`
	Dependencies        []string       `json:"dependencies"`
	DependencyRevisions map[string]int `json:"dependencyRevisions,omitempty"`
	Proof               string         `json:"proof"`
	ProofAuthor         string         `json:"proofAuthor,omitempty"`
	ProofAttempt        string         `json:"proofAttempt,omitempty"`
	Counterexample      string         `json:"counterexample,omitempty"`
	ReviewReason        string         `json:"reviewReason,omitempty"`
	ReviewedBy          string         `json:"reviewedBy,omitempty"`
}
type Task struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Title     string `json:"title"`
	Objective string `json:"objective"`
	Kind      string `json:"kind"`
	Agent     string `json:"agent"`
	Skill     string `json:"skill"`
	State     string `json:"state"`
	Question  string `json:"question,omitempty"`
	Attempt   string `json:"attempt,omitempty"`
}
type Question struct {
	ID            string `json:"id"`
	Target        string `json:"target"`
	Text          string `json:"text"`
	Kind          string `json:"kind"`
	Snapshot      int    `json:"snapshot"`
	Answer        string `json:"answer"`
	AnswerAttempt string `json:"answerAttempt,omitempty"`
}
type Finding struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Text     string `json:"text"`
	Severity string `json:"severity"`
	State    string `json:"state"`
	Revision int    `json:"revision"`
}
type Application struct {
	ID            string `json:"id"`
	Lemma         string `json:"lemma"`
	LemmaRevision int    `json:"lemmaRevision"`
	Target        string `json:"target"`
	State         string `json:"state"`
}
type Attempt struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"task_id"`
	Target         string     `json:"target"`
	TargetRevision int        `json:"target_revision"`
	Profile        string     `json:"profile"`
	Workspace      string     `json:"workspace"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	InputSnapshot  int        `json:"input_snapshot"`
	InputSHA256    string     `json:"input_sha256,omitempty"`
	ResultSHA256   string     `json:"result_sha256,omitempty"`
	RemoteOutcome  string     `json:"remote_outcome"`
}
type Delegation struct {
	ID     string `json:"id"`
	Target string `json:"target"`
}
type Data struct {
	Schema       int           `json:"schema"`
	Revision     int           `json:"revision"`
	Paused       bool          `json:"paused"`
	ScenarioStep int           `json:"scenarioStep"`
	Studies      []Study       `json:"studies"`
	Entities     []Entity      `json:"entities"`
	WorkLinks    [][2]string   `json:"workLinks"`
	Tasks        []Task        `json:"tasks"`
	Questions    []Question    `json:"questions"`
	Findings     []Finding     `json:"findings"`
	Applications []Application `json:"applications"`
	Attempts     []Attempt     `json:"attempts"`
	Delegations  []Delegation  `json:"delegations"`
}
type History struct {
	ID     int    `json:"id"`
	Time   string `json:"time"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Target string `json:"target"`
	Actor  string `json:"actor"`
}
type View struct {
	Data
	History []History `json:"history"`
}
type Action struct {
	Type             string   `json:"type"`
	ExpectedRevision int      `json:"expected_revision"`
	RequestID        string   `json:"request_id"`
	Target           string   `json:"target,omitempty"`
	Study            string   `json:"study,omitempty"`
	Title            string   `json:"title,omitempty"`
	Statement        string   `json:"statement,omitempty"`
	Assumptions      string   `json:"assumptions,omitempty"`
	Category         string   `json:"category,omitempty"`
	Text             string   `json:"text,omitempty"`
	Parts            []string `json:"parts,omitempty"`
	Lemma            string   `json:"lemma,omitempty"`
	Finding          string   `json:"finding,omitempty"`
	Severity         string   `json:"severity,omitempty"`
	Decision         string   `json:"decision,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	Attempt          string   `json:"attempt,omitempty"`
}

func emptyData() Data {
	return Data{Schema: 1, Revision: 1, Studies: []Study{}, Entities: []Entity{}, WorkLinks: [][2]string{},
		Tasks: []Task{}, Questions: []Question{}, Findings: []Finding{}, Applications: []Application{},
		Attempts: []Attempt{}, Delegations: []Delegation{}}
}
func identifier(prefix string) string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(data[:])
}
func hash(value any) string {
	data, _ := json.Marshal(value)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func (d *Data) entity(id string) *Entity {
	for i := range d.Entities {
		if d.Entities[i].ID == id {
			return &d.Entities[i]
		}
	}
	return nil
}
func (d *Data) task(id string) *Task {
	for i := range d.Tasks {
		if d.Tasks[i].ID == id {
			return &d.Tasks[i]
		}
	}
	return nil
}
func (d *Data) attempt(id string) *Attempt {
	for i := range d.Attempts {
		if d.Attempts[i].ID == id {
			return &d.Attempts[i]
		}
	}
	return nil
}
func (d *Data) effective(id string, seen map[string]bool) string {
	e := d.entity(id)
	if e == nil || seen[id] {
		return "blocked"
	}
	if e.Status == "challenged" {
		return "challenged"
	}
	for dep, revision := range e.DependencyRevisions {
		if current := d.entity(dep); current == nil || current.Revision != revision {
			return "blocked"
		}
	}
	for _, app := range d.Applications {
		if app.ID == id {
			if lemma := d.entity(app.Lemma); lemma == nil || lemma.Revision != app.LemmaRevision {
				return "blocked"
			}
		}
	}
	seen[id] = true
	defer delete(seen, id)
	for _, id := range e.Dependencies {
		status := d.effective(id, seen)
		if status == "blocked" || status == "challenged" || status == "refuted" {
			return "blocked"
		}
	}
	return e.Status
}
func textOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsRune(s, 0)
}
func newEntity(id, title, kind, study, statement, assumptions string) Entity {
	return Entity{ID: id, Title: title, Kind: kind, Study: study, Status: "open", Statement: statement,
		Revision: 1, Author: "operator", Assumptions: assumptions, Dependencies: []string{}}
}
func (d *Data) addTask(target, title, kind, objective string) {
	d.Tasks = append(d.Tasks, Task{ID: identifier("T"), Target: target, Title: title, Kind: kind, Objective: objective, State: "queued"})
}
func require(ok bool, message string) error {
	if !ok {
		return RuleError(message)
	}
	return nil
}
func eventFor(d *Data, label, target, detail, actor string) History {
	return History{ID: d.Revision, Time: time.Now().UTC().Format(time.RFC3339), Label: label, Target: target, Detail: detail, Actor: actor}
}
func reference(revision int) string { return fmt.Sprintf("research/v%d", revision) }
