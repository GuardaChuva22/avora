package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Every agent thinks with the native model and acts through tools (spec
// principle 1: Claude Code and Codex are the heavy workers, the Avora
// orchestrates). A turn is one agent answering one message: the model calls
// tools in rounds (delegating to Claude Code or Codex, creating a routine,
// asking a colleague) until it writes the answer.
//
// A turn runs in the background, detached from the request that started it:
// a delegation can take many minutes and the app may close meanwhile. Its
// steps go to the audit log under the turn's id (so /v1/events?job=<id>
// follows it) and to open streams through the hub.

// TurnMinutes bounds one turn, a wait for approval and a delegation
// included.
const TurnMinutes = AgentReplyMinutes + MaxApprovalMinutes + 5

// ToolEngine is a chat engine that can call tools. The native engine is one;
// an engine without tools still answers, just without acting.
type ToolEngine interface {
	Run(ctx context.Context, system string, history []store.ChatMessage, tools []llm.Tool, run engine.ToolRunner, onDelta func(llm.Delta)) (llm.Response, error)
}

// turn is one agent answer in flight.
type turn struct {
	agent   store.Agent
	team    []store.Agent
	id      string // steps are recorded and published under it
	conv    string
	channel bool  // in #time: colleagues are called with @, not consulted
	parent  *turn // a consulted colleague reports its steps to the asker
	onDelta func(llm.Delta)

	// askedBrowser: the owner marked @Navegador, so the browser may open
	// even the pages of a connected app.
	askedBrowser bool

	mu    sync.Mutex
	steps []AgentStep
	job   string  // the last job it delegated
	apps  appTurn // apps on demand (native engine only)
}

// step records what the agent did, for the live view and the answer's cards.
func (s *Server) step(t *turn, st AgentStep) {
	if t.parent != nil {
		st.Tool = t.agent.Name + " · " + st.Tool
		s.step(t.parent, st)
		return
	}
	st.Tool, st.Summary, st.Detail = truncate(st.Tool, 80), truncate(st.Summary, maxStepField), truncate(st.Detail, maxStepDetail)
	t.mu.Lock()
	if len(t.steps) < maxSteps {
		t.steps = append(t.steps, st)
	}
	t.mu.Unlock()
	payload, _ := json.Marshal(st)
	_ = s.store.RecordEvent(context.Background(), store.Event{
		At: s.now(), Type: store.EventAgentStep, Actor: "agent:" + t.agent.ID, JobID: t.id, Payload: payload,
	})
	s.hub.publish(t.id, hubEvent{"step", st})
}

// runTurn has the agent answer history and returns the answer to store.
func (s *Server) runTurn(ctx context.Context, t *turn, system string, history []store.ChatMessage) store.ChatMessage {
	started := time.Now()
	answer := store.ChatMessage{Role: "assistant"}
	eng, handled, lerr := s.resolveLocalTurnEngine(ctx, t)
	fallback := ""
	if lerr != nil {
		answer.Error, answer.CreatedAt = publicLLMError(lerr), s.now()
		return answer
	}
	if !handled {
		var hardErr error
		eng, fallback, hardErr = s.selectBrain(ctx, t.agent)
		if hardErr != nil {
			s.recordBrainSwitch(t, "failed", hardErr.Error())
			t.mu.Lock()
			if len(t.steps) > 0 {
				answer.Steps, _ = json.Marshal(t.steps)
			}
			t.mu.Unlock()
			answer.Error, answer.CreatedAt = hardErr.Error(), s.now()
			return answer
		}
	}
	if eng == nil {
		answer.Error, answer.CreatedAt = "nenhuma IA conectada (Configurações → Modelos)", s.now()
		return answer
	}
	if fallback != "" {
		s.recordBrainSwitch(t, "fallback", fallback)
	}
	system += s.turnBlocks(ctx, t.agent)
	_, sees := eng.(*codexEngine)
	system += s.attachmentBlock(ctx, t, history, sees)
	system += s.mentionBlock(ctx, t.agent, history)
	system += s.citedDocsBlock(ctx, t, history)
	if win := s.agentContextWindow(ctx, t.agent); win > 0 {
		history = trimHistoryToWindow(history, max(4096, win-estTokens(system)-8192))
	}
	t.askedBrowser = ownerAskedBrowser(history)
	run := func(ctx context.Context, c llm.ToolCall) string { return s.callTool(ctx, t, c) }
	execEngine := func(chosen ChatEngine) (llm.Response, error) {
		switch te := chosen.(type) {
		case *codexEngine:
			return s.runCodexTurn(ctx, t, te, system, history)
		case *claudeEngine:
			return s.runClaudeTurn(ctx, t, te, system, history)
		case *agyEngine:
			return s.runAgyTurn(ctx, t, te, system, history)
		case *engine.Native:
			s.startApps(ctx, t, history)
			return te.RunDynamic(ctx, system, history, func() []llm.Tool { return s.toolsFor(t) }, run, t.onDelta)
		case ToolEngine:
			return te.Run(ctx, system, history, s.toolsFor(t), run, t.onDelta)
		default:
			return chosen.Reply(ctx, system, history, t.onDelta)
		}
	}
	resp, err := execEngine(eng)
	if err != nil && ctx.Err() == nil && t.agent.Brain.Provider != "" && fallback == "" && t.agent.Brain.FallbackMode() == "auto" {
		if main := s.chatEngine(); main != nil && main.Provider() != eng.Provider() {
			why := publicLLMError(err)
			fbLine := "o cérebro de " + t.agent.Name + " (" + s.brainName(ctx, t.agent.Brain) + ") falhou (" + why + "); respondi com o modelo automático"
			s.recordBrainSwitch(t, "fallback", fbLine)
			eng = s.applyBrainOptionsToMain(ctx, main, store.Agent{Brain: store.AgentBrain{Context: t.agent.Brain.Context, Speed: t.agent.Brain.Speed}})
			resp, err = execEngine(eng)
		}
	}
	if err == nil && strings.TrimSpace(resp.Content) == "" {
		err = errors.New("model returned an empty answer")
	}
	switch {
	case err != nil && errors.Is(ctx.Err(), context.Canceled):
		answer.Error = "parado por você"
	case err != nil:
		s.recordLLMFailure(t.conv, err, time.Since(started))
		answer.Error = publicLLMError(err)
	default:
		answer.Content, answer.Model = resp.Content, resp.Model
		if answer.Model == "" {
			answer.Model = eng.Model()
		}
		answer.PromptTokens, answer.CompletionTokens = resp.Usage.PromptTokens, resp.Usage.CompletionTokens
	}
	t.mu.Lock()
	if len(t.steps) > 0 {
		answer.Steps, _ = json.Marshal(t.steps)
	}
	t.mu.Unlock()
	answer.DurationMS = int(time.Since(started).Milliseconds())
	answer.CreatedAt = s.now()
	return answer
}

func newID() string {
	raw, err := randomBytes(12)
	if err != nil {
		panic(err) // the system's random source failed
	}
	return hex.EncodeToString(raw)
}

// turnState holds the turns in flight, to stop them.
type turnState struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func (ts *turnState) add(id string, cancel context.CancelFunc) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.cancel == nil {
		ts.cancel = map[string]context.CancelFunc{}
	}
	ts.cancel[id] = cancel
}

