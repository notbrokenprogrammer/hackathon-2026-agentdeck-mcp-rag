package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"feeders/internal/server"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"feeders/internal/model"
	"feeders/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const CookieName = "session_id"

type session struct {
	user    model.User
	expires time.Time
}

type Handler struct {
	accounts  storage.Accounts
	log       *slog.Logger
	ttl       time.Duration
	secure    bool
	mu        sync.Mutex
	sessions  map[string]session
	work      chan struct{}
	dummyHash []byte
}

func New(accounts storage.Accounts, ttl time.Duration, secure bool, log *slog.Logger) (*Handler, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("invalid-account-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	h := &Handler{accounts: accounts, log: log, ttl: ttl, secure: secure, sessions: make(map[string]session), work: make(chan struct{}, 4), dummyHash: hash}
	if err := h.restoreSessions(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", h.Register)
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/auth/me", h.Me)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func readCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var c credentials
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		fail(w, 400, "invalid_request", "Некорректный JSON")
		return c, false
	}
	if d.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_request", "Ожидается один JSON-объект")
		return c, false
	}
	c.Username = strings.ToLower(strings.TrimSpace(c.Username))
	valid := utf8.RuneCountInString(c.Username) >= 3 && utf8.RuneCountInString(c.Username) <= 32
	for _, ch := range c.Username {
		valid = valid && (unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-')
	}
	if !valid || len(c.Password) < 6 || len(c.Password) > 72 {
		fail(w, 400, "invalid_credentials", "Имя: 3–32 буквы, цифры, _ или -. Пароль: 6–72 байта.")
		return c, false
	}
	return c, true
}

func (h *Handler) acquire(w http.ResponseWriter) bool {
	select {
	case h.work <- struct{}{}:
		return true
	default:
		fail(w, 429, "busy", "Повторите попытку позже")
		return false
	}
}

func token() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	c, ok := readCredentials(w, r)
	if !ok {
		return
	}
	if !h.acquire(w) {
		return
	}
	defer func() { <-h.work }()
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		h.internal(w, err)
		return
	}
	id, err := token()
	if err != nil {
		h.internal(w, err)
		return
	}
	u := model.User{ID: id, Username: c.Username}
	err = h.accounts.CreateAccount(r.Context(), model.Account{User: u, PasswordHash: hash})
	if errors.Is(err, storage.ErrConflict) {
		fail(w, 409, "username_taken", "Имя уже занято")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	if err = h.issue(w, r, u); err != nil {
		h.internal(w, err)
		return
	}
	respond(w, 201, map[string]any{"success": true, "user": u})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	c, ok := readCredentials(w, r)
	if !ok {
		return
	}
	if !h.acquire(w) {
		return
	}
	defer func() { <-h.work }()
	account, err := h.accounts.FindAccount(r.Context(), c.Username)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		h.internal(w, err)
		return
	}
	hash := account.PasswordHash
	if err != nil {
		hash = h.dummyHash
	}
	compare := bcrypt.CompareHashAndPassword(hash, []byte(c.Password))
	if err != nil || compare != nil {
		fail(w, 401, "unauthorized", "Неверное имя или пароль")
		return
	}
	if err = h.issue(w, r, account.User); err != nil {
		h.internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"success": true, "user": account.User})
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request, u model.User) error {
	id, err := token()
	if err != nil {
		return err
	}
	now := time.Now()
	expires := now.Add(h.ttl)
	h.mu.Lock()
	for k, s := range h.sessions {
		if !now.Before(s.expires) {
			delete(h.sessions, k)
		}
	}
	if old, err := r.Cookie(CookieName); err == nil {
		delete(h.sessions, sessionKey(old.Value))
	}
	h.sessions[sessionKey(id)] = session{u, expires}
	if err := h.persistSessionsLocked(); err != nil {
		h.mu.Unlock()
		return err
	}
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.secure || server.IsHTTPS(r), MaxAge: int(h.ttl.Seconds()), Expires: expires})
	return nil
}

// Resolve also works for an upgraded request and checks expiry/revocation each time.
func (h *Handler) Resolve(r *http.Request) (model.User, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return model.User{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionKey(cookie.Value)]
	if ok && !time.Now().Before(s.expires) {
		delete(h.sessions, sessionKey(cookie.Value))
		ok = false
	}
	return s.user, ok
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		h.mu.Lock()
		delete(h.sessions, sessionKey(cookie.Value))
		if err := h.persistSessionsLocked(); err != nil {
			h.mu.Unlock()
			h.internal(w, err)
			return
		}
		h.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.secure || server.IsHTTPS(r), MaxAge: -1})
	respond(w, 200, map[string]bool{"success": true})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := h.Resolve(r)
	if !ok {
		fail(w, 401, "unauthorized", "Требуется вход")
		return
	}
	respond(w, 200, map[string]any{"success": true, "user": u})
}

func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.log.Error("auth operation failed", "error", err)
	fail(w, 500, "internal_error", "Ошибка сервера")
}
func fail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]string{"code": code, "message": message})
}
func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type savedSession struct {
	User    model.User `json:"user"`
	Expires time.Time  `json:"expires"`
}

func sessionKey(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }
func (h *Handler) restoreSessions() error {
	store, ok := h.accounts.(storage.StateStore)
	if !ok {
		return nil
	}
	rows := map[string]savedSession{}
	if err := store.LoadState("sessions_v1", &rows); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	for key, row := range rows {
		if time.Now().Before(row.Expires) {
			h.sessions[key] = session{row.User, row.Expires}
		}
	}
	return nil
}
func (h *Handler) persistSessionsLocked() error {
	store, ok := h.accounts.(storage.StateStore)
	if !ok {
		return nil
	}
	rows := map[string]savedSession{}
	for key, row := range h.sessions {
		rows[key] = savedSession{row.user, row.expires}
	}
	return store.SaveState("sessions_v1", rows)
}
