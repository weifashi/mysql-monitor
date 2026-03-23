package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	cookieName    = "session_token"
	sessionMaxAge = 24 * time.Hour
)

type Session struct {
	Username    string
	UserID      int64
	GitHubLogin string
	Role        string
	AvatarURL   string
	ExpiresAt   time.Time
}

type GitHubConfig struct {
	ClientID     string
	ClientSecret string
}

// SessionDB is the persistence interface for sessions.
type SessionDB interface {
	SaveSession(sess *SessionRow) error
	GetSession(token string) (*SessionRow, error)
	DeleteSession(token string) error
	CleanupExpiredSessions() error
	UpdateSessionExpiry(token string, expiresAt time.Time) error
}

// SessionRow mirrors store.SessionRow to avoid circular imports.
type SessionRow struct {
	Token       string
	Username    string
	UserID      int64
	GitHubLogin string
	Role        string
	AvatarURL   string
	ExpiresAt   time.Time
}

type SessionStore struct {
	db            SessionDB
	adminUser     string
	adminPassword string
	GitHub        GitHubConfig
}

func NewSessionStore(adminUser, adminPassword string, db SessionDB) *SessionStore {
	return &SessionStore{
		db:            db,
		adminUser:     adminUser,
		adminPassword: adminPassword,
	}
}

// Login validates admin credentials (password login).
func (ss *SessionStore) Login(user, pass string) (string, bool) {
	if user != ss.adminUser || pass != ss.adminPassword {
		return "", false
	}
	token := generateToken()
	ss.db.SaveSession(&SessionRow{
		Token:     token,
		Username:  user,
		Role:      "admin",
		ExpiresAt: time.Now().UTC().Add(sessionMaxAge),
	})
	return token, true
}

// LoginGitHub creates a session for a GitHub-authenticated user.
func (ss *SessionStore) LoginGitHub(userID int64, username, githubLogin, avatarURL, role string) string {
	token := generateToken()
	ss.db.SaveSession(&SessionRow{
		Token:       token,
		Username:    username,
		UserID:      userID,
		GitHubLogin: githubLogin,
		Role:        role,
		AvatarURL:   avatarURL,
		ExpiresAt:   time.Now().UTC().Add(sessionMaxAge),
	})
	return token
}

func (ss *SessionStore) Validate(token string) (*Session, bool) {
	row, err := ss.db.GetSession(token)
	if err != nil {
		return nil, false
	}
	if time.Now().UTC().After(row.ExpiresAt) {
		ss.db.DeleteSession(token)
		return nil, false
	}
	return &Session{
		Username:    row.Username,
		UserID:      row.UserID,
		GitHubLogin: row.GitHubLogin,
		Role:        row.Role,
		AvatarURL:   row.AvatarURL,
		ExpiresAt:   row.ExpiresAt,
	}, true
}

// RenewIfNeeded extends the session if less than half the max age remains.
// Returns true if the session was renewed.
func (ss *SessionStore) RenewIfNeeded(token string, sess *Session) bool {
	remaining := time.Until(sess.ExpiresAt)
	if remaining > sessionMaxAge/2 {
		return false
	}
	newExpiry := time.Now().UTC().Add(sessionMaxAge)
	if err := ss.db.UpdateSessionExpiry(token, newExpiry); err != nil {
		return false
	}
	sess.ExpiresAt = newExpiry
	return true
}

func (ss *SessionStore) CleanupExpired() {
	ss.db.CleanupExpiredSessions()
}

func (ss *SessionStore) Logout(token string) {
	ss.db.DeleteSession(token)
}

func (ss *SessionStore) AdminUser() string {
	return ss.adminUser
}

func (ss *SessionStore) AdminPassword() string {
	return ss.adminPassword
}

// AuthMiddleware protects routes — returns 401 JSON for API requests.
// It also performs sliding session renewal: when less than half the max age
// remains, the session expiry and cookie are refreshed automatically.
func (ss *SessionStore) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		sess, ok := ss.Validate(cookie.Value)
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		// Sliding renewal: refresh session and cookie when < 50% time remains
		if ss.RenewIfNeeded(cookie.Value, sess) {
			SetSessionCookie(w, cookie.Value)
		}
		next.ServeHTTP(w, r)
	})
}

func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

func GetSessionToken(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// --- GitHub OAuth Helpers ---

func (ss *SessionStore) GetGitHubAuthURL(redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", ss.GitHub.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "read:user")
	return "https://github.com/login/oauth/authorize?" + q.Encode()
}

type GitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (ss *SessionStore) ExchangeGitHubCode(code string) (*GitHubUser, error) {
	// Exchange code for token
	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", nil)
	q := req.URL.Query()
	q.Set("client_id", ss.GitHub.ClientID)
	q.Set("client_secret", ss.GitHub.ClientSecret)
	q.Set("code", code)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	var tokenResp GitHubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token from GitHub")
	}

	// Get user info
	userReq, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userReq.Header.Set("Accept", "application/json")

	userResp, err := client.Do(userReq)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	defer userResp.Body.Close()

	userBody, err := io.ReadAll(userResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read user response: %w", err)
	}
	var ghUser GitHubUser
	if err := json.Unmarshal(userBody, &ghUser); err != nil {
		return nil, fmt.Errorf("parse user: %w", err)
	}
	if ghUser.Login == "" {
		return nil, fmt.Errorf("empty login from GitHub")
	}

	return &ghUser, nil
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
