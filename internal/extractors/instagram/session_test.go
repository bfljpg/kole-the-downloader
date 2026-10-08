package instagram

import (
	"errors"
	"testing"
	"time"
)

func TestSessionGuard(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	g := &sessionGuard{now: func() time.Time { return now }}

	for i := 0; i < sessionDailyLimit; i++ {
		if err := g.acquire(); err != nil {
			t.Fatalf("lookup %d: unexpected error: %v", i, err)
		}
	}
	if err := g.acquire(); !errors.Is(err, errSessionLimit) {
		t.Fatalf("expected daily limit, got %v", err)
	}

	now = now.Add(24 * time.Hour)
	if err := g.acquire(); err != nil {
		t.Fatalf("limit should reset on a new day, got %v", err)
	}

	g.trip()
	if err := g.acquire(); !errors.Is(err, errSessionCooldown) {
		t.Fatalf("expected cooldown, got %v", err)
	}
	now = now.Add(sessionCooldown + time.Minute)
	if err := g.acquire(); err != nil {
		t.Fatalf("cooldown should expire, got %v", err)
	}
}
