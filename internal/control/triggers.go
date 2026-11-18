package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Event triggers: a routine that runs when something happens instead of at
// a time. A connector trigger checks a read tool of a Plugin every few
// minutes (Gmail search, ClickUp tasks) and fires with the items it had not
// seen; the first check only learns what is already there. A node trigger
// fires when the PC or the VPS goes online or offline. The event goes to
// the agent with the routine's instruction; the agent decides what to do.

const (
	TriggerTick         = time.Minute
	defaultTriggerEvery = 5
	minTriggerEvery     = 2
	maxTriggerEvery     = 24 * 60
	maxTriggerSeen      = 300
	maxTriggerItems     = 15
)

// triggerTools are the connector tools a trigger may watch.
var triggerTools = map[string]struct {
	Label    string
	Defaults map[string]any
	Keys     []string // the arguments the owner may set
}{
	"gmail_buscar":    {"E-mail novo", map[string]any{"busca": "in:inbox", "quantidade": 20}, []string{"busca", "conta"}},
	"clickup_tarefas": {"Tarefa nova no ClickUp", map[string]any{"quantidade": 40}, []string{"lista_id", "busca", "conta"}},
}

// triggerItem finds the id at the start of an item line: "[id] ...".
var triggerItem = regexp.MustCompile(`^\[([^\]\s]{1,120})\]`)

type triggerState struct {
	mu      sync.Mutex
	checked map[string]time.Time // routine id -> last connector check
	online  map[string]bool      // routine id -> node state last seen
}

// cleanTrigger validates a trigger from the app or an agent, keeping what
// base already saw when it watches the same thing.
func (s *Server) cleanTrigger(ctx context.Context, tr store.Trigger, base *store.Trigger) (*store.Trigger, error) {
	out := store.Trigger{Kind: tr.Kind}
	switch tr.Kind {
	case "conector":
		spec, ok := triggerTools[tr.Tool]
		if !ok {
			return nil, errors.New("o gatilho de conector vigia gmail_buscar ou clickup_tarefas")
		}
		out.Tool, out.Args = tr.Tool, map[string]any{}
		for k, v := range spec.Defaults {
			out.Args[k] = v
		}
		for _, k := range spec.Keys {
			if v, ok := tr.Args[k].(string); ok && strings.TrimSpace(v) != "" {
				out.Args[k] = truncate(strings.TrimSpace(v), 300)
			}
		}
		out.Every = tr.Every
		if out.Every == 0 {
			out.Every = defaultTriggerEvery
		}
		if out.Every < minTriggerEvery || out.Every > maxTriggerEvery {
			return nil, fmt.Errorf("o gatilho confere entre %d e %d minutos", minTriggerEvery, maxTriggerEvery)
		}
	case "no":
		if tr.Event != "online" && tr.Event != "offline" {
			return nil, errors.New(`o evento do nó é "online" ou "offline"`)
		}
		n, err := s.nodeByID(ctx, tr.Node)
		if err != nil || n.RevokedAt != nil {
			return nil, errors.New("nó desconhecido")
		}
		out.Node, out.NodeName, out.Event = n.ID, n.Name, tr.Event
	default:
		return nil, errors.New(`o gatilho é "conector" ou "no"`)
	}
	if base != nil && base.Kind == out.Kind && base.Tool == out.Tool && fmt.Sprint(base.Args) == fmt.Sprint(out.Args) {
		out.Seen = base.Seen
	}
	return &out, nil
}

// triggerArgs is a trigger as an agent describes it (criar_rotina).
type triggerArgs struct {
	Tipo         string `json:"tipo"`
	Busca        string `json:"busca"`
	ListaID      string `json:"lista_id"`
	No           string `json:"no"`
	Evento       string `json:"evento"`
	ACadaMinutos int    `json:"a_cada_minutos"`
}

