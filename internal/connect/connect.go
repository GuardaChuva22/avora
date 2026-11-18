// Package connect holds the connectors (Plugins): typed, tested clients for
// the owner's apps. A connector says which tools it gives the agents and how
// risky each one is; the server runs them with a credential the vault opens
// only for the call, so tokens never reach the model's context.
package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Risk is how much a tool call can do. Reading and writing in the owner's
// own space (a draft, a task, a doc) run freely; anything others see or that
// cannot be undone waits for the owner.
type Risk string

const (
	Read   Risk = "ler"
	Write  Risk = "escrever"
	Send   Risk = "enviar"
	Delete Risk = "apagar"
	Spend  Risk = "gastar" // payments, refunds, purchases
)

// Locked says whether a call with this risk asks the owner first.
func (r Risk) Locked() bool { return r == Send || r == Delete || r == Spend }

// Tool is one thing a connector lets an agent do.
type Tool struct {
	Name        string
	Description string
	Params      string // JSON schema of the arguments
	Risk        Risk
	// RiskOf, when set, decides the risk from the arguments (an event with
	// guests sends invitations).
	RiskOf func(Args) Risk
	// Summary says what a call does, in words, for cards and approvals.
	Summary func(Args) string
}

func (t Tool) RiskFor(a Args) Risk {
	if t.RiskOf != nil {
		return t.RiskOf(a)
	}
	return t.Risk
}

func (t Tool) SummaryFor(a Args) string {
	if t.Summary != nil {
		return t.Summary(a)
	}
	return t.Name
}

// Info describes a connector for the Plugins page.
type Info struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Auth        string   `json:"auth"` // "token" or "oauth"
	// Login: a token app that also connects with its own browser login
	// (oauth/start), besides the token box.
	Login bool `json:"login,omitempty"`
	Help        string   `json:"help"` // how to get the token or set it up
	HelpURL     string   `json:"help_url,omitempty"`
	Apps        []string `json:"apps,omitempty"`
	// Ready is false when the server lacks something to connect it (an
	// OAuth client); Help says what.
	Ready bool `json:"ready"`
	// Category groups the catalog on the Plugins page.
	Category string `json:"category,omitempty"`
	// MCP apps: the server address, and whether the owner added it.
	URL    string `json:"url,omitempty"`
	Custom bool   `json:"custom,omitempty"`
	// Fields replaces the single token box when set.
	Fields []Field `json:"fields,omitempty"`
	// OAuthPort is the fixed loopback port the app must listen on, for
	// providers that only accept the exact redirect URI they registered
	// (0: any free port).
	OAuthPort int `json:"oauth_port,omitempty"`
	// Via is the app whose accounts this one runs on: it shows as connected
	// when that app is, and has nothing of its own to connect.
	Via string `json:"via,omitempty"`
}

// Backed maps the apps that run on another app's accounts to that app.
var Backed = map[string]string{"shopee": "apify", "aliexpress": "apify"}

// Field is one box of a token form, for apps that need more than one
// value; the values go in Cred.Extra by key.
type Field struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Secret bool   `json:"secret,omitempty"`
	// File: the value is a file the owner picks (sent as base64).
	File bool `json:"file,omitempty"`
	// Optional: the box may stay empty.
	Optional bool `json:"optional,omitempty"`
}

