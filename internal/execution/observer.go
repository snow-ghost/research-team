package execution

import (
	"context"
	"strings"
	"unicode/utf8"
)

type observerKey struct{}

// Observers receive visible actions and tool results, never reasoning chunks.
func WithObserver(ctx context.Context, observe func(Event)) context.Context {
	return context.WithValue(ctx, observerKey{}, observe)
}

func Publish(ctx context.Context, event Event) {
	if observe, ok := ctx.Value(observerKey{}).(func(Event)); ok {
		observe(event)
	}
}

func record(ctx context.Context, result *Result, event Event) {
	event.Input, event.Output = Preview(event.Input, 8192), Preview(event.Output, 8192)
	if len(result.Events) < 2000 {
		result.Events = append(result.Events, event)
	}
	Publish(ctx, event)
}

func Preview(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "\n[truncated]"
}

func maskedEvent(event Event, secret string) Event {
	if secret != "" {
		event.Input = strings.ReplaceAll(event.Input, secret, "[REDACTED]")
		event.Output = strings.ReplaceAll(event.Output, secret, "[REDACTED]")
		event.Tool = strings.ReplaceAll(event.Tool, secret, "[REDACTED]")
		event.CallID = strings.ReplaceAll(event.CallID, secret, "[REDACTED]")
	}
	return event
}
