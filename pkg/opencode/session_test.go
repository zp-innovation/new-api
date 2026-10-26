package opencode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreExpiresAndDeletes(t *testing.T) {
	store := NewSessionStore()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	store.Put("session-1", Session{
		DeviceCode: "device",
		Server:     DefaultServer,
		Mode:       ModeZen,
		Interval:   2,
	})

	session, ok := store.Get("session-1")
	require.True(t, ok)
	assert.Equal(t, "device", session.DeviceCode)
	assert.True(t, store.Delete("session-1"))
	_, ok = store.Get("session-1")
	assert.False(t, ok)
}

func TestSessionStoreDropsExpiredAttempts(t *testing.T) {
	store := NewSessionStore()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	store.Put("session-1", Session{
		DeviceCode: "device",
		ExpiresAt:  now.Add(time.Minute),
	})

	now = now.Add(2 * time.Minute)
	_, ok := store.Get("session-1")
	assert.False(t, ok)
}

func TestSessionStoreCleanupDropsAbandonedAttempts(t *testing.T) {
	store := NewSessionStore()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	store.Put("abandoned", Session{DeviceCode: "device", ExpiresAt: now.Add(time.Minute)})

	now = now.Add(2 * time.Minute)
	store.Put("live", Session{DeviceCode: "device"})

	assert.Equal(t, 1, store.Cleanup(), "Cleanup must drop only the abandoned attempt")
	_, ok := store.Get("live")
	assert.True(t, ok)
}

func TestRandomSessionIDIsOpaqueHex(t *testing.T) {
	id, err := RandomSessionID()
	require.NoError(t, err)
	assert.Len(t, id, 32)
}
