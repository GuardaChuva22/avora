package control

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Plugin usage: apps that bill per account (ElevenLabs credits) tell the
// Plugins page how much each connected account has left. Readings are
// cached for a few minutes so opening the page does not hit the API every
// time. The page's list never waits for an app's API: it gets the last
// reading (or "pending") at once and a stale or missing one is refreshed in
// the background, ready for the page's next request.

const (
	usageTTL     = 3 * time.Minute
	usageTimeout = 8 * time.Second
)

// ConnectionUsage is one account's reading, or why it failed.
type ConnectionUsage struct {
	connect.Usage
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	// Pending: no reading yet; one is being taken in the background.
	Pending bool `json:"pending,omitempty"`
}

type usageCache struct {
	mu   sync.Mutex
	m    map[string]ConnectionUsage // by connection id
	busy map[string]bool            // background readings in flight
}

// peek returns the last reading, stale or not, and whether it is fresh.
func (u *usageCache) peek(id string, now time.Time) (v ConnectionUsage, have, fresh bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	v, have = u.m[id]
	return v, have, have && now.Sub(v.CheckedAt) < usageTTL
}

// claim marks a background reading of id as started; false if one already is.
func (u *usageCache) claim(id string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy[id] {
		return false
	}
	if u.busy == nil {
		u.busy = map[string]bool{}
	}
	u.busy[id] = true
	return true
}

func (u *usageCache) release(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.busy, id)
}

func (u *usageCache) get(id string, now time.Time) (ConnectionUsage, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	v, ok := u.m[id]
	return v, ok && now.Sub(v.CheckedAt) < usageTTL
}

func (u *usageCache) set(id string, v ConnectionUsage) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.m == nil {
		u.m = map[string]ConnectionUsage{}
	}
	u.m[id] = v
}

// spent marks an account as out of credit until its next reading.
func (u *usageCache) spent(id string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.m == nil {
		u.m = map[string]ConnectionUsage{}
	}
	v := u.m[id]
	if v.Limit <= 0 {
		v.Limit = 1
	}
	v.Used, v.Error, v.Pending, v.CheckedAt = v.Limit, "", false, now
	u.m[id] = v
}

// poolOrder is the order a pooled app's accounts are tried in: the most
// credit left first, accounts not read yet next, the spent ones last.
func (s *Server) poolOrder(ctx context.Context, con connect.Connector, agentID string) []store.Connection {
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return nil
	}
	_, by := accountsOf(conns)
	usable := usableOnly(by[con.Info().Kind], agentID)
	// Readings older than a few minutes are taken again: the choice is worth
	// one quick call per account.
	readings := s.connectionsUsage(ctx, con, usable, true)
	left := func(c store.Connection) (float64, int) {
		v, ok := readings[c.ID]
		if !ok || v.Pending || v.Error != "" || v.Limit <= 0 {
			return 0, 1
		}
		if v.Used >= v.Limit {
			return 0, 2
		}
		return float64(v.Limit - v.Used), 0
	}
	sort.SliceStable(usable, func(i, j int) bool {
		li, gi := left(usable[i])
		lj, gj := left(usable[j])
		if gi != gj {
			return gi < gj
		}
		return li > lj
	})
	return usable
}

func (u *usageCache) drop(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.m, id)
}

// connectionsUsage reads the usage of every working account of an app that
// reports one, in parallel; nil when the app reports none. With wait false
// it never blocks: cached readings come back as they are and the stale or
// missing ones are refreshed in the background.
func (s *Server) connectionsUsage(ctx context.Context, con connect.Connector, conns []store.Connection, wait bool) map[string]ConnectionUsage {
	reader, ok := con.(connect.UsageReader)
	if !ok || len(conns) == 0 {
		return nil
	}
	out := map[string]ConnectionUsage{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range conns {
		if c.Status != "ok" {
			continue
		}
		v, have, fresh := s.usage.peek(c.ID, s.now())
		if fresh {
			out[c.ID] = v
			continue
		}
		if !wait {
			if have {
				out[c.ID] = v
			} else {
				out[c.ID] = ConnectionUsage{Pending: true}
			}
			if s.usage.claim(c.ID) {
				go func(c store.Connection) {
					defer s.usage.release(c.ID)
					s.usage.set(c.ID, s.readUsage(context.Background(), reader, c))
				}(c)
			}
			continue
		}
		wg.Add(1)
		go func(c store.Connection) {
			defer wg.Done()
			v := s.readUsage(ctx, reader, c)
			s.usage.set(c.ID, v)
			mu.Lock()
			out[c.ID] = v
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	return out
}

// readUsage asks the app for one account's usage.
func (s *Server) readUsage(ctx context.Context, reader connect.UsageReader, c store.Connection) ConnectionUsage {
	v := ConnectionUsage{CheckedAt: s.now()}
	cred, err := s.openCred(c)
	if err == nil {
		uctx, cancel := context.WithTimeout(ctx, usageTimeout)
		v.Usage, err = reader.Usage(uctx, &connect.Env{Conn: c.ID, Cred: &cred, Loc: s.zone(), Now: s.now})
		cancel()
	}
	if err != nil {
		v.Error = "não consegui ler o saldo"
		if connect.IsAuth(err) {
			v.Error = "a chave não tem acesso ao plano (permissão User)"
		}
	}
	return v
}