func (ts *turnState) remove(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.cancel, id)
}

// stop cancels a turn; false when there is none with this id.
func (ts *turnState) stop(id string) bool {
	ts.mu.Lock()
	cancel := ts.cancel[id]
	ts.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return cancel != nil
}

// startTurn stores content as a message to the agent in conv and starts the
// answer in the background, under id (subscribe to the hub before calling).
// extra goes to the model only (a routine's state snapshot). onDone runs
// with the stored answer.
func (s *Server) startTurn(ctx context.Context, id string, conv store.Conversation, agent store.Agent, content string, replyTo int64, extra, actor string, onDone func(store.ChatMessage)) (store.ChatMessage, error) {
	if err := s.store.StartConversationJob(ctx, conv.ID, id); err != nil {
		return store.ChatMessage{}, err
	}
	user, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: conv.ID, Role: "user", Content: content, ReplyTo: replyTo, CreatedAt: s.now(),
	}, actor)
	if err != nil {
		s.failAgentReply(conv.ID, id, "não consegui guardar a mensagem")
		return store.ChatMessage{}, err
	}
	history, err := s.store.Messages(ctx, conv.ID)
	if err != nil || len(history) == 0 {
		s.failAgentReply(conv.ID, id, "não consegui ler a conversa")
		return user, errors.New("reading the conversation failed")
	}
	s.observeOwnerMessage(ctx, conv.ID, agent, history[:len(history)-1], content, actor)
	sum, _ := s.store.GetSummary(ctx, conv.ID)
	history = compacted(history, sum)
	if extra != "" {
		history[len(history)-1].Content += "\n\n" + extra
	}
	all, _ := s.store.ListAgents(ctx)
	history = colleaguesSigned(history, agent, all)
	team := visibleTeam(agent, all)
	tctx, cancel := context.WithTimeout(context.Background(), TurnMinutes*time.Minute)
	s.turns.add(id, cancel)
	t := &turn{agent: agent, team: team, id: id, conv: conv.ID}
	t.onDelta = func(d llm.Delta) {
		if d.Reasoning != "" {
			s.hub.publish(id, hubEvent{"reasoning", map[string]string{"content": d.Reasoning}})
		}
		if d.Content != "" {
			s.hub.publish(id, hubEvent{"delta", map[string]string{"content": d.Content}})
		}
	}
	go func() {
		defer cancel()
		defer s.turns.remove(id)
		answer := s.runTurn(tctx, t, nativeSystem(agent, team)+summaryBlock(sum), history)
		msg, err := s.store.FinishConversationJob(context.Background(), conv.ID, id, answer, "", "", s.engineActor())
		if err != nil {
			log.Printf("conversation %s: storing the answer of turn %s failed: %v", conv.ID, id, err)
			msg = answer
		}
		s.hub.publish(id, hubEvent{"done", msg})
		if onDone != nil {
			onDone(msg)
		}
		s.maybeCompact(conv.ID)
	}()
	return user, nil
}