func (s *Server) triggerFromArgs(ctx context.Context, g *triggerArgs) (*store.Trigger, error) {
	if g == nil || strings.TrimSpace(g.Tipo) == "" {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(g.Tipo)) {
	case "email", "gmail":
		return &store.Trigger{Kind: "conector", Tool: "gmail_buscar", Every: g.ACadaMinutos,
			Args: map[string]any{"busca": orDefault(strings.TrimSpace(g.Busca), "in:inbox")}}, nil
	case "clickup":
		return &store.Trigger{Kind: "conector", Tool: "clickup_tarefas", Every: g.ACadaMinutos,
			Args: map[string]any{"busca": g.Busca, "lista_id": g.ListaID}}, nil
	case "no":
		want := scheduler.Windows
		if strings.EqualFold(g.No, "vps") {
			want = scheduler.VPS
		}
		nodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if n.Kind == want && n.RevokedAt == nil {
				return &store.Trigger{Kind: "no", Node: n.ID, Event: orDefault(g.Evento, "offline")}, nil
			}
		}
		return nil, fmt.Errorf("não há nó %s pareado", strings.ToUpper(orDefault(g.No, "pc")))
	}
	return nil, fmt.Errorf("gatilho %q não existe (email, clickup, no)", g.Tipo)
}

// triggerText says in words what fires a routine.
func triggerText(tr *store.Trigger) string {
	switch tr.Kind {
	case "conector":
		label := triggerTools[tr.Tool].Label
		var what []string
		for _, k := range []string{"busca", "lista_id"} {
			if v, ok := tr.Args[k].(string); ok && v != "" {
				what = append(what, v)
			}
		}
		if len(what) > 0 {
			label += " «" + strings.Join(what, " · ") + "»"
		}
		return fmt.Sprintf("quando chegar: %s (confere a cada %d min)", label, tr.Every)
	case "no":
		return "quando " + orDefault(tr.NodeName, "o nó") + " ficar " + tr.Event
	}
	return "gatilho"
}

// whenOf says when a routine runs: its schedule or its trigger.
func whenOf(rt store.Routine) string {
	if rt.Trigger != nil {
		return triggerText(rt.Trigger)
	}
	return scheduleText(rt.Schedule)
}

