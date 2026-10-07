package execution

import (
	"encoding/json"
	"io"
	"strings"
)

// A typed report may have one labeled block, never competing JSON documents.
func DecodeAgentReport(text string, out any) error {
	text = strings.TrimSpace(text)
	if len(text) > maxPayload {
		return ErrLimit
	}
	if strings.Contains(text, "```") {
		if strings.Count(text, "```") != 2 || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			return ErrProtocol
		}
		block := strings.TrimSpace(strings.Split(text, "```")[1])
		line, body, ok := strings.Cut(block, "\n")
		if !ok || strings.TrimSpace(line) != "json" {
			return ErrProtocol
		}
		text = body
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrProtocol
	}
	return nil
}
