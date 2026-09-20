package pirpc

import "encoding/json"

// ImageContent is one vision attachment for prompt/steer/follow_up.
// Matches pi's RPC type: {type:"image", data:<base64>, mimeType}.
type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// Command is one JSONL line sent to pi stdin. Only set the fields your
// command needs; the rest are omitted. Message has NO omitempty: pi does
// command.message.startsWith(...) unguarded, so a missing message crashes
// the prompt ("Cannot read properties of undefined") — tray-only sends
// (images, empty text) must still transmit "message":"".
type Command struct {
	ID                 string `json:"id,omitempty"`
	Type               string `json:"type"`
	Message            string `json:"message"`
	Images             []ImageContent `json:"images,omitempty"`
	StreamingBehavior  string `json:"streamingBehavior,omitempty"`
	ShellCommand       string `json:"command,omitempty"`
	SessionPath        string `json:"sessionPath,omitempty"`
	ParentSession      string `json:"parentSession,omitempty"`
	Provider           string `json:"provider,omitempty"`
	ModelID            string `json:"modelId,omitempty"`
	Level              string `json:"level,omitempty"`
	Mode               string `json:"mode,omitempty"`
	Enabled            *bool  `json:"enabled,omitempty"`
	Name               string `json:"name,omitempty"`
	CustomInstructions string `json:"customInstructions,omitempty"`
	OutputPath         string `json:"outputPath,omitempty"`
	EntryID            string `json:"entryId,omitempty"`
	Since              string `json:"since,omitempty"`
	// extension_ui_response fields
	Value     *string `json:"value,omitempty"`
	Confirmed *bool   `json:"confirmed,omitempty"`
	Cancelled *bool   `json:"cancelled,omitempty"`
}

// Response is pi's reply to a command (matched by ID).
type Response struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Event is any non-response line from pi stdout.
type Event struct {
	Type string
	Raw  json.RawMessage
}

// ContentBlock is one entry of an assistant/toolResult content array.
// User echoes may also carry {"type":"image"} blocks (no text payload).
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ImageCount returns how many {"type":"image"} blocks raw holds.
func ImageCount(raw json.RawMessage) int {
	n := 0
	for _, b := range BlocksOf(raw) {
		if b.Type == "image" {
			n++
		}
	}
	return n
}

// AgentMessage is one row of get_messages (role: user/assistant/toolResult/bashExecution).
type AgentMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content,omitempty"`
	Command      string          `json:"command,omitempty"` // bashExecution
	Output       string          `json:"output,omitempty"`  // bashExecution
	ExitCode     int             `json:"exitCode,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"` // toolResult
	ToolName     string          `json:"toolName,omitempty"`   // toolResult
	Details      json.RawMessage `json:"details,omitempty"`    // toolResult (e.g. edit diff)
	IsError      bool            `json:"isError,omitempty"`
	StopReason   string          `json:"stopReason,omitempty"`   // assistant
	ErrorMessage string          `json:"errorMessage,omitempty"` // assistant
}

// TextOf joins all {"type":"text"} blocks; plain-string content also works.
func TextOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	out := ""
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// BlocksOf parses a content array (nil when content is a plain string).
func BlocksOf(raw json.RawMessage) []ContentBlock {
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	return blocks
}

// State mirrors get_state data (subset we display).
type State struct {
	Model struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Provider      string `json:"provider"`
		ContextWindow int    `json:"contextWindow"`
	} `json:"model"`
	ThinkingLevel  string `json:"thinkingLevel"`
	IsStreaming    bool   `json:"isStreaming"`
	SteeringMode   string `json:"steeringMode"`
	FollowUpMode   string `json:"followUpMode"`
	AutoCompaction bool   `json:"autoCompactionEnabled"`
	SessionFile    string `json:"sessionFile"`
	SessionID      string `json:"sessionId"`
	SessionName    string `json:"sessionName"`
	MessageCount   int    `json:"messageCount"`
}

// ModelInfo is one entry of get_available_models.
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// TreeEntry is one session entry; TreeNode forms the get_tree hierarchy.
type TreeEntry struct {
	Type     string       `json:"type"`
	ID       string       `json:"id"`
	Provider string       `json:"provider,omitempty"`
	ModelID  string       `json:"modelId,omitempty"`
	Level    string       `json:"level,omitempty"`
	Message  AgentMessage `json:"message"`
}