// RunTriggers checks the event triggers until ctx ends.
func (s *Server) RunTriggers(ctx context.Context) {
	ticker := time.NewTicker(TriggerTick)
	defer ticker.Stop()
	for {
		s.TickTriggers(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// TickTriggers checks every enabled routine with a trigger once.
func (s *Server) TickTriggers(ctx context.Context) {
	list, err := s.store.ListRoutines(ctx, "")
	if err != nil {
		return
	}
	for _, rt := range list {
		if !rt.Enabled || rt.Trigger == nil {
			continue
		}
		switch rt.Trigger.Kind {
		case "no":
			s.checkNodeTrigger(ctx, rt)
		case "conector":
			s.triggers.mu.Lock()
			last := s.triggers.checked[rt.ID]
			due := last.IsZero() || s.now().Sub(last) >= time.Duration(rt.Trigger.Every)*time.Minute
			if due {
				if s.triggers.checked == nil {
					s.triggers.checked = map[string]time.Time{}
				}
				s.triggers.checked[rt.ID] = s.now()
			}
			s.triggers.mu.Unlock()
			if due {
				s.checkConnectorTrigger(ctx, rt)
			}
		}
	}
}

func (s *Server) checkNodeTrigger(ctx context.Context, rt store.Routine) {
	n, err := s.nodeByID(ctx, rt.Trigger.Node)
	if err != nil {
		return
	}
	on := s.online(n)
	s.triggers.mu.Lock()
	prev, known := s.triggers.online[rt.ID]
	if s.triggers.online == nil {
		s.triggers.online = map[string]bool{}
	}
	s.triggers.online[rt.ID] = on
	s.triggers.mu.Unlock()
	if !known || prev == on || (rt.Trigger.Event == "online") != on {
		return
	}
	where := "o PC"
	if n.Kind == scheduler.VPS {
		where = "a VPS"
	}
	state := map[bool]string{true: "ficou online", false: "ficou offline (parou de responder)"}[on]
	s.startRoutine(ctx, rt, false, fmt.Sprintf("Gatilho: %s (%s) %s às %s.", where, n.Name, state, s.now().In(s.zone()).Format("15:04")))
}

// pollTrigger runs the trigger's connector tool as the routine's agent.
func (s *Server) pollTrigger(ctx context.Context, rt store.Routine) (string, error) {
	return s.pollConnector(ctx, rt.AgentID, rt.Trigger.Tool, rt.Trigger.Args)
}

// pollConnector runs a Plugin's read tool as the agent, without the model:
// triggers and goal waits look at what came back.
func (s *Server) pollConnector(ctx context.Context, agentID, tool string, argv map[string]any) (string, error) {
	if s.connectors == nil {
		return "", errors.New("conectores desligados")
	}
	con, _, found := s.connectors.Tool(tool)
	if !found {
		return "", errors.New("o conector não existe mais")
	}
	raw, _ := json.Marshal(argv)
	args, err := connect.ParseArgs(string(raw))
	if err != nil {
		return "", err
	}
	want := args.Str("conta")
	delete(args, "conta")
	conn, _, err := s.pickAccount(ctx, con.Info(), agentID, want)
	if err != nil {
		return "", err
	}
	return s.useConnection(ctx, con, conn, func(ctx context.Context, env *connect.Env) (string, error) {
		env.Files = agentFiles{s, agentID}
		return con.Call(ctx, env, tool, args)
	}, agentID, tool)
}

func (s *Server) checkConnectorTrigger(ctx context.Context, rt store.Routine) {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	out, err := s.pollTrigger(cctx, rt)
	cancel()
	tr := rt.Trigger
	if err != nil {
		if msg := truncate(err.Error(), 300); tr.LastError != msg {
			tr.LastError = msg
			_, _ = s.store.SaveRoutine(ctx, rt, store.ActorSystem, s.now())
		}
		if ctx.Err() == nil {
			log.Printf("routine %s: trigger check failed: %v", rt.ID, err)
		}
		return
	}
	seen := map[string]bool{}
	for _, id := range tr.Seen {
		seen[id] = true
	}
	var ids, fresh, lines []string
	added := false // the last item went into lines
	for _, line := range strings.Split(out, "\n") {
		m := triggerItem.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			if added && strings.HasPrefix(line, "   ") {
				lines[len(lines)-1] += "\n" + line // a snippet under the item
			}
			continue
		}
		ids = append(ids, m[1])
		added = false
		if !seen[m[1]] {
			fresh = append(fresh, m[1])
			if len(lines) < maxTriggerItems {
				lines = append(lines, strings.TrimSpace(line))
				added = true
			}
		}
	}
	first := tr.Seen == nil
	changed := tr.LastError != "" || first && len(ids) > 0
	tr.LastError = ""
	if first {
		tr.Seen = []string{} // the first check only learns what is there
		fresh = nil
	}
	if len(fresh) > 0 {
		label := triggerTools[tr.Tool].Label
		event := fmt.Sprintf("Gatilho: %s (%d novo(s)):\n%s", label, len(fresh), strings.Join(lines, "\n"))
		if len(fresh) > len(lines) {
			event += fmt.Sprintf("\n… e mais %d", len(fresh)-len(lines))
		}
		if !s.startRoutine(ctx, rt, false, event) {
			return // still running: these items fire on the next check
		}
		cur, err := s.store.GetRoutine(ctx, rt.ID)
		if err != nil || cur.Trigger == nil {
			return
		}
		rt, tr = cur, cur.Trigger
		changed = true
	}
	if first || len(fresh) > 0 {
		tr.Seen = append(append([]string{}, ids...), tr.Seen...)
		uniq := map[string]bool{}
		kept := []string{}
		for _, id := range tr.Seen {
			if !uniq[id] && len(kept) < maxTriggerSeen {
				uniq[id] = true
				kept = append(kept, id)
			}
		}
		tr.Seen = kept
	}
	if changed {
		_, _ = s.store.SaveRoutine(ctx, rt, store.ActorSystem, s.now())
	}
}
