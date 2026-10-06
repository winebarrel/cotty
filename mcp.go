package cotty

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// replyMargin is how much longer than the requested wait a read may take
// before the MCP server gives up on the session.
const replyMargin = 10 * time.Second

const mcpInstructions = `cotty gives you terminals that the user started with "cotty wrap -n <name> <command>", typically an ssh login or a serial console. The user sees the same terminal and may type into it too.

Call list_sessions to find them. To run something, call send and then read. read returns the output that arrived since your previous read of that session, so nothing is missed between calls. Use wait_for to wait for a prompt or other text, or idle_ms to wait until the output settles.

The user can deny your input with a key binding; send then fails, and you should ask the user rather than retry. The user can also pause the output, for example while a secret is on the screen; read then says so, and you should wait for the user rather than act on what you cannot see.`

// MCPCmd serves the sessions to an agent over MCP on stdio.
type MCPCmd struct {
	ReadMax    ByteSize      `default:"32KiB" env:"COTTY_READ_MAX" help:"Most output one read returns. Older output beyond it is dropped."`
	TailLines  int           `default:"50" env:"COTTY_TAIL_LINES" help:"Lines a read returns when it has no previous read to continue from."`
	Timeout    time.Duration `default:"30s" env:"COTTY_TIMEOUT" help:"How long a read waits when the agent does not say."`
	MaxTimeout time.Duration `default:"10m" env:"COTTY_MAX_TIMEOUT" help:"Longest wait the agent may ask a read for."`
}

func (m *MCPCmd) Validate() error {
	switch {
	case m.TailLines < 1:
		return fmt.Errorf("--tail-lines must be at least 1")
	case m.Timeout <= 0:
		return fmt.Errorf("--timeout must be positive")
	case m.MaxTimeout < m.Timeout:
		return fmt.Errorf("--max-timeout must not be shorter than --timeout")
	}

	return nil
}

func (m *MCPCmd) Run(c *Context) error {
	return newMCPServer(c, m).Run(context.Background(), &mcp.StdioTransport{})
}

type cursor struct {
	id   string
	next int64
}

type mcpHandler struct {
	ctx  *Context
	opts *MCPCmd

	// cursors holds, per session name, where the last read left off.
	mu      sync.Mutex
	cursors map[string]cursor
}

type listSessionsInput struct{}

type listSessionsOutput struct {
	Sessions []SessionInfo `json:"sessions"`
}

type sendInput struct {
	Session string `json:"session" jsonschema:"session name"`
	Text    string `json:"text" jsonschema:"text to type"`
	Enter   *bool  `json:"enter,omitempty" jsonschema:"press Enter after the text (default true)"`
}

type sendKeyInput struct {
	Session string `json:"session" jsonschema:"session name"`
	Key     string `json:"key" jsonschema:"key to press: enter, tab, esc, space, backspace, delete, up, down, left, right, home, end, pageup, pagedown, or ctrl-<char> such as ctrl-c"`
}

type readInput struct {
	Session   string `json:"session" jsonschema:"session name"`
	WaitFor   string `json:"wait_for,omitempty" jsonschema:"regular expression (Go RE2) to wait for in the new output"`
	IdleMs    int64  `json:"idle_ms,omitempty" jsonschema:"without wait_for, wait until no output has arrived for this many milliseconds"`
	TimeoutMs int64  `json:"timeout_ms,omitempty" jsonschema:"longest time to wait in milliseconds"`
	TailLines int    `json:"tail_lines,omitempty" jsonschema:"ignore where the previous read left off and return the last this many lines"`
}

func newMCPServer(c *Context, opts *MCPCmd) *mcp.Server {
	h := &mcpHandler{
		ctx:     c,
		opts:    opts,
		cursors: map[string]cursor{},
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "cotty", Version: c.Version},
		&mcp.ServerOptions{Instructions: mcpInstructions},
	)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List the running cotty sessions.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.listSessions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send",
		Description: "Type text into a session, followed by Enter unless enter is false.",
	}, h.send)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_key",
		Description: "Press a special key in a session, such as ctrl-c or up.",
	}, h.sendKey)

	mcp.AddTool(server, &mcp.Tool{
		Name: "read",
		Description: fmt.Sprintf("Read a session's output as plain text. "+
			"Returns what arrived since the previous read of the session, or the last %d lines on the first read, "+
			"up to the last %d bytes. "+
			"Without wait_for or idle_ms it returns at once; otherwise it waits up to timeout_ms (default %d, max %d).",
			opts.TailLines, opts.ReadMax, opts.Timeout.Milliseconds(), opts.MaxTimeout.Milliseconds()),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.read)

	return server
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func (h *mcpHandler) sockPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}

	return h.ctx.sockPath(name), nil
}

