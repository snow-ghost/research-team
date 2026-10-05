package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFailureDiagnostics_ExposeOnlyStableCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.DeadlineExceeded, "deadline_exceeded"},
		{context.Canceled, "cancelled"},
		{ErrLimit, "limit_reached"},
		{errors.Join(ErrLimit, errors.New("cleanup failed")), "limit_reached"},
		{fmt.Errorf("private-token: %w", ErrProtocol), "protocol_or_remote_error"},
		{errors.New("private-token"), "execution_failed"},
		{nil, ""},
	} {
		r := Result{Candidate: "candidate"}
		finish(&r, tc.err)
		if r.FailureCode != tc.code {
			t.Fatalf("got %s, want %s", r.FailureCode, tc.code)
		}
		data, _ := json.Marshal(r)
		if strings.Contains(string(data), "private-token") {
			t.Fatal("raw error exposed")
		}
		if tc.err != nil && r.Candidate != "" {
			t.Fatal("failed candidate retained")
		}
	}
}
