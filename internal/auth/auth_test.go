package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockDB implements SessionDB for testing.
type mockDB struct {
	sessions map[string]*SessionRow
}

func newMockDB() *mockDB {
	return &mockDB{sessions: make(map[string]*SessionRow)}
}

func (m *mockDB) SaveSession(sess *SessionRow) error {
	m.sessions[sess.Token] = sess
	return nil
}

func (m *mockDB) GetSession(token string) (*SessionRow, error) {
	sess, ok := m.sessions[token]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return sess, nil
}

func (m *mockDB) DeleteSession(token string) error {
	delete(m.sessions, token)
	return nil
}

func (m *mockDB) CleanupExpiredSessions() error {
	now := time.Now().UTC()
	for k, v := range m.sessions {
		if now.After(v.ExpiresAt) {
			delete(m.sessions, k)
		}
	}
	return nil
}

func (m *mockDB) UpdateSessionExpiry(token string, expiresAt time.Time) error {
	sess, ok := m.sessions[token]
	if !ok {
		return fmt.Errorf("not found")
	}
	sess.ExpiresAt = expiresAt
	return nil
}

func TestLoginAndValidate(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Wrong credentials
	if _, ok := ss.Login("admin", "wrong"); ok {
		t.Fatal("expected login to fail with wrong password")
	}

	// Correct credentials
	token, ok := ss.Login("admin", "pass123")
	if !ok {
		t.Fatal("expected login to succeed")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate
	sess, ok := ss.Validate(token)
	if !ok {
		t.Fatal("expected session to be valid")
	}
	if sess.Username != "admin" || sess.Role != "admin" {
		t.Fatalf("unexpected session: %+v", sess)
	}
}

func TestExpiredSessionReturnsUnauthorized(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Create a session that's already expired
	token := "expired-token"
	db.SaveSession(&SessionRow{
		Token:     token,
		Username:  "admin",
		Role:      "admin",
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
	})

	sess, ok := ss.Validate(token)
	if ok || sess != nil {
		t.Fatal("expected expired session to be invalid")
	}

	// Session should be deleted from DB
	if _, exists := db.sessions[token]; exists {
		t.Fatal("expected expired session to be deleted from DB")
	}
}

func TestRenewIfNeeded_NotNeeded(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Session with plenty of time remaining (23 hours > 12 hours)
	token := "fresh-token"
	expiry := time.Now().UTC().Add(23 * time.Hour)
	db.SaveSession(&SessionRow{
		Token:     token,
		Username:  "admin",
		Role:      "admin",
		ExpiresAt: expiry,
	})

	sess, ok := ss.Validate(token)
	if !ok {
		t.Fatal("expected valid session")
	}

	renewed := ss.RenewIfNeeded(token, sess)
	if renewed {
		t.Fatal("expected no renewal when > 50% time remains")
	}

	// Expiry should be unchanged
	row, _ := db.GetSession(token)
	if !row.ExpiresAt.Equal(expiry) {
		t.Fatal("expiry should not have changed")
	}
}

func TestRenewIfNeeded_Needed(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Session with only 6 hours remaining (< 12 hours = 50% of 24h)
	token := "old-token"
	oldExpiry := time.Now().UTC().Add(6 * time.Hour)
	db.SaveSession(&SessionRow{
		Token:     token,
		Username:  "admin",
		Role:      "admin",
		ExpiresAt: oldExpiry,
	})

	sess, ok := ss.Validate(token)
	if !ok {
		t.Fatal("expected valid session")
	}

	renewed := ss.RenewIfNeeded(token, sess)
	if !renewed {
		t.Fatal("expected renewal when < 50% time remains")
	}

	// New expiry should be ~24 hours from now
	row, _ := db.GetSession(token)
	expectedMin := time.Now().UTC().Add(23 * time.Hour)
	if row.ExpiresAt.Before(expectedMin) {
		t.Fatalf("new expiry too soon: %v", row.ExpiresAt)
	}
}

func TestMiddlewareRenewsCookie(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Session with only 2 hours remaining — should trigger renewal
	token := "renew-me"
	db.SaveSession(&SessionRow{
		Token:     token,
		Username:  "admin",
		Role:      "admin",
		ExpiresAt: time.Now().UTC().Add(2 * time.Hour),
	})

	handler := ss.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Check that a Set-Cookie header was sent (renewal)
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == cookieName && c.Value == token {
			found = true
			if c.MaxAge != int(sessionMaxAge.Seconds()) {
				t.Fatalf("expected MaxAge=%d, got %d", int(sessionMaxAge.Seconds()), c.MaxAge)
			}
		}
	}
	if !found {
		t.Fatal("expected session cookie to be refreshed")
	}

	// DB expiry should also be extended
	row, _ := db.GetSession(token)
	if time.Until(row.ExpiresAt) < 23*time.Hour {
		t.Fatal("DB session expiry not extended")
	}
}

func TestMiddlewareNoRenewalWhenFresh(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	// Session with 20 hours remaining — should NOT trigger renewal
	token := "fresh-session"
	db.SaveSession(&SessionRow{
		Token:     token,
		Username:  "admin",
		Role:      "admin",
		ExpiresAt: time.Now().UTC().Add(20 * time.Hour),
	})

	handler := ss.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// No Set-Cookie should be sent
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == cookieName {
			t.Fatal("should NOT refresh cookie when session is still fresh")
		}
	}
}

func TestMiddlewareRejects401(t *testing.T) {
	db := newMockDB()
	ss := NewSessionStore("admin", "pass123", db)

	handler := ss.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	// No cookie
	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Invalid token
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
