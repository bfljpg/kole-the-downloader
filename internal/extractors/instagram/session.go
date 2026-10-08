package instagram

import (
	"errors"
	"sync"
	"time"
)

const (
	// logged in lookups allowed per utc day, so a burst of links
	// can never turn into a burst of requests from the account.
	sessionDailyLimit = 40
	// how long the session stays unused after instagram pushed back.
	sessionCooldown = 12 * time.Hour
)

var (
	errSessionCooldown = errors.New("session is paused after a failed request")
	errSessionLimit    = errors.New("daily session limit reached")
)

// sessionGuard decides if the logged in session may be used right now.
// state is kept in memory only, a restart resets it.
type sessionGuard struct {
	mu           sync.Mutex
	now          func() time.Time
	day          string
	used         int
	blockedUntil time.Time
}

var igSession = &sessionGuard{now: time.Now}

// acquire reserves one session lookup.
func (g *sessionGuard) acquire() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	if now.Before(g.blockedUntil) {
		return errSessionCooldown
	}
	if day := now.UTC().Format("2006-01-02"); day != g.day {
		g.day, g.used = day, 0
	}
	if g.used >= sessionDailyLimit {
		return errSessionLimit
	}
	g.used++
	return nil
}

// trip pauses the session, call it when instagram rejected a logged in
// request: retrying only makes a suspicious account look worse.
func (g *sessionGuard) trip() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blockedUntil = g.now().Add(sessionCooldown)
}
