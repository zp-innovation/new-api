package opencode

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session is one in-flight device authorization. The device code never leaves
// the server; the panel only receives the opaque session id.
type Session struct {
	DeviceCode string
	Server     string
	Mode       string
	Interval   int
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// SessionStore keeps short-lived device authorization attempts.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	now      func() time.Time
}

// NewSessionStore returns an empty store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]Session),
		now:      time.Now,
	}
}

// SetClock replaces the time source; it exists for tests.
func (s *SessionStore) SetClock(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Put records a pending session.
func (s *SessionStore) Put(id string, session Session) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = s.now()
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = session.CreatedAt.Add(LoginTTLSeconds * time.Second)
	}
	s.sessions[id] = session
}

// Get returns a live session and drops an expired one.
func (s *SessionStore) Get(id string) (Session, bool) {
	if s == nil || id == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	if !s.now().Before(session.ExpiresAt) {
		delete(s.sessions, id)
		return Session{}, false
	}
	return session, true
}

// Delete removes a session.
func (s *SessionStore) Delete(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	return true
}

// Cleanup removes expired sessions and returns how many were dropped. Attempts
// an operator abandoned are never read again, so without this the map would grow
// for the lifetime of the process.
func (s *SessionStore) Cleanup() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	removed := 0
	for id, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

// RandomSessionID returns a 128-bit opaque id.
func RandomSessionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