// call sends a request to the named session. A session that cannot be
// reached is reported as not running.
func (h *mcpHandler) call(ctx context.Context, name string, req *request) (*response, error) {
	path, err := h.sockPath(name)

	if err != nil {
		return nil, err
	}

	resp, err := roundTrip(ctx, path, req)

	if err != nil {
		if isDialError(err) {
			return nil, fmt.Errorf("session %q is not running", name)
		}

		return nil, err
	}

	return resp, nil
}

func (h *mcpHandler) listSessions(ctx context.Context, _ *mcp.CallToolRequest, _ listSessionsInput) (*mcp.CallToolResult, listSessionsOutput, error) {
	return nil, listSessionsOutput{Sessions: h.ctx.sessions(ctx)}, nil
}

func (h *mcpHandler) send(ctx context.Context, _ *mcp.CallToolRequest, in sendInput) (*mcp.CallToolResult, any, error) {
	data := in.Text

	if in.Enter == nil || *in.Enter {
		data += "\r"
	}

	if _, err := h.call(ctx, in.Session, &request{Op: opSend, Data: data}); err != nil {
		return nil, nil, err
	}

	return textResult("sent"), nil, nil
}

func (h *mcpHandler) sendKey(ctx context.Context, _ *mcp.CallToolRequest, in sendKeyInput) (*mcp.CallToolResult, any, error) {
	key, err := keyBytes(in.Key)

	if err != nil {
		return nil, nil, err
	}

	if _, err := h.call(ctx, in.Session, &request{Op: opSend, Data: string(key)}); err != nil {
		return nil, nil, err
	}

	return textResult("sent"), nil, nil
}

func (h *mcpHandler) read(ctx context.Context, _ *mcp.CallToolRequest, in readInput) (*mcp.CallToolResult, any, error) {
	wait := h.opts.Timeout

	if in.TimeoutMs > 0 {
		wait = min(time.Duration(in.TimeoutMs)*time.Millisecond, h.opts.MaxTimeout)
	}

	req := &request{
		Op:        opRead,
		TailLines: h.opts.TailLines,
		WaitFor:   in.WaitFor,
		TimeoutMs: wait.Milliseconds(),
		IdleMs:    in.IdleMs,
		Max:       int(h.opts.ReadMax),
	}

	h.mu.Lock()
	cur, ok := h.cursors[in.Session]
	h.mu.Unlock()

	if in.TailLines > 0 {
		req.TailLines = in.TailLines
	} else if ok {
		req.ID = cur.id
		req.From = &cur.next
	}

	ctx, cancel := context.WithTimeout(ctx, wait+replyMargin)
	defer cancel()

	resp, err := h.call(ctx, in.Session, req)

	if err != nil {
		return nil, nil, err
	}

	r := resp.Read

	h.mu.Lock()
	h.cursors[in.Session] = cursor{id: r.ID, next: r.Next}
	h.mu.Unlock()

	return textResult(formatRead(r, in.WaitFor)), nil, nil
}

// formatRead renders a read as the output itself, with notes from cotty in
// brackets on lines of their own.
func formatRead(r *readReply, waitFor string) string {
	var b strings.Builder

	if r.Truncated {
		b.WriteString("[cotty: earlier output was dropped]\n")
	}

	b.WriteString(r.Output)

	var notes []string

	switch {
	case r.Output == "":
		notes = append(notes, "no new output")
	case !strings.HasSuffix(r.Output, "\n"):
		b.WriteString("\n")
	}

	if r.TimedOut {
		if waitFor != "" {
			notes = append(notes, fmt.Sprintf("timed out waiting for %q", waitFor))
		} else {
			notes = append(notes, "timed out waiting for output to settle")
		}
	}

	if r.Closed {
		notes = append(notes, "session ended")
	} else if r.Paused {
		notes = append(notes, "the user paused the output; new output is hidden until they resume it")
	}

	for _, note := range notes {
		b.WriteString("[cotty: " + note + "]\n")
	}

	return b.String()
}