// Cred is what the vault keeps for a connection.
type Cred struct {
	Token   string            `json:"token"`
	Refresh string            `json:"refresh,omitempty"`
	Expiry  time.Time         `json:"expiry,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// Files saves what a tool produces (audio, exports) in the agent's folder
// and returns the path the agent sees.
type Files interface {
	Save(name string, data []byte) (string, error)
}

// Env is what one call gets.
type Env struct {
	// Conn is the connection (account) the call runs as; empty while a new
	// credential is being checked.
	Conn    string
	Cred    *Cred
	Changed bool // the connector refreshed Cred; the server seals it again
	Files   Files
	Loc     *time.Location
	Now     func() time.Time
	// Incoming reads the money that came into the owner's banks (Pluggy),
	// for the connectors that confirm payments; nil when there is none.
	Incoming func(ctx context.Context, since time.Time) ([]Incoming, error)
}

func (e *Env) zone() *time.Location {
	if e.Loc != nil {
		return e.Loc
	}
	return time.UTC
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

type Connector interface {
	Info() Info
	Tools() []Tool
	// Check proves the credential works and names the account.
	Check(ctx context.Context, env *Env) (account string, err error)
	Call(ctx context.Context, env *Env, tool string, a Args) (string, error)
}

// Usage is how much of an account's plan is used: credits, characters...
type Usage struct {
	Plan     string `json:"plan,omitempty"`
	Used     int64  `json:"used"`
	Limit    int64  `json:"limit"`
	Unit     string `json:"unit"`
	ResetsAt int64  `json:"resets_at,omitempty"` // unix seconds
}

// UsageReader is a connector that can tell how much of each account's
// plan is left, for the Plugins page.
type UsageReader interface {
	Usage(ctx context.Context, env *Env) (Usage, error)
}

// AuthError means the credential no longer works: the connection needs the
// owner to reconnect it.
type AuthError struct{ Msg string }

func (e *AuthError) Error() string { return e.Msg }

func IsAuth(err error) bool {
	var a *AuthError
	return errors.As(err, &a)
}

// CreditError means the account has no credit left for the call: a pooled
// connector's call moves to the next account.
type CreditError struct{ Msg string }

func (e *CreditError) Error() string { return e.Msg }

func IsCredit(err error) bool {
	var c *CreditError
	return errors.As(err, &c)
}

// Pooled connectors spread calls over the owner's accounts: the server
// runs each call on the account with the most credit left and moves to the
// next when one runs out.
type Pooled interface{ Pooled() }

// Routed connectors know, from a call's arguments, which account holds
// what it refers to (a run started on one account); the answer is the
// connection id, or "".
type Routed interface {
	RouteOf(tool string, a Args) string
}

var errReconnect = &AuthError{Msg: "a conexão perdeu o acesso (token inválido ou revogado); reconecte em Plugins"}

// Args are a tool call's arguments.
type Args map[string]any

func ParseArgs(raw string) (Args, error) {
	a := Args{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return a, nil
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, fmt.Errorf("os argumentos não são um JSON válido: %v", err)
	}
	return a, nil
}

func (a Args) Str(k string) string {
	switch v := a[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func (a Args) Int(k string, def, lo, hi int) int {
	n := def
	switch v := a[k].(type) {
	case float64:
		n = int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = i
		}
	}
	return max(lo, min(hi, n))
}

func (a Args) Bool(k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "sim"
	}
	return false
}

func (a Args) List(k string) []string {
	out := []string{}
	switch v := a[k].(type) {
	case []any:
		for _, x := range v {
			if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Need returns the argument or an error naming it.
func (a Args) Need(k string) (string, error) {
	if v := a.Str(k); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("falta o argumento %s", k)
}

func cut(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// partRunes is how much text one result carries; longer texts come in parts.
const partRunes = 6500

// part returns part n (1-based) of a long text and how many there are.
func part(text string, n int) (string, int) {
	r := []rune(text)
	total := max(1, (len(r)+partRunes-1)/partRunes)
	n = max(1, min(n, total))
	return string(r[(n-1)*partRunes : min(n*partRunes, len(r))]), total
}

// withPart adds "parte n de N" to a long text's header.
func withPart(head, text string, n int) string {
	body, total := part(text, n)
	if total > 1 {
		head += fmt.Sprintf(" (parte %d de %d; peça parte=%d para continuar)", max(1, min(n, total)), total, min(max(1, n)+1, total))
	}
	return head + "\n" + body
}

// parseWhen reads "2026-11-12", "2026-11-12 15:00" or "12/11/2026 15:00" in loc.
func parseWhen(s string, loc *time.Location) (t time.Time, allDay bool, err error) {
	s = strings.TrimSpace(strings.Replace(s, "T", " ", 1))
	for _, f := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05", "02/01/2006 15:04"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, false, nil
		}
	}
	for _, f := range []string{"2006-01-02", "02/01/2006"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, true, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, strings.Replace(s, " ", "T", 1)); err == nil {
		return t, false, nil
	}
	return time.Time{}, false, fmt.Errorf("data inválida %q (use AAAA-MM-DD ou AAAA-MM-DD HH:MM)", s)
}

// Registry is the catalog of connectors, in display order. MCP apps the
// owner adds join it at run time.
type Registry struct {
	mu   sync.RWMutex
	list []Connector
}

func NewRegistry(cs ...Connector) *Registry {
	return &Registry{list: append([]Connector{}, cs...)}
}

func (r *Registry) All() []Connector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Connector{}, r.list...)
}

func (r *Registry) Get(kind string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.list {
		if c.Info().Kind == kind {
			return c, true
		}
	}
	return nil, false
}

// Add puts a connector in the catalog; false if its kind is taken.
func (r *Registry) Add(c Connector) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.list {
		if x.Info().Kind == c.Info().Kind {
			return false
		}
	}
	r.list = append(r.list, c)
	return true
}

func (r *Registry) Remove(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.list {
		if x.Info().Kind == kind {
			r.list = append(r.list[:i], r.list[i+1:]...)
			return
		}
	}
}

// Tool finds the connector and definition of a tool name.
func (r *Registry) Tool(name string) (Connector, Tool, bool) {
	for _, c := range r.All() {
		for _, t := range c.Tools() {
			if t.Name == name {
				return c, t, true
			}
		}
	}
	return nil, Tool{}, false
}
