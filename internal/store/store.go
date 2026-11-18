// Package store holds Avora's durable state: jobs, node presence and the
// append-only event log. Every state change writes its event in the same
// transaction, so the event log is the complete history of what happened.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/scheduler"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("job belongs to another node")
	ErrConflict  = errors.New("job is not in a state that allows this operation")
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusTimedOut  Status = "timed_out"
)

// Terminal reports whether no further transition is possible.
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled || s == StatusTimedOut
}

type Job struct {
	ID             string           `json:"id"`
	Action         string           `json:"action"`
	Target         scheduler.Target `json:"target"`
	DeviceID       string           `json:"device_id"`
	Status         Status           `json:"status"`
	Output         string           `json:"output,omitempty"`
	Error          string           `json:"error,omitempty"`
	Attempts       int              `json:"attempts"`
	CreatedAt      time.Time        `json:"created_at"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	LeaseExpiresAt *time.Time       `json:"lease_expires_at,omitempty"`
	CompletedAt    *time.Time       `json:"completed_at,omitempty"`
	// Agent is set for agent.run jobs: what to ask the agent and how much it may do.
	Agent *AgentSpec `json:"agent,omitempty"`
}

// ActionAgentRun runs a coding agent CLI (Claude Code or Codex) on a node. Unlike
// ping/diagnostics it is never retried automatically: a second run could
// repeat side effects, so an expired lease marks it timed out.
const ActionAgentRun = "agent.run"

// ActionAgentLogin logs a Claude or Codex subscription account in on a node,
// in that account's own config folder (AgentSpec.Engine and .Account).
const ActionAgentLogin = "agent.login"

// ActionMCPRun runs a stdio plugin in the VPS sandbox on its own lane.
const ActionMCPRun = "mcp.run"

// ActionCodexMain runs the main model on a Codex account: one chat turn
// (AgentSpec.Mode "chat", or "consult" for a colleague consulted inside a
// turn) or the list of the account's models ("models").
const ActionCodexMain = "codex.main"

const (
	CodexChatMode    = "chat"
	CodexConsultMode = "consult"
	CodexModelsMode  = "models"
	// AgyCheckMode is the login of the PC's own agy account (D44): it only
	// checks that agy answers. Other Antigravity accounts sign in (D61).
	AgyCheckMode = "check"
)

// codexNested says whether a codex.main job runs on the nested lane: work
// a running turn waits for, which must never queue behind that turn.
func codexNested(mode string) bool { return mode == CodexConsultMode || mode == CodexModelsMode }

// ClaimSlots says which kinds of background work a node has room for.
type ClaimSlots struct {
	Agents      bool `json:"agents"`
	MCP         bool `json:"mcp,omitempty"`
	CodexMain   bool `json:"codex_main,omitempty"`
	CodexNested bool `json:"codex_nested,omitempty"`
}

// AgentSpec describes one agent run. Mode bounds what the agent may touch:
// "read" (read files and the web) or "edit" (also edit files, no shell).
type AgentSpec struct {
	Engine string `json:"engine"`
	// Model is passed to the CLI as is ("haiku", "sonnet", "gpt-5.5-mini"...);
	// empty uses the CLI's default for the account.
	Model string `json:"model,omitempty"`
	// Effort is the reasoning effort (Codex: low, medium, high, xhigh).
	Effort string `json:"effort,omitempty"`
	// Speed is the generation speed mode ("standard" or "fast") when supported.
	Speed string `json:"speed,omitempty"`
	// Search turns on the CLI's live web search (Codex --search).
	Search  bool   `json:"search,omitempty"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
	Workdir string `json:"workdir,omitempty"`
	// HomeDir is an agent's configured home folder on its home computer
	// (relative to the node workspace, or an absolute path on that computer).
	HomeDir string `json:"home_dir,omitempty"`
	// Folder is an absolute folder on a Windows node (a project's), used
	// instead of Workdir. The node runs there only if its owner picked
	// that folder in the app on that PC.
	Folder     string `json:"folder,omitempty"`
	MaxMinutes int    `json:"max_minutes"`
	// ConversationID is set when the run answers a chat message; Resume is
	// the CLI session it continues (on the same node).
	ConversationID string `json:"conversation_id,omitempty"`
	Resume         string `json:"resume,omitempty"`
	// Bridge is the run's key to Avora's MCP bridge (the owner's Plugins).
	// It works only while the run lasts.
	Bridge string `json:"bridge,omitempty"`
	// Account is the subscription account to run on (a provider id);
	// empty uses the node's own login.
	Account string `json:"account,omitempty"`
	// Owner is the agent that delegated the run and Title what it is
	// about: the app lists the run as a thread under that agent. Mission
	// is the mission the run is a round of, if any.
	Owner      string `json:"owner,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
	AgentEmail string `json:"agent_email,omitempty"`
	Title      string `json:"title,omitempty"`
	Mission    string `json:"mission,omitempty"`
	// Images go to the model with the prompt (Codex --image): pictures the
	// owner attached to the message the run answers.
	Images []SpecImage `json:"images,omitempty"`
}

// SpecImage is an image sent with a run's prompt.
type SpecImage struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Data []byte `json:"data"`
}

type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
	// Stats is recorded in the completion event (tokens, turns, duration), not in the job.
	Stats json.RawMessage `json:"stats,omitempty"`
}

// Event is one immutable entry of the audit log.
type Event struct {
	ID      int64           `json:"id"`
	At      time.Time       `json:"at"`
	Type    string          `json:"type"`
	Actor   string          `json:"actor"`
	JobID   string          `json:"job_id,omitempty"`
	NodeID  string          `json:"node_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type EventFilter struct {
	AfterID int64
	JobID   string
	Limit   int
	// Latest returns the newest matching events, newest first.
	Latest bool
}

