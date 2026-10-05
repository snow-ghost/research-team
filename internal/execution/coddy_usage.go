package execution

import (
	"bufio"
	"encoding/json"
	"io"
)

// Coddy's token_usage extension is not represented by the SDK's update union.
type coddyWireReader struct {
	reader  *bufio.Reader
	pending []byte
	client  *coddyACPClient
	limit   int
}

func newCoddyWireReader(reader io.Reader, client *coddyACPClient, limit int) *coddyWireReader {
	return &coddyWireReader{reader: bufio.NewReader(reader), client: client, limit: limit}
}
func (r *coddyWireReader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		var frame []byte
		for {
			chunk, err := r.reader.ReadSlice('\n')
			if len(frame)+len(chunk) > r.limit {
				return 0, ErrLimit
			}
			frame = append(frame, chunk...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil && !(err == io.EOF && len(frame) > 0) {
				return 0, err
			}
			break
		}
		handled, err := r.client.consumeUsage(frame)
		if err != nil {
			return 0, err
		}
		if !handled {
			r.pending = frame
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func (c *coddyACPClient) consumeUsage(frame []byte) (bool, error) {
	var header struct {
		Method string `json:"method"`
		Params struct {
			Update struct {
				Kind string `json:"sessionUpdate"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &header) != nil || header.Method != "session/update" || header.Params.Update.Kind != "token_usage" {
		return false, nil
	}
	var envelope struct {
		Method string `json:"method"`
		Params struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				Kind   string `json:"sessionUpdate"`
				Input  *int64 `json:"inputTokens"`
				Output *int64 `json:"outputTokens"`
				Total  *int64 `json:"totalTokens"`
			} `json:"update"`
		} `json:"params"`
	}
	decodeErr := json.Unmarshal(frame, &envelope)
	c.mu.Lock()
	defer c.mu.Unlock()
	if decodeErr != nil {
		return true, c.reject(ErrProtocol)
	}
	if !c.running {
		return true, nil
	}
	if envelope.Params.SessionID != string(c.session) {
		return true, c.reject(ErrProtocol)
	}
	u := envelope.Params.Update
	if u.Input == nil || u.Output == nil || u.Total == nil || *u.Input < 0 || *u.Output < 0 || *u.Input > 1e9 || *u.Output > 1e9 || *u.Total < 0 || *u.Total > 2e9 {
		return true, c.reject(ErrProtocol)
	}
	// input/output describe one call; totalTokens is cumulative for this prompt.
	delta := *u.Input + *u.Output
	if delta == 0 {
		if *u.Total != c.usageTotal {
			return true, c.reject(ErrProtocol)
		}
		c.usageUnknown = true
		c.action(Event{Type: "usage_unavailable"})
		return true, nil
	}
	if *u.Total == c.usageTotal {
		return true, nil
	}
	if *u.Total != c.usageTotal+delta || c.usage.InputTokens+*u.Input > 1e9 || c.usage.OutputTokens+*u.Output > 1e9 {
		return true, c.reject(ErrProtocol)
	}
	c.usage.InputTokens += *u.Input
	c.usage.OutputTokens += *u.Output
	c.usageTotal = *u.Total
	c.usageSeen = true
	report := c.reportedUsage()
	c.action(Event{Type: "usage", Usage: report})
	return true, nil
}
func (c *coddyACPClient) reportedUsage() *Usage {
	if !c.usageSeen {
		return nil
	}
	return &Usage{InputTokens: c.usage.InputTokens, OutputTokens: c.usage.OutputTokens, Source: "coddy_token_usage", Incomplete: c.usageUnknown}
}
