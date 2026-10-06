package cotty

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultMCPCmd() *MCPCmd {
	return &MCPCmd{ReadMax: 32 << 10, TailLines: 50, Timeout: 30 * time.Second, MaxTimeout: 10 * time.Minute}
}

func connectMCP(t *testing.T, c *Context, opts *MCPCmd) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	_, err := newMCPServer(c, opts).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })

	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.Len(t, res.Content, 1)

	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestMCP(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	home := shortTempDir(t)
	ts := startSession(t, home, "web1", "cat")
	cs := connectMCP(t, &Context{Home: home, Version: "test"}, defaultMCPCmd())

	text, isErr := callTool(t, cs, "list_sessions", nil)
	require.False(isErr, text)

	var list listSessionsOutput
	require.NoError(json.Unmarshal([]byte(text), &list))
	require.Len(list.Sessions, 1)
	assert.Equal("web1", list.Sessions[0].Name)
	assert.True(list.Sessions[0].AgentInput)

	// The first read has a cursor to start from afterwards.
	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1"})
	assert.Equal("[cotty: no new output]\n", text)

	text, isErr = callTool(t, cs, "send", map[string]any{"session": "web1", "text": "hello"})
	require.False(isErr, text)

	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1", "wait_for": `hello\n.*hello\n`, "timeout_ms": 5000})
	assert.Equal("hello\nhello\n", text)

	text, _ = callTool(t, cs, "send", map[string]any{"session": "web1", "text": "partial", "enter": false})
	assert.Equal("sent", text)

	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1", "idle_ms": 200})
	assert.Equal("partial\n", text)

	text, _ = callTool(t, cs, "send_key", map[string]any{"session": "web1", "key": "enter"})
	assert.Equal("sent", text)

	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1", "wait_for": `partial\n`, "timeout_ms": 5000})
	assert.Equal("\npartial\n", text)

	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1", "wait_for": "never", "timeout_ms": 100})
	assert.Equal("[cotty: no new output]\n[cotty: timed out waiting for \"never\"]\n", text)

	text, _ = callTool(t, cs, "read", map[string]any{"session": "web1", "tail_lines": 1})
	assert.Equal("partial\n", text)

	text, isErr = callTool(t, cs, "send_key", map[string]any{"session": "web1", "key": "f13"})
	assert.True(isErr)
	assert.Contains(text, `unknown key: "f13"`)

	text, isErr = callTool(t, cs, "send_key", map[string]any{"session": "web1", "key": "ctrl-d"})
	require.False(isErr, text)
	require.NoError(ts.wait(t))

	text, isErr = callTool(t, cs, "send", map[string]any{"session": "web1", "text": "x"})
	assert.True(isErr)
	assert.Contains(text, `session "web1" is not running`)

	text, _ = callTool(t, cs, "list_sessions", nil)
	assert.JSONEq(`{"sessions":[]}`, text)
}

func TestMCPOptions(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	home := shortTempDir(t)
	ts := startSession(t, home, "opts", "sh", "-c", "for i in 1 2 3 4 5; do echo line$i; done; read x")
	cs := connectMCP(t, &Context{Home: home}, &MCPCmd{ReadMax: 12, TailLines: 2, Timeout: 50 * time.Millisecond, MaxTimeout: 100 * time.Millisecond})

	// The first read is the last two lines.
	text, _ := callTool(t, cs, "read", map[string]any{"session": "opts", "wait_for": "line5", "timeout_ms": 5000})
	assert.Equal("line4\nline5\n", text)

	// A read returns no more than the last 12 bytes.
	text, _ = callTool(t, cs, "read", map[string]any{"session": "opts", "tail_lines": 5})
	assert.Equal("[cotty: earlier output was dropped]\nline4\nline5\n", text)

	// The agent cannot wait longer than the maximum.
	began := time.Now()
	text, _ = callTool(t, cs, "read", map[string]any{"session": "opts", "wait_for": "never", "timeout_ms": 60000})
	assert.Less(time.Since(began), 5*time.Second)
	assert.Contains(text, "timed out")

	// Without timeout_ms the default applies.
	began = time.Now()
	callTool(t, cs, "read", map[string]any{"session": "opts", "wait_for": "never"})
	assert.Less(time.Since(began), 5*time.Second)

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(err)

	for _, tool := range res.Tools {
		if tool.Name == "read" {
			assert.True(strings.Contains(tool.Description, "last 2 lines"), tool.Description)
			assert.True(strings.Contains(tool.Description, "default 50, max 100"), tool.Description)
		}
	}

	_, err = ts.call(&request{Op: opSend, Data: "\r"})
	require.NoError(err)
	require.NoError(ts.wait(t))
}

