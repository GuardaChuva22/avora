package control

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// QuotaWindow is one rolling limit of a subscription: how much of it is used
// (0-100) and when it resets (unix seconds).
type QuotaWindow struct {
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    int64   `json:"resets_at,omitempty"`
}

// Quota is what a node knows about one agent subscription's limits. Nodes
// read it from the official CLIs only: Claude Code's rate_limit_event and
// Codex's app-server; Avora never touches the logins themselves.
type Quota struct {
	Engine string `json:"engine"`
	// Account is the extra account the reading is of ("" = the node's own
	// login of Engine).
	Account   string       `json:"account,omitempty"`
	Plan      string       `json:"plan,omitempty"`
	FiveHour  *QuotaWindow `json:"five_hour,omitempty"`
	Weekly    *QuotaWindow `json:"weekly,omitempty"`
	Limited   bool         `json:"limited"`
	CheckedAt time.Time    `json:"checked_at"`
	Error     string       `json:"error,omitempty"`
	// Name is the account's name in Settings; only the server fills it.
	Name string `json:"name,omitempty"`
}

// QuotaStatus is a quota as shown to the admin, with the node it came from.
type QuotaStatus struct {
	Quota
	Node string `json:"node"`
}

// quotaBook keeps the latest quotas each node reported. It lives in memory:
// nodes resend theirs with every heartbeat.
type quotaBook struct {
	mu     sync.Mutex
	byNode map[string][]Quota
}

func (b *quotaBook) set(nodeID string, quotas []Quota) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byNode == nil {
		b.byNode = map[string][]Quota{}
	}
	b.byNode[nodeID] = quotas
}

func (b *quotaBook) get(nodeID string) []Quota {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byNode[nodeID]
}

// quotaLimited reports whether a login hit its limit: a node's own login
// of engine (account "") or one extra account.
func (s *Server) quotaLimited(nodeID, engine string, account ...string) bool {
	acc := ""
	if len(account) > 0 {
		acc = account[0]
	}
	for _, q := range s.quotaBook.get(nodeID) {
		if q.Engine == engine && q.Account == acc && q.Limited {
			return true
		}
	}
	return false
}

// quotaEntry finds the reported Quota reading for engine and account on nodeID.
func (s *Server) quotaEntry(nodeID, engine, account string) (Quota, bool) {
	for _, q := range s.quotaBook.get(nodeID) {
		if q.Engine == engine && q.Account == account {
			return q, true
		}
	}
	return Quota{}, false
}

// formatResetAt turns a unix reset timestamp into "às 18:00" (same day or soon)
// or "em 20/11 às 18:00" in the server's time zone.
func (s *Server) formatResetAt(ts int64) string {
	if ts <= 0 {
		return ""
	}
	loc := s.zone()
	t := time.Unix(ts, 0).In(loc)
	now := s.now().In(loc)
	if (t.Year() == now.Year() && t.YearDay() == now.YearDay()) || (t.After(now) && t.Sub(now) <= 18*time.Hour) {
		return "às " + t.Format("15:04")
	}
	return "em " + t.Format("02/01") + " às " + t.Format("15:04")
}

// quotaResetClause returns "renova às HH:MM" when a reset timestamp is known.
func (s *Server) quotaResetClause(nodeID, engine, account string) string {
	q, ok := s.quotaEntry(nodeID, engine, account)
	if !ok {
		return ""
	}
	var resets int64
	for _, w := range []*QuotaWindow{q.FiveHour, q.Weekly} {
		if w != nil && w.UsedPercent >= 99.5 && w.ResetsAt > 0 && (resets == 0 || w.ResetsAt < resets) {
			resets = w.ResetsAt
		}
	}
	if resets == 0 {
		for _, w := range []*QuotaWindow{q.FiveHour, q.Weekly} {
			if w != nil && w.ResetsAt > 0 && (resets == 0 || w.ResetsAt < resets) {
				resets = w.ResetsAt
			}
		}
	}
	if when := s.formatResetAt(resets); when != "" {
		return "renova " + when
	}
	return ""
}

// quotaLimitReason builds the one-sentence unavailable reason when a login hit its limit.
func (s *Server) quotaLimitReason(nodeID, engine, account string) string {
	if rc := s.quotaResetClause(nodeID, engine, account); rc != "" {
		return "Limite atingido; " + rc + "."
	}
	return "Limite atingido; espere renovar."
}

