package researchweb

import "testing"

func TestAgentReportBDD_SingleTypedJSONWithProse(t *testing.T) {
	for _, body := range []string{`{"outcome":"none_found","evidence":"Checked"}`, "Explanation\n```json\n{\"outcome\":\"none_found\",\"evidence\":\"Checked\"}\n```\nConclusion"} {
		var r CounterReport
		if err := decodeAgentReport(body, &r); err != nil || r.Outcome != "none_found" {
			t.Fatal(r, err)
		}
	}
	for _, body := range []string{"```json\n{}\n```\n```json\n{}\n```", "```lean\n{}\n```", `{"outcome":"none_found","evidence":"Checked","unknown":true}`, `{"outcome":"none_found"} {}`, "{}\n```json\n{}\n```"} {
		var r CounterReport
		if err := decodeAgentReport(body, &r); err == nil {
			t.Fatal("ambiguous or unknown content parsed", body)
		}
	}
}