const (
	DefaultEventLimit = 100
	MaxEventLimit     = 1000
)

func (f EventFilter) limit() int {
	if f.Limit <= 0 {
		return DefaultEventLimit
	}
	if f.Limit > MaxEventLimit {
		return MaxEventLimit
	}
	return f.Limit
}

// Event types written by the store.
const (
	EventJobCreated   = "job.created"
	EventJobClaimed   = "job.claimed"
	EventJobSucceeded = "job.succeeded"
	EventJobFailed    = "job.failed"
	EventJobCanceled  = "job.canceled"
	EventJobRequeued  = "job.requeued"
	EventJobTimedOut  = "job.timed_out"
	EventNodeOnline   = "node.online"
	EventAgentStep    = "agent.step"

	EventPairingCodeCreated = "node.pairing_code_created"
	EventNodePaired         = "node.paired"
	EventNodeRevoked        = "node.revoked"

	EventAgentSaved          = "agent.saved"
	EventRoutineSaved        = "routine.saved"
	EventRoutineDeleted      = "routine.deleted"
	EventRoutineRun          = "routine.run"
	EventHeartbeat           = "heartbeat"
	EventApprovalRequested   = "approval.requested"
	EventApprovalDecided     = "approval.decided"
	EventGrantCreated        = "grant.created"
	EventGrantDeleted        = "grant.deleted"
	EventConversationCreated = "conversation.created"
	EventConversationArchive = "conversation.archived"
	EventConversationDeleted = "conversation.deleted"
	EventAvatarSaved         = "agent.avatar_saved"
	EventAvatarDeleted       = "agent.avatar_deleted"
	EventMemorySaved         = "agent.memory_saved"
	EventMemoryDeleted       = "agent.memory_deleted"
	EventConnectionSaved     = "connection.saved"
	EventConnectionDeleted   = "connection.deleted"
	EventConnectorCalled     = "connector.called"
	EventMessageCreated      = "message.created"
	EventMessageReacted      = "message.reacted"
	EventLLMFailed           = "llm.failed"
	EventBrainSwitched       = "brain.switched"

	EventPersonCreated    = "person.created"
	EventPersonUpdated    = "person.updated"
	EventWorkspaceCreated = "workspace.created"
	EventWorkspaceUpdated = "workspace.updated"
	EventWorkspaceDeleted = "workspace.deleted"
	EventMemberUpdated    = "workspace.member_updated"
	EventMemberRemoved    = "workspace.member_removed"
	EventInviteCreated    = "invite.created"
	EventInviteDeleted    = "invite.deleted"
	EventInviteAccepted   = "invite.accepted"

	EventSectorSaved     = "sector.saved"
	EventSectorDeleted   = "sector.deleted"
	EventProposalCreated = "proposal.created"
	EventProposalUpdated = "proposal.updated"
)

// Agent is a funcionário (M1): a named role with its own persona, engine and
// permission mode. Every save is a new version; old ones can be restored.
type Agent struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Role    string `json:"role"`
	Persona string `json:"persona"`
	// Engine is "native" (answers in the server), "claude" or "codex" (runs
	// the CLI on a node, continuing the conversation's session).
	Engine   string `json:"engine"`
	Model    string `json:"model"`
	Mode     string `json:"mode"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	// ProjectID is the project the agent works in; empty = universal (sees
	// every agent and project). Set by MoveAgent, not by SaveAgent.
	ProjectID string `json:"project_id"`
	// Responsibilities are the agent's standing goals (spec M1).
	Responsibilities []Responsibility `json:"responsibilities"`
	// Style is how the agent talks (D50).
	Style AgentStyle `json:"style"`
	// Brain is what the agent thinks with (D55); empty = the main model.
	Brain AgentBrain `json:"brain"`
	// Approvals is when it asks the owner before acting (D56): "" (only
	// the locked actions), "always" (anything that changes something) or
	// "never" (it acts on its own; payments still ask).
	Approvals string `json:"approvals"`
	// HomeNodeID pins every execution of this agent to one computer; HomeDir
	// overrides its default working folder there (<workspace>/agents/<slug>).
	HomeNodeID string `json:"home_node_id"`
	HomeDir    string `json:"home_dir"`
	// Sector groups agents visually and organizationally in a workspace.
	Sector string `json:"sector"`
	// ReportsTo is the ID of the manager/lead agent this agent reports to.
	ReportsTo string `json:"reports_to"`
	// Pinned keeps up to 5 agents at the top of the workspace agent list.
	Pinned bool `json:"pinned"`
	// Autonomy bounds scheduled/background execution (can_start_alone, max_parallel 1..5).
	Autonomy AgentAutonomy `json:"autonomy"`
	// Apps lists connector/app IDs assigned to this agent.
	Apps []string `json:"apps,omitempty"`
	// ArchivedAt takes the agent off the team (lists, routines, #time)
	// while its conversations and docs stay; nil = active.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Version    int        `json:"version"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// AgentAutonomy bounds what the scheduler may start on its own for an agent.