// cliErrorReason maps a CLI or quota error to a single plain Portuguese sentence.
func cliErrorReason(progName, nodeName, rawErr string) string {
	low := strings.ToLower(strings.TrimSpace(rawErr))
	if low == "" {
		return ""
	}
	where := ""
	if nodeName != "" {
		where = " em " + nodeName
	}
	switch {
	case strings.Contains(low, "not installed") || strings.Contains(low, "não está instalado") ||
		strings.Contains(low, "executable file not found") || strings.Contains(low, "no such file") ||
		strings.Contains(low, "cannot find the file"):
		return "O " + progName + " não está instalado" + where + "."
	case strings.Contains(low, "outdated") || strings.Contains(low, "old version") ||
		strings.Contains(low, "versão") || strings.Contains(low, "unsupported version") ||
		strings.Contains(low, "please update") || strings.Contains(low, "please upgrade") ||
		strings.Contains(low, "update required") || strings.Contains(low, "upgrade required") ||
		strings.Contains(low, "too old"):
		return "A versão do " + progName + where + " está velha; atualize o programa."
	case strings.Contains(low, "invalid api key") || strings.Contains(low, "chave inválida") ||
		strings.Contains(low, "chave foi recusada") || strings.Contains(low, "401") ||
		strings.Contains(low, "unauthorized") || strings.Contains(low, "authentication required") ||
		strings.Contains(low, "not logged"):
		return "Chave inválida ou conta desconectada no " + progName + "."
	}
	return ""
}

// MaxQuotas bounds the readings one node reports.
const MaxQuotas = 64

func validQuotas(quotas []Quota) bool {
	if len(quotas) > MaxQuotas {
		return false
	}
	for _, q := range quotas {
		if !validEngine(q.Engine) || len(q.Plan) > 32 || len(q.Error) > 300 ||
			q.Account != "" && !strings.HasPrefix(q.Account, q.Engine+".") || len(q.Account) > 40 {
			return false
		}
	}
	return true
}

func validEngine(engine string) bool {
	for _, e := range AgentEngines {
		if e == engine {
			return true
		}
	}
	return false
}

// better says whether a should replace b as an engine's shown quota: a
// working reading beats an error, then the most recent one wins.
func better(a, b Quota) bool {
	if (a.Error == "") != (b.Error == "") {
		return a.Error == ""
	}
	return a.CheckedAt.After(b.CheckedAt)
}

// quotas lists the subscriptions' limits: the nodes' own login of each
// engine (the best reading among the online nodes) and then every extra
// account, under the name the owner gave it.
func (s *Server) quotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	providers, err := s.store.ListProviders(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	accounts := map[string]string{}
	// Antigravity has one login per PC: the node's reading is the account
	// added for that node in Settings.
	agyOn := map[string]string{}
	for _, p := range providers {
		if p.Kind == "antigravity" && isAgyPCAccount(p) {
			agyOn[p.NodeID] = p.ID
		}
		if p.Kind == "claude" || p.Kind == "codex" || p.Kind == "antigravity" {
			accounts[p.ID] = p.Name
		}
	}
	best := map[string]QuotaStatus{}
	for _, n := range nodes {
		if !s.online(n) {
			continue
		}
		for _, q := range s.quotaBook.get(n.ID) {
			key := q.Engine
			if q.Engine == "antigravity" && q.Account == "" && agyOn[n.ID] != "" {
				q.Account = agyOn[n.ID]
			}
			if q.Account != "" {
				name, ok := accounts[q.Account]
				if !ok {
					continue // removed in Settings
				}
				key = q.Account
				q.Name = name
				if q.Engine == "antigravity" && (name == "" || name == "Principal" || name == "Antigravity") {
					q.Name = "Antigravity"
				}
			}
			if cur, ok := best[key]; !ok || better(q, cur.Quota) {
				best[key] = QuotaStatus{Quota: q, Node: n.Name}
			}
		}
	}
	out := make([]QuotaStatus, 0, len(best))
	for _, q := range best {
		out = append(out, q)
	}
	order := map[string]int{}
	for i, e := range AgentEngines {
		order[e] = i
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Account == "") != (b.Account == "") {
			return a.Account == ""
		}
		if a.Engine != b.Engine {
			return order[a.Engine] < order[b.Engine]
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	writeJSON(w, http.StatusOK, out)
}
