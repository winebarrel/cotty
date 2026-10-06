package cotty

import "time"

// A connection to a session socket carries one request and one response, each
// a line of JSON.

const (
	opInfo = "info"
	opSend = "send"
	opRead = "read"
)

type request struct {
	Op string `json:"op"`

	// Data is the input for opSend.
	Data string `json:"data,omitempty"`

	// The rest are for opRead. From is honored only when ID names the running
	// session, so a cursor left over from an earlier session of the same name
	// is not applied to this one; without it the last TailLines lines are
	// returned. Max caps the bytes returned.
	ID        string `json:"id,omitempty"`
	From      *int64 `json:"from,omitempty"`
	TailLines int    `json:"tail_lines,omitempty"`
	WaitFor   string `json:"wait_for,omitempty"`
	TimeoutMs int64  `json:"timeout_ms,omitempty"`
	IdleMs    int64  `json:"idle_ms,omitempty"`
	Max       int    `json:"max,omitempty"`
}

type response struct {
	Error string       `json:"error,omitempty"`
	Info  *SessionInfo `json:"info,omitempty"`
	Read  *readReply   `json:"read,omitempty"`
}

// SessionInfo describes a running session.
type SessionInfo struct {
	Name       string    `json:"name"`
	ID         string    `json:"id"`
	Command    []string  `json:"command"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	AgentInput bool      `json:"agent_input"`
	Paused     bool      `json:"paused"`
}

type readReply struct {
	ID        string `json:"id"`
	Output    string `json:"output"`
	Next      int64  `json:"next"`
	Truncated bool   `json:"truncated,omitempty"`
	Matched   bool   `json:"matched,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Closed    bool   `json:"closed,omitempty"`
	Paused    bool   `json:"paused,omitempty"`
}