type AgentAutonomy struct {
	CanStartAlone *bool `json:"can_start_alone"`
	MaxParallel   int   `json:"max_parallel"`
}

// Bool returns a pointer to v.
func Bool(v bool) *bool { return &v }

// AllowsStartAlone reports whether scheduled work may start without the owner asking.
func (a AgentAutonomy) AllowsStartAlone() bool {
	return a.CanStartAlone == nil || *a.CanStartAlone
}

// LimitParallel returns max_parallel clamped to [1, 5].
func (a AgentAutonomy) LimitParallel() int {
	if a.MaxParallel < 1 {
		return 1
	}
	if a.MaxParallel > 5 {
		return 5
	}
	return a.MaxParallel
}

// NormalizeAutonomy fills defaults (can_start_alone=true, max_parallel in [1,5]) and clones the bool pointer.
func NormalizeAutonomy(a AgentAutonomy) AgentAutonomy {
	return AgentAutonomy{CanStartAlone: Bool(a.AllowsStartAlone()), MaxParallel: a.LimitParallel()}
}

// AgentBrain is what an agent thinks with (D55): a provider of Settings →
// Modelos (an API, or a Codex or Claude account on a node), its model and
// how hard it thinks. An empty provider is the main model.
type AgentBrain struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Effort: "", "low", "medium", "high" or "max".
	Effort string `json:"effort,omitempty"`
	// Fallback: "" or "auto" (default: fall back to the main model when the
	// brain cannot answer or runs out of quota) or "none" (fail and explain).
	Fallback string `json:"fallback,omitempty"`
	// Rotate: nil/true (default) moves to the next account of the same kind
	// when an account hits its quota limit; false ("Só esta conta") stays put.
	Rotate *bool `json:"rotate,omitempty"`
	// Account pins a specific subscription/pool account ID for this agent.
	Account string `json:"account,omitempty"`
	// Context overrides the conversation context window in tokens (200000,
	// 400000 or 1000000, capped at what the model accepts; 0 = default).
	Context int `json:"context,omitempty"`
	// Speed: "", "standard" or "fast" where the provider supports it.
	Speed string `json:"speed,omitempty"`
}

// Rotates reports whether the brain may move to another account of the same
// kind when the current account hits its limit. Defaults to true when unset.
func (b AgentBrain) Rotates() bool {
	return b.Rotate == nil || *b.Rotate
}

// FallbackMode returns "none" when fallback is disabled, or "auto" otherwise.
func (b AgentBrain) FallbackMode() string {
	if b.Fallback == "none" {
		return "none"
	}
	return "auto"
}

// Clone returns a deep copy of AgentBrain so pointer fields are never aliased.
func (b AgentBrain) Clone() AgentBrain {
	if b.Rotate != nil {
		v := *b.Rotate
		b.Rotate = &v
	}
	return b
}

// AgentStyle is how an agent talks, set by the owner: its voice in plain
// words, how often it joins in #time, emojis and how long it writes.
type AgentStyle struct {
	// Voice: "descontraída, direta, usa gíria de SP".
	Voice string `json:"voice,omitempty"`
	// Talk: how often it joins in #time without being called: "pouco",
	// "normal" (empty) or "muito".
	Talk string `json:"talk,omitempty"`
	// Emoji: "nunca", "as_vezes" (empty) or "bastante".
	Emoji string `json:"emoji,omitempty"`
	// Length: "curta", "normal" (empty) or "longa".
	Length string `json:"length,omitempty"`
}

// Responsibility is a standing goal: what to keep true, how to measure it and
// how often to check.
type Responsibility struct {
	ID      string `json:"id"`
	Goal    string `json:"goal"`
	Metric  string `json:"metric"`
	Cadence string `json:"cadence"`
}

// Schedule says when a routine runs: every Every minutes, or at At ("08:00",
// server time zone) on Days (0 = Sunday; empty = every day).
type Schedule struct {
	At    string `json:"at,omitempty"`
	Days  []int  `json:"days,omitempty"`
	Every int    `json:"every,omitempty"`
	// Date (YYYY-MM-DD, with At) makes it run once, that day, and then turn
	// itself off: a reminder.
	Date string `json:"date,omitempty"`
}