func TestMCPCmdValidate(t *testing.T) {
	assert.NoError(t, defaultMCPCmd().Validate())

	for _, mod := range []func(*MCPCmd){
		func(m *MCPCmd) { m.TailLines = 0 },
		func(m *MCPCmd) { m.Timeout = 0 },
		func(m *MCPCmd) { m.MaxTimeout = time.Second },
	} {
		m := defaultMCPCmd()
		mod(m)
		assert.Error(t, m.Validate())
	}
}

func TestMCPDenied(t *testing.T) {
	require := require.New(t)
	home := shortTempDir(t)
	ts := startSession(t, home, "deny", "cat")
	cs := connectMCP(t, &Context{Home: home}, defaultMCPCmd())

	ts.stdin.Write([]byte{prefixKey, prefixToggleAgent})

	require.Eventually(func() bool {
		text, isErr := callTool(t, cs, "send_key", map[string]any{"session": "deny", "key": "enter"})
		return isErr && strings.Contains(text, "agent input is denied")
	}, 5*time.Second, 10*time.Millisecond)

	text, isErr := callTool(t, cs, "send", map[string]any{"session": "deny", "text": "x"})
	assert.True(t, isErr)
	assert.Contains(t, text, "agent input is denied")

	ts.stdin.Write([]byte{prefixKey, prefixToggleAgent})

	require.Eventually(func() bool {
		_, isErr := callTool(t, cs, "send_key", map[string]any{"session": "deny", "key": "ctrl-d"})
		return !isErr
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(ts.wait(t))
}

func TestMCPNotRunning(t *testing.T) {
	cs := connectMCP(t, &Context{Home: shortTempDir(t)}, defaultMCPCmd())

	calls := map[string]map[string]any{
		"send":     {"session": "none", "text": "x"},
		"send_key": {"session": "none", "key": "enter"},
		"read":     {"session": "none"},
	}

	for tool, args := range calls {
		text, isErr := callTool(t, cs, tool, args)
		assert.True(t, isErr, tool)
		assert.Contains(t, text, `session "none" is not running`, tool)
	}
}

func TestMCPInvalidSession(t *testing.T) {
	cs := connectMCP(t, &Context{Home: shortTempDir(t)}, defaultMCPCmd())

	text, isErr := callTool(t, cs, "read", map[string]any{"session": "../x"})
	assert.True(t, isErr)
	assert.Contains(t, text, "may contain only")
}

func TestFormatRead(t *testing.T) {
	tests := []struct {
		name    string
		reply   readReply
		waitFor string
		want    string
	}{
		{"output", readReply{Output: "a\n"}, "", "a\n"},
		{"no trailing newline", readReply{Output: "$ "}, "", "$ \n"},
		{"empty", readReply{}, "", "[cotty: no new output]\n"},
		{"truncated", readReply{Output: "a\n", Truncated: true}, "", "[cotty: earlier output was dropped]\na\n"},
		{"idle timeout", readReply{Output: "a\n", TimedOut: true}, "", "a\n[cotty: timed out waiting for output to settle]\n"},
		{"closed", readReply{Output: "bye\n", Closed: true}, "x", "bye\n[cotty: session ended]\n"},
		{"paused", readReply{Paused: true}, "", "[cotty: no new output]\n[cotty: the user paused the output; new output is hidden until they resume it]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatRead(&tt.reply, tt.waitFor))
		})
	}
}
