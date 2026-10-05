package execution

import (
	"context"
	"encoding/json"
)

type RuntimeTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Validate    func(string) error
	Run         func(context.Context, string) (string, error)
}

type runtimeToolsKey struct{}

// Only the trusted caller installs handlers; model output cannot define a tool.
func WithTools(ctx context.Context, handlers ...RuntimeTool) context.Context {
	tools := map[string]RuntimeTool{}
	for _, h := range handlers {
		h.Parameters = append(json.RawMessage(nil), h.Parameters...)
		tools[h.Name] = h
	}
	return context.WithValue(ctx, runtimeToolsKey{}, tools)
}

func runtimeTool(ctx context.Context, name string) (RuntimeTool, bool) {
	tools, _ := ctx.Value(runtimeToolsKey{}).(map[string]RuntimeTool)
	h, ok := tools[name]
	return h, ok && h.Validate != nil && h.Run != nil && json.Valid(h.Parameters)
}