// Trigger makes a routine run on an event instead of a time. Kind
// "conector" checks a connector read tool (Tool, Args) every Every minutes
// and fires with the items it had not seen; kind "no" fires when Node goes
// Event ("online" or "offline").
type Trigger struct {
	Kind      string         `json:"kind"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Every     int            `json:"every,omitempty"`
	Node      string         `json:"node,omitempty"`
	NodeName  string         `json:"node_name,omitempty"`
	Event     string         `json:"event,omitempty"`
	Seen      []string       `json:"seen,omitempty"`
	LastError string         `json:"last_error,omitempty"`
}

// Routine is a scheduled prompt one agent runs (spec M5). Its answer goes to
// #time or to the routine's own conversation with the agent.
type Routine struct {
	ID               string   `json:"id"`
	AgentID          string   `json:"agent_id"`
	ResponsibilityID string   `json:"responsibility_id,omitempty"`
	Title            string   `json:"title"`
	Prompt           string   `json:"prompt"`
	Schedule         Schedule `json:"schedule"`
	Deliver          string   `json:"deliver"` // "time" or "conversation"
	// Skill is a skill the routine follows (a workflow); empty = none.
	Skill string `json:"skill,omitempty"`
	// Trigger, when set, runs the routine on an event (Schedule is empty).
	Trigger        *Trigger   `json:"trigger,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Enabled        bool       `json:"enabled"`
	NextRun        *time.Time `json:"next_run,omitempty"`
	LastRun        *time.Time `json:"last_run,omitempty"`
	// LastStatus is "", "running", "ok", "failed" or "skipped".
	LastStatus  string    `json:"last_status,omitempty"`
	LastPreview string    `json:"last_preview,omitempty"`
	Failures    int       `json:"failures"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Approval is an agent action waiting for the owner (M6): it runs only once
// approved, and expires when nobody answers in time.
type Approval struct {
	ID             string     `json:"id"`
	AgentID        string     `json:"agent_id"`
	ConversationID string     `json:"conversation_id"`
	TurnID         string     `json:"turn_id"`
	Action         string     `json:"action"`  // policy key, e.g. "claude_code.edit"
	Summary        string     `json:"summary"` // what will happen, in words
	Status         string     `json:"status"`  // pending, approved, denied, expired
	Always         bool       `json:"always"`  // approved as "sempre permitir"
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	DecidedBy      string     `json:"decided_by,omitempty"`
}

// Connection is one connected account of an app (a Plugin): which account,
// its state and its credential, sealed by the vault. An app may have several
// (two Gmails). The secret never leaves the server: it is not serialized and
// no API returns it.
type Connection struct {
	// ID names the connection; the first account of an app uses the kind.
	ID        string     `json:"id"`
	Kind      string     `json:"kind"` // the connector: google, clickup, discord...
	Account   string     `json:"account"`
	Status    string     `json:"status"` // ok, error, reconnect, pending
	LastError string     `json:"last_error,omitempty"`
	LastOK    *time.Time `json:"last_ok,omitempty"`
	// Agents that may use it; empty = every agent.
	Agents []string `json:"agents"`
	Secret []byte   `json:"-"`
	// Meta is the connector's own state (an MCP app's tools); not secret,
	// but not part of the API either.
	Meta      []byte    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Grant is a standing "sempre permitir": the agent may take the action
// without asking until the owner revokes it.
type Grant struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// AgentVersion is a saved state of an agent.
type AgentVersion struct {
	AgentID   string    `json:"agent_id"`
	Version   int       `json:"version"`
	Agent     Agent     `json:"agent"`
	CreatedAt time.Time `json:"created_at"`
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Title   string `json:"title"`
	Engine  string `json:"engine"`
	Model   string `json:"model"`
	// SessionID is the agent CLI session this conversation continues, which
	// lives on SessionNode.
	SessionID   string `json:"session_id,omitempty"`
	SessionNode string `json:"session_node,omitempty"`
	// JobID is the agent run answering the last message, while it runs.
	JobID string `json:"job_id,omitempty"`
	// ArchivedAt hides the conversation from the list until it is unarchived.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Preview    string     `json:"preview"`
	LastRole   string     `json:"last_role,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// AgentMemory is one thing an agent or workspace team learned and keeps across conversations.
type AgentMemory struct {
	ID          int64      `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	AgentID     string     `json:"agent_id"`
	Scope       string     `json:"scope"` // "agent" or "team"
	Kind        string     `json:"kind"`  // "decision", "fact", "preference", "lesson", "rule"
	Content     string     `json:"content"`
	Source      string     `json:"source"` // "conversa", "rotina", "dono", "#time", ...
	Verified    bool       `json:"verified"`
	Pinned      bool       `json:"pinned"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	Occurrences int        `json:"occurrences"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// MemoryFilter narrows a SearchMemories query.
type MemoryFilter struct {
	WorkspaceID string
	AgentID     string
	// AgentOrTeam, when set with AgentID and WorkspaceID, returns both the agent's own
	// memories and team memories in WorkspaceID.
	AgentOrTeam     bool
	Scope           string // "", "agent", "team"
	Kind            string // "", "decision", "fact", "preference", "lesson", "rule"
	Query           string
	Verified        *bool
	Pinned          *bool
	IncludeArchived bool
	ArchivedOnly    bool
	Limit           int
}

// MemoryPatch selectively updates fields of an existing memory.
type MemoryPatch struct {
	Content     *string
	Kind        *string
	Scope       *string
	Source      *string
	Verified    *bool
	Pinned      *bool
	Archived    *bool
	Occurrences *int
}

// Sector organizes agents inside a workspace with an optional leader.
type Sector struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Icon        string    `json:"icon"`
	LeadAgentID string    `json:"lead_agent_id"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Proposal is a suggested change to an agent's instructions, role or apps.
type Proposal struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	FromAgent      string     `json:"from_agent"`
	TargetAgent    string     `json:"target_agent"`
	Field          string     `json:"field"` // "instructions", "role", "apps"
	Before         string     `json:"before"`
	After          string     `json:"after"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"` // "pending", "applied", "rejected", "undone"
	PreVersion     int        `json:"pre_version,omitempty"`
	AppliedVersion int        `json:"applied_version,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
}

// ProposalFilter narrows ListProposals.
type ProposalFilter struct {
	WorkspaceID    string
	ConversationID string
	TargetAgent    string
	Status         string
	Limit          int
}

// Avatar is an agent's photo.
type Avatar struct {
	AgentID     string
	Data        []byte
	ContentType string
	UpdatedAt   time.Time
}

// previewOf is a message as shown in a conversation list: one line, short.
func previewOf(m ChatMessage) string {
	content := m.Content
	if content == "" {
		content = m.Error
	}
	p := strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(p) > 140 {
		p = string([]rune(p)[:140])
	}
	return p
}

// ChatMessage is one turn of a conversation. Token counts and duration are
// filled for assistant messages.
type ChatMessage struct {
	ID               int64  `json:"id"`
	ConversationID   string `json:"conversation_id"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	Model            string `json:"model,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	DurationMS       int    `json:"duration_ms,omitempty"`
	// AgentID is who wrote an answer in the team channel (empty elsewhere:
	// the conversation's own agent).
	AgentID string `json:"agent_id,omitempty"`
	// JobID, Steps and Error are set for answers from agent CLI runs.
	JobID string          `json:"job_id,omitempty"`
	Steps json.RawMessage `json:"steps,omitempty"`
	Error string          `json:"error,omitempty"`
	// ReplyTo is the message this one answers (0: none); the agent reads the
	// quoted message with it.
	ReplyTo int64 `json:"reply_to,omitempty"`
	// Reactions are emoji agents put on the message instead of answering
	// (one per agent; D50).
	Reactions []Reaction `json:"reactions,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Reaction is an agent's emoji on a message.
type Reaction struct {
	AgentID string `json:"agent_id"`
	Emoji   string `json:"emoji"`
}

// maxReactions bounds the emoji on one message.
const maxReactions = 12

// withReaction puts r on the list: an agent's new emoji replaces its old one.
func withReaction(list []Reaction, r Reaction) []Reaction {
	out := []Reaction{}
	for _, x := range list {
		if x.AgentID != r.AgentID {
			out = append(out, x)
		}
	}
	out = append(out, r)
	if len(out) > maxReactions {
		out = out[len(out)-maxReactions:]
	}
	return out
}

// messagePayload is what the audit log records about a message: never its content.
func messagePayload(m ChatMessage) map[string]any {
	return map[string]any{
		"message_id": m.ID, "conversation_id": m.ConversationID, "role": m.Role, "model": m.Model,
		"chars": len(m.Content), "prompt_tokens": m.PromptTokens, "completion_tokens": m.CompletionTokens,
		"duration_ms": m.DurationMS, "agent_id": m.AgentID,
	}
}

// Node is a paired machine that runs avora-runtime.
type Node struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	Name        string           `json:"name"`
	Kind        scheduler.Target `json:"kind"`
	CreatedAt   time.Time        `json:"created_at"`
	RevokedAt   *time.Time       `json:"revoked_at,omitempty"`
	LastSeen    *time.Time       `json:"last_seen,omitempty"`
}

// PairingCode lets one new node pair itself. Only its hash is stored.
type PairingCode struct {
	Hash        []byte
	WorkspaceID string
	Name        string
	Kind        scheduler.Target
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Actors that appear in events.
const (
	ActorAdmin  = "admin"
	ActorSystem = "system"
)

func NodeActor(id string) string   { return "node:" + id }
func PersonActor(id string) string { return "person:" + id }

type Store interface {
	// CreateJob stores a new queued job and records why it was routed where it was.
	CreateJob(ctx context.Context, job Job, reason, actor string) (Job, error)
	GetJob(ctx context.Context, id string) (Job, error)
	// ListJobs returns the most recent jobs first.
	ListJobs(ctx context.Context, limit int) ([]Job, error)
	// ClaimJob hands the oldest queued job of a node to it, under a lease.
	// It returns nil when there is nothing to do.
	// Agent jobs are skipped unless allowAgent is set (the node has a free slot).
	ClaimJob(ctx context.Context, nodeID string, now time.Time, lease time.Duration, slots ClaimSlots) (*Job, error)
	// RenewLeases extends the leases of the given jobs while they still run on
	// the node, and returns the ones that no longer run there (canceled,
	// timed out, revoked) so the node can stop them.
	RenewLeases(ctx context.Context, nodeID string, ids []string, now time.Time, lease time.Duration) (stop []string, err error)
	CompleteJob(ctx context.Context, id, nodeID string, result Result, now time.Time) (Job, error)
	CancelJob(ctx context.Context, id, actor string, now time.Time) (Job, error)
	// ExpireLeases requeues running jobs whose lease ran out, or marks them
	// timed out once they used maxAttempts (agent jobs: at once). It returns the jobs it changed.
	ExpireLeases(ctx context.Context, now time.Time, maxAttempts int) ([]Job, error)
	// RecordHeartbeat updates presence and logs node.online when the node
	// had been silent for longer than onlineTTL.
	RecordHeartbeat(ctx context.Context, nodeID string, now time.Time, onlineTTL time.Duration) error
	Events(ctx context.Context, filter EventFilter) ([]Event, error)

	CreatePairingCode(ctx context.Context, code PairingCode, actor string) error
	// RedeemPairingCode turns an unused, unexpired code into a node with the
	// given token hash. Unknown, used and expired codes all yield ErrNotFound.
	RedeemPairingCode(ctx context.Context, codeHash []byte, nodeID string, tokenHash []byte, now time.Time) (Node, error)
	// NodeByToken finds an active (not revoked) node by its token hash.
	NodeByToken(ctx context.Context, tokenHash []byte) (Node, error)
	// ListNodes returns every node, revoked ones included, with presence.
	ListNodes(ctx context.Context) ([]Node, error)
	// RevokeNode disables a node and cancels its unfinished jobs.
	RevokeNode(ctx context.Context, id, actor string, now time.Time) (Node, error)

	ListAgents(ctx context.Context) ([]Agent, error)
	GetAgent(ctx context.Context, id string) (Agent, error)
	// SaveAgent creates the agent (version 1) or stores its next version.
	SaveAgent(ctx context.Context, a Agent, actor string, now time.Time) (Agent, error)
	// AgentVersions returns an agent's saved versions, newest first.
	AgentVersions(ctx context.Context, id string) ([]AgentVersion, error)
	// ListAgents leaves archived agents out; ListArchivedAgents returns
	// only them. ArchiveAgent archives (true) or brings one back (false).
	ListArchivedAgents(ctx context.Context) ([]Agent, error)
	ArchiveAgent(ctx context.Context, id string, archived bool, actor string, now time.Time) (Agent, error)
	// AgentUsage sums the agent's answers since a time, per day (in the
	// zone) and model.
	AgentUsage(ctx context.Context, agentID string, since time.Time, zone *time.Location) ([]UsageRow, error)

	// ListRoutines returns the routines, oldest first, only the given
	// agent's when agentID is set.
	ListRoutines(ctx context.Context, agentID string) ([]Routine, error)
	GetRoutine(ctx context.Context, id string) (Routine, error)
	// SaveRoutine creates or replaces a routine. A non-empty actor records a
	// routine.saved event (a change of settings, not a run's status).
	SaveRoutine(ctx context.Context, r Routine, actor string, now time.Time) (Routine, error)
	DeleteRoutine(ctx context.Context, id, actor string, now time.Time) error

	// CreateApproval records an action waiting for the owner's decision.
	CreateApproval(ctx context.Context, a Approval, actor string) (Approval, error)
	GetApproval(ctx context.Context, id string) (Approval, error)
	// ListApprovals returns approvals newest first, only those with status
	// when it is not empty.
	ListApprovals(ctx context.Context, status string, limit int) ([]Approval, error)
	// DecideApproval moves a pending approval to approved, denied or
	// expired, once (ErrConflict after that). Approved with always also
	// grants the agent the action from then on, in the same step.
	DecideApproval(ctx context.Context, id, status string, always bool, actor string, now time.Time) (Approval, error)
	// ListGrants returns the standing permissions, only the agent's when
	// agentID is not empty.
	ListGrants(ctx context.Context, agentID string) ([]Grant, error)
	DeleteGrant(ctx context.Context, id, actor string, now time.Time) error

	// ListConnections returns every connection by kind, then oldest first:
	// an app's first account is its default.
	ListConnections(ctx context.Context) ([]Connection, error)
	GetConnection(ctx context.Context, id string) (Connection, error)
	// SaveConnection creates or replaces a connection (an empty ID means the
	// kind). A non-empty actor records connection.saved (a new account or
	// settings, not a token refresh or a status change).
	SaveConnection(ctx context.Context, c Connection, actor string, now time.Time) (Connection, error)
	DeleteConnection(ctx context.Context, id, actor string, now time.Time) error

	// Providers are the model sources of Settings, oldest first.
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id string) (Project, error)
	SaveProject(ctx context.Context, p Project, actor string, now time.Time) (Project, error)
	DeleteProject(ctx context.Context, id, actor string, now time.Time) error
	ListChannels(ctx context.Context) ([]Channel, error)
	GetChannel(ctx context.Context, id string) (Channel, error)
	SaveChannel(ctx context.Context, c Channel, actor string, now time.Time) (Channel, error)
	ListSkills(ctx context.Context) ([]Skill, error)
	GetSkill(ctx context.Context, id string) (Skill, error)
	SaveSkill(ctx context.Context, s Skill, actor string, now time.Time) (Skill, error)
	DeleteSkill(ctx context.Context, id, actor string, now time.Time) error
	ListMissions(ctx context.Context, f MissionFilter) ([]Mission, error)
	GetMission(ctx context.Context, id string) (Mission, error)
	MissionByJob(ctx context.Context, jobID string) (Mission, error)
	SaveMission(ctx context.Context, m Mission, actor string, now time.Time) (Mission, error)
	ListGoals(ctx context.Context, agentID string) ([]Goal, error)
	GetGoal(ctx context.Context, id string) (Goal, error)
	SaveGoal(ctx context.Context, g Goal, actor string, now time.Time) (Goal, error)
	GetSummary(ctx context.Context, convID string) (ConversationSummary, error)
	SaveFile(ctx context.Context, f File) (File, error)
	GetFile(ctx context.Context, id string) (File, error)
	// Docs (D60): the team's documents, with every version kept.
	ListDocs(ctx context.Context, f DocFilter) ([]Doc, error)
	// GetDoc returns a doc with its content; version > 0 asks for an older one.
	GetDoc(ctx context.Context, id string, version int) (Doc, error)
	DocVersions(ctx context.Context, id string) ([]DocVersion, error)
	CreateDoc(ctx context.Context, d Doc, note, actor string, now time.Time) (Doc, error)
	UpdateDoc(ctx context.Context, id string, c DocChange, actor string, now time.Time) (Doc, error)
	// TrashDoc moves a doc to the trash or, with trash false, back.
	TrashDoc(ctx context.Context, id string, trash bool, actor string, now time.Time) (Doc, error)
	// PurgeDocs deletes for good the trashed doc id, or (id empty) every doc
	// trashed before the given time.
	PurgeDocs(ctx context.Context, id string, before time.Time, actor string, now time.Time) (int, error)
	SaveSummary(ctx context.Context, s ConversationSummary, actor string, now time.Time) (ConversationSummary, error)
	MoveAgent(ctx context.Context, agentID, projectID, actor string, now time.Time) (Agent, error)
	ListProviders(ctx context.Context) ([]Provider, error)
	GetProvider(ctx context.Context, id string) (Provider, error)
	SaveProvider(ctx context.Context, p Provider, actor string, now time.Time) (Provider, error)
	DeleteProvider(ctx context.Context, id, actor string, now time.Time) error
	// Settings are small owner choices by key (ErrNotFound when unset).
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
	SetSetting(ctx context.Context, key string, value json.RawMessage, actor string, now time.Time) error

	CreateConversation(ctx context.Context, c Conversation, actor string) (Conversation, error)
	GetConversation(ctx context.Context, id string) (Conversation, error)
	// ListConversations returns the most recently active conversations first,
	// only the given agent's when agentID is set.
	ListConversations(ctx context.Context, agentID string, limit int) ([]Conversation, error)
	// StartConversationJob marks the conversation as being answered by jobID.
	// It fails with ErrConflict while another answer is still running.
	StartConversationJob(ctx context.Context, convID, jobID string) error
	// FinishConversationJob stores the answer of jobID and clears the mark, in
	// one step and only once: later calls for the same job get ErrConflict.
	// A non-empty sessionID becomes the session the conversation continues.
	FinishConversationJob(ctx context.Context, convID, jobID string, answer ChatMessage, sessionID, sessionNode, actor string) (ChatMessage, error)
	// AppendMessage stores a message and bumps the conversation's updated_at.
	AppendMessage(ctx context.Context, m ChatMessage, actor string) (ChatMessage, error)
	Messages(ctx context.Context, conversationID string) ([]ChatMessage, error)
	// React puts an agent's emoji on a message (replacing its earlier one).
	React(ctx context.Context, conversationID string, messageID int64, r Reaction, actor string, now time.Time) (ChatMessage, error)
	// ArchiveConversation hides (or, with archived false, shows again) a
	// conversation in the list.
	ArchiveConversation(ctx context.Context, id string, archived bool, actor string, now time.Time) (Conversation, error)
	// DeleteConversation removes a conversation and its messages; ErrConflict
	// while it is being answered.
	DeleteConversation(ctx context.Context, id, actor string, now time.Time) error

	// SetAvatar stores an agent's photo (already compressed by the app).
	SetAvatar(ctx context.Context, agentID string, data []byte, contentType, actor string, now time.Time) error
	GetAvatar(ctx context.Context, agentID string) (Avatar, error)
	DeleteAvatar(ctx context.Context, agentID, actor string, now time.Time) error
	// AvatarTimes says when each agent with a photo last changed it.
	AvatarTimes(ctx context.Context) (map[string]time.Time, error)

	// AddMemory stores a memory of an agent; ListMemories returns them
	// newest first, only those containing every word of query when set.
	AddMemory(ctx context.Context, m AgentMemory, actor string, now time.Time) (AgentMemory, error)
	ListMemories(ctx context.Context, agentID, query string, limit int) ([]AgentMemory, error)
	UpdateMemory(ctx context.Context, id int64, content, actor string, now time.Time) (AgentMemory, error)
	DeleteMemory(ctx context.Context, id int64, actor string, now time.Time) (AgentMemory, error)
	GetMemory(ctx context.Context, id int64) (AgentMemory, error)
	PatchMemory(ctx context.Context, id int64, patch MemoryPatch, actor string, now time.Time) (AgentMemory, error)
	SearchMemories(ctx context.Context, f MemoryFilter) ([]AgentMemory, error)

	ListSectors(ctx context.Context) ([]Sector, error)
	GetSector(ctx context.Context, id string) (Sector, error)
	SaveSector(ctx context.Context, s Sector, actor string, now time.Time) (Sector, error)
	DeleteSector(ctx context.Context, id, actor string, now time.Time) error
	ReorderSectors(ctx context.Context, workspaceID string, ids []string, actor string, now time.Time) ([]Sector, error)

	CreateProposal(ctx context.Context, p Proposal, actor string, now time.Time) (Proposal, error)
	GetProposal(ctx context.Context, id string) (Proposal, error)
	ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error)
	UpdateProposal(ctx context.Context, p Proposal, actor string, now time.Time) (Proposal, error)
	// RecordEvent appends an event that has no state change of its own,
	// such as a failed model call.
	RecordEvent(ctx context.Context, e Event) error

	CountPeople(ctx context.Context) (int, error)
	CreatePerson(ctx context.Context, p Person, onlyFirst bool, now time.Time) (Person, bool, error)
	GetPerson(ctx context.Context, id string) (Person, error)
	PersonByEmail(ctx context.Context, email string) (Person, error)
	UpdatePerson(ctx context.Context, p Person) (Person, error)
	TouchPerson(ctx context.Context, id string, now time.Time) error
	CreateSession(ctx context.Context, s Session) error
	PersonBySession(ctx context.Context, tokenHash []byte, now time.Time) (Person, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error

	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	ListPersonWorkspaces(ctx context.Context, personID string) ([]PersonWorkspace, error)
	GetWorkspace(ctx context.Context, id string) (Workspace, error)
	CreateWorkspace(ctx context.Context, w Workspace, now time.Time) (Workspace, error)
	UpdateWorkspace(ctx context.Context, w Workspace) (Workspace, error)
	DeleteWorkspace(ctx context.Context, id string) error
	ListMembers(ctx context.Context, workspaceID string) ([]Member, error)
	GetMember(ctx context.Context, workspaceID, personID string) (Member, error)
	SetMemberRole(ctx context.Context, workspaceID, personID, role string) (Member, error)
	ApproveMember(ctx context.Context, workspaceID, personID string) (Member, error)
	RemoveMember(ctx context.Context, workspaceID, personID string) error
	CreateInvite(ctx context.Context, inv Invite) (Invite, error)
	ListInvites(ctx context.Context, workspaceID string) ([]Invite, error)
	DeleteInvite(ctx context.Context, workspaceID, inviteID string) error
	InviteByCode(ctx context.Context, codeHash []byte, now time.Time) (Invite, error)
	AcceptInvite(ctx context.Context, codeHash []byte, personID string, now time.Time) (Member, error)

	Durable() bool
	Close()
}

// ts normalizes timestamps to what Postgres stores, so both stores agree.
func ts(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

func payload(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage(`{}`)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func nonEmpty(p json.RawMessage) json.RawMessage {
	if len(p) == 0 {
		return json.RawMessage(`{}`)
	}
	return p
}

func completionPayload(r Result) map[string]any {
	p := map[string]any{"error": r.Error}
	if len(r.Stats) > 0 && json.Valid(r.Stats) {
		p["stats"] = r.Stats
	}
	return p
}

func completionEvent(success bool) string {
	if success {
		return EventJobSucceeded
	}
	return EventJobFailed
}
