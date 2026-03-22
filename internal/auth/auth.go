package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
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

type SessionStore struct {
	mu            sync.RWMutex
	sessions      map[string]Session
	adminUser     string
	adminPassword string
	GitHub        GitHubConfig
}

func NewSessionStore(adminUser, adminPassword string) *SessionStore {
	return &SessionStore{
		sessions:      make(map[string]Session),
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
	ss.mu.Lock()
	ss.sessions[token] = Session{
		Username:  user,
		Role:      "admin",
		ExpiresAt: time.Now().Add(sessionMaxAge),
	}
	ss.mu.Unlock()
	return token, true
}

// LoginGitHub creates a session for a GitHub-authenticated user.
func (ss *SessionStore) LoginGitHub(userID int64, username, githubLogin, avatarURL, role string) string {
	token := generateToken()
	ss.mu.Lock()
	ss.sessions[token] = Session{
		Username:    username,
		UserID:      userID,
		GitHubLogin: githubLogin,
		Role:        role,
		AvatarURL:   avatarURL,
		ExpiresAt:   time.Now().Add(sessionMaxAge),
	}
	ss.mu.Unlock()
	return token
}

func (ss *SessionStore) Validate(token string) (*Session, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s, ok := ss.sessions[token]
	if !ok || time.Now().After(s.ExpiresAt) {
		if ok {
			delete(ss.sessions, token)
		}
		return nil, false
	}
	return &s, true
}

func (ss *SessionStore) CleanupExpired() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now()
	for token, s := range ss.sessions {
		if now.After(s.ExpiresAt) {
			delete(ss.sessions, token)
		}
	}
}

func (ss *SessionStore) Logout(token string) {
	ss.mu.Lock()
	delete(ss.sessions, token)
	ss.mu.Unlock()
}

func (ss *SessionStore) AdminUser() string {
	return ss.adminUser
}

func (ss *SessionStore) AdminPassword() string {
	return ss.adminPassword
}

// AuthMiddleware protects routes — returns 401 JSON for API requests.
func (ss *SessionStore) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if _, ok := ss.Validate(cookie.Value); !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
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
	return fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user",
		ss.GitHub.ClientID, redirectURI,
	)
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
