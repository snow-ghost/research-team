package researchweb

import "time"

type MethodBasis struct {
	Source       string    `json:"source"`
	Method       string    `json:"method"`
	Procedure    string    `json:"procedure"`
	Rationale    string    `json:"rationale"`
	Plan         string    `json:"plan,omitempty"`
	Attempt      string    `json:"attempt,omitempty"`
	ResultSHA256 string    `json:"result_sha256,omitempty"`
	ConfirmedAt  time.Time `json:"confirmed_at"`
}

type OperatorNote struct {
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
}

func recordOperatorNote(team *ResearchTeam, r TeamCommand) {
	if r.Note == "" {
		return
	}
	kind := r.NoteKind
	if kind == "" {
		kind = "unspecified"
	}
	team.Notes = append(team.Notes, OperatorNote{Kind: kind, Text: r.Note, Command: r.RequestID, CreatedAt: time.Now().UTC()})
}
