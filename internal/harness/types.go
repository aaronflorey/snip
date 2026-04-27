package harness

import "encoding/json"

// Provider streams normalized harness events.
type Provider interface {
	ID() string
	Stream(query Query, yield func(Event) error) error
}

// Query defines the scan scope for all providers.
type Query struct {
	All         bool
	Since       int64
	CWD         string
	ProjectRoot string
}

// Event is the strict normalized session record shared across harnesses.
type Event struct {
	Provider    string
	SessionID   string
	ProjectRoot string
	Timestamp   int64
	Source      string
	Kind        string
	ToolCall    *ToolCall
	Metadata    json.RawMessage
}

// ToolCall stores normalized tool invocation data.
type ToolCall struct {
	Name      string
	Command   string
	Arguments json.RawMessage
}