type TreeNode struct {
	Entry    TreeEntry  `json:"entry"`
	Children []TreeNode `json:"children"`
}

// Stats mirrors get_session_stats data (subset we display).
type Stats struct {
	SessionID   string  `json:"sessionId"`
	UserMsgs    int     `json:"-"`
	AsstMsgs    int     `json:"-"`
	ToolCalls   int     `json:"toolCalls"`
	ToolResults int     `json:"-"`
	Cost        float64 `json:"cost"`
	In          int     `json:"-"`
	Out         int     `json:"-"`
	CacheRead   int     `json:"-"`
	CacheWrite  int     `json:"-"`
	TokensTotal int     `json:"-"`
	ContextPct  float64 `json:"-"`
	ContextToks int     `json:"-"`
	ContextWin  int     `json:"-"`
}

func (s *Stats) UnmarshalJSON(data []byte) error {
	var wire struct {
		SessionID     string  `json:"sessionId"`
		UserMessages  int     `json:"userMessages"`
		AssistantMsgs int     `json:"assistantMessages"`
		ToolCalls     int     `json:"toolCalls"`
		ToolResults   int     `json:"toolResults"`
		Cost          float64 `json:"cost"`
		Tokens        struct {
			Input      int `json:"input"`
			Output     int `json:"output"`
			CacheRead  int `json:"cacheRead"`
			CacheWrite int `json:"cacheWrite"`
			Total      int `json:"total"`
		} `json:"tokens"`
		ContextUsage *struct {
			Tokens        *int     `json:"tokens"`
			ContextWindow int      `json:"contextWindow"`
			Percent       *float64 `json:"percent"`
		} `json:"contextUsage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	s.SessionID = wire.SessionID
	s.UserMsgs = wire.UserMessages
	s.AsstMsgs = wire.AssistantMsgs
	s.ToolCalls = wire.ToolCalls
	s.ToolResults = wire.ToolResults
	s.Cost = wire.Cost
	s.In = wire.Tokens.Input
	s.Out = wire.Tokens.Output
	s.CacheRead = wire.Tokens.CacheRead
	s.CacheWrite = wire.Tokens.CacheWrite
	s.TokensTotal = wire.Tokens.Total
	if wire.ContextUsage != nil {
		if wire.ContextUsage.Tokens != nil {
			s.ContextToks = *wire.ContextUsage.Tokens
		}
		s.ContextWin = wire.ContextUsage.ContextWindow
		if wire.ContextUsage.Percent != nil {
			s.ContextPct = *wire.ContextUsage.Percent
		}
	}
	return nil
}

// Delta is the assistantMessageEvent inside message_update.
type Delta struct {
	Type         string `json:"type"`
	ContentIndex int    `json:"contentIndex"`
	Delta        string `json:"delta,omitempty"`
	Reason       string `json:"reason,omitempty"` // done: stop | toolUse | ...
	ID           string `json:"id,omitempty"`
	ToolName     string `json:"toolName,omitempty"`
	ToolCall     *struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"toolCall,omitempty"`
}

// MessageUpdate is the parsed message_update event.
type MessageUpdate struct {
	Usage struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cost   struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
	Event Delta `json:"assistantMessageEvent"`
}

// UIRequest is an extension_ui_request (permission dialogs etc.).
type UIRequest struct {
	ID          string   `json:"id"`
	Method      string   `json:"method"`
	Title       string   `json:"title,omitempty"`
	Message     string   `json:"message,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"`
	NotifyType  string   `json:"notifyType,omitempty"`
	StatusKey   string   `json:"statusKey,omitempty"`
	StatusText  string   `json:"statusText,omitempty"`
	Text        string   `json:"text,omitempty"`
}

// Queue mirrors queue_update.
type Queue struct {
	Steering []string `json:"steering"`
	FollowUp []string `json:"followUp"`
}

// RepoCommand is one runnable /command: extension, prompt template or skill.
type RepoCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // extension | prompt | skill
	Location    string `json:"location,omitempty"`
	Path        string `json:"path,omitempty"`
}

// ParseQueue extracts queue_update payloads (arrays may be absent → nil).
func ParseQueue(raw json.RawMessage) Queue {
	var q Queue
	_ = json.Unmarshal(raw, &q)
	return q
}