// RecoverTurns ends the answers that were in flight when the server stopped:
// a turn lives in memory, so after a restart nobody would finish it.
func (s *Server) RecoverTurns(ctx context.Context) error {
	if err := s.expirePending(ctx); err != nil {
		return err
	}
	convs, err := s.store.ListConversations(ctx, "", 1000)
	if err != nil {
		return err
	}
	for _, c := range convs {
		if c.JobID == "" {
			continue
		}
		if _, err := s.store.GetJob(ctx, c.JobID); !errors.Is(err, store.ErrNotFound) {
			continue // a node job: it ends on its own
		}
		_, _ = s.store.FinishConversationJob(ctx, c.ID, c.JobID, store.ChatMessage{
			Role: "assistant", Error: "interrompido: o servidor reiniciou no meio da resposta", CreatedAt: s.now(),
		}, "", "", store.ActorSystem)
	}
	s.RecoverMissions(ctx)
	return nil
}

// jobWaits wakes the turns waiting on a delegated job.
type jobWaits struct {
	mu sync.Mutex
	m  map[string]chan jobEnd
}

type jobEnd struct {
	job    Job
	result *JobResult
}

func (jw *jobWaits) add(jobID string) <-chan jobEnd {
	jw.mu.Lock()
	defer jw.mu.Unlock()
	if jw.m == nil {
		jw.m = map[string]chan jobEnd{}
	}
	ch := make(chan jobEnd, 1)
	jw.m[jobID] = ch
	return ch
}

func (jw *jobWaits) drop(jobID string) {
	jw.mu.Lock()
	defer jw.mu.Unlock()
	delete(jw.m, jobID)
}

func (jw *jobWaits) done(job Job, result *JobResult) {
	jw.mu.Lock()
	ch := jw.m[job.ID]
	delete(jw.m, job.ID)
	jw.mu.Unlock()
	if ch != nil {
		ch <- jobEnd{job, result}
	}
}
