package researchweb

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/coddy"
)

type browserSession struct {
	CSRF    string
	Expires time.Time
}
type HTTP struct {
	service       *Service
	key           string
	mux           *http.ServeMux
	root          *os.Root
	hosts         map[string]bool
	mu            sync.Mutex
	sessions      map[string]browserSession
	loginWindow   time.Time
	loginFailures int
}

func randomKey() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func AccessKey(config Config, lookup func(string) (string, bool)) (string, error) {
	if config.TokenEnv != "" {
		key, ok := lookup(config.TokenEnv)
		if !ok || len(key) < 32 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n\x00") {
			return "", errors.New("access token environment value must contain 32 to 256 non-whitespace characters")
		}
		return key, nil
	}
	p := filepath.Join(config.DataDir, "access-token")
	if info, err := os.Lstat(p); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("access token file must be private")
		}
		data, err := readBoundedFile(p, 256)
		key := strings.TrimSpace(string(data))
		if err != nil || len(key) < 32 || len(key) > 256 {
			return "", errors.New("invalid access token file")
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	key := randomKey()
	return key, atomicFile(p, []byte(key+"\n"))
}
func NewHTTP(s *Service, key string) (*HTTP, error) {
	if len(key) < 32 {
		return nil, errors.New("access key too short")
	}
	root, err := os.OpenRoot(s.Options.Config.WebDir)
	if err != nil {
		return nil, errors.New("web build unavailable; run npm run build first")
	}
	_, port, err := net.SplitHostPort(s.Options.Config.Listen)
	if err != nil {
		root.Close()
		return nil, err
	}
	h := &HTTP{service: s, key: key, root: root, mux: http.NewServeMux(), sessions: map[string]browserSession{},
		hosts: map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true, net.JoinHostPort("::1", port): true}}
	h.routes()
	return h, nil
}
func (h *HTTP) Close() error { return h.root.Close() }
func secureEqual(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if !h.hosts[r.Host] {
		http.Error(w, "Недопустимый адрес сервера.", http.StatusForbidden)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+r.Host {
		http.Error(w, "Недопустимый источник запроса.", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		h.static(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/session" && r.Method == "POST" {
		if origin == "" {
			http.Error(w, "Нужен источник запроса.", http.StatusForbidden)
			return
		}
		h.login(w, r)
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	bearerOK := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && secureEqual(bearer, h.key)
	var session browserSession
	if !bearerOK {
		cookie, err := r.Cookie("research_session")
		if err == nil {
			h.mu.Lock()
			session = h.sessions[cookie.Value]
			h.mu.Unlock()
		}
		if session.Expires.Before(time.Now()) {
			http.Error(w, "Нужен вход.", http.StatusUnauthorized)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin == "" || !secureEqual(r.Header.Get("X-Research-CSRF"), session.CSRF) {
				http.Error(w, "Неверное подтверждение запроса.", http.StatusForbidden)
				return
			}
		}
	}
	if r.URL.Path == "/api/session" && r.Method == "GET" {
		h.json(w, map[string]any{"csrf": session.CSRF, "operator": "operator"})
		return
	}
	if r.URL.Path == "/api/session" && r.Method == "DELETE" {
		if cookie, err := r.Cookie("research_session"); err == nil {
			h.mu.Lock()
			delete(h.sessions, cookie.Value)
			h.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "research_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != "GET" && h.service.ctx.Err() != nil {
		h.fail(w, errors.New("service stopped"))
		return
	}
	h.mux.ServeHTTP(w, r)
}
func (h *HTTP) login(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if time.Since(h.loginWindow) > time.Minute {
		h.loginWindow = time.Now()
		h.loginFailures = 0
	}
	blocked := h.loginFailures >= 30
	h.mu.Unlock()
	if blocked {
		http.Error(w, "Слишком много попыток входа.", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := bodyJSON(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	if !secureEqual(strings.TrimSpace(body.Token), h.key) {
		h.mu.Lock()
		h.loginFailures++
		h.mu.Unlock()
		http.Error(w, "Неверный ключ.", http.StatusUnauthorized)
		return
	}
	id, csrf := randomKey(), randomKey()
	h.mu.Lock()
	for key, s := range h.sessions {
		if s.Expires.Before(time.Now()) {
			delete(h.sessions, key)
		}
	}
	if len(h.sessions) >= 64 {
		h.mu.Unlock()
		http.Error(w, "Достигнуто ограничение сеансов.", http.StatusTooManyRequests)
		return
	}
	h.sessions[id] = browserSession{CSRF: csrf, Expires: time.Now().Add(12 * time.Hour)}
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "research_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	h.json(w, map[string]any{"csrf": csrf, "operator": "operator"})
}
func bodyJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return RuleError("Требуется application/json.")
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		return ErrLimit
	}
	return decodeJSON(data, dest)
}
func (h *HTTP) fail(w http.ResponseWriter, err error) {
	code, message := http.StatusServiceUnavailable, "Запрос не выполнен. Обновите состояние; внешнее действие нельзя повторять без сверки."
	var rule RuleError
	switch {
	case errors.As(err, &rule):
		code, message = http.StatusBadRequest, rule.Error()
	case errors.Is(err, ErrConflict) || errors.Is(err, coddy.ErrConflict):
		code, message = http.StatusConflict, "Снимок изменился. Обновите состояние."
	case errors.Is(err, ErrLimit) || errors.Is(err, coddy.ErrLimit):
		code, message = http.StatusRequestEntityTooLarge, "Достигнуто ограничение данных."
	case errors.Is(err, coddy.ErrApproval):
		code, message = http.StatusBadRequest, "Нужно явное подтверждение публикации."
	case errors.Is(err, coddy.ErrUnknown):
		code, message = http.StatusConflict, "Исход публикации неизвестен. Выполните сверку."
	case errors.Is(err, coddy.ErrPaused):
		code, message = http.StatusConflict, "Делегирование приостановлено."
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func (h *HTTP) json(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		h.fail(w, err)
		return
	}
	// Mask credential values in strings without changing the response's JSON keys.
	keys := []string{h.service.Options.Config.TokenEnv}
	for _, p := range h.service.Options.Profiles {
		if p.Model != nil {
			keys = append(keys, p.Model.TokenEnv)
		}
		if p.External != nil {
			for _, ref := range p.External.SecretEnv {
				keys = append(keys, ref)
			}
		}
	}
	if h.service.Options.Coddy != nil {
		keys = append(keys, h.service.Options.Coddy.TokenEnv)
	}
	secrets := []string{h.key}
	for _, key := range keys {
		if key != "" {
			if value, ok := h.service.Options.Lookup(key); ok && value != "" {
				secrets = append(secrets, value)
			}
		}
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		h.fail(w, err)
		return
	}
	var clean func(any) any
	clean = func(value any) any {
		switch v := value.(type) {
		case string:
			for _, secret := range secrets {
				v = strings.ReplaceAll(v, secret, "[REDACTED]")
			}
			return v
		case map[string]any:
			for key, item := range v {
				v[key] = clean(item)
			}
			return v
		case []any:
			for i, item := range v {
				v[i] = clean(item)
			}
			return v
		default:
			return v
		}
	}
	data, err = json.Marshal(clean(decoded))
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}
func (h *HTTP) view(w http.ResponseWriter) {
	v, err := h.service.Store.Read()
	if err != nil {
		h.fail(w, err)
		return
	}
	profiles := h.service.Profiles()
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	workspaces := []map[string]string{}
	for _, workspace := range h.service.Options.Config.Workspaces {
		workspaces = append(workspaces, map[string]string{"id": workspace.ID, "label": workspace.Label})
	}
	coddyInfo := map[string]any{"configured": h.service.Options.Coddy != nil, "available": false}
	if c := h.service.Options.Coddy; c != nil {
		value, ok := h.service.Options.Lookup(c.TokenEnv)
		coddyInfo["available"] = ok && value != ""
		coddyInfo["repository"] = c.Repository
		coddyInfo["bot_login"] = c.BotLogin
	}
	h.json(w, map[string]any{"state": v, "profiles": profiles, "workspaces": workspaces, "coddy": coddyInfo, "operational": h.service.ctx.Err() == nil})
}
func (h *HTTP) routes() {
	h.mux.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) { h.view(w) })
	h.mux.HandleFunc("GET /api/history/{revision}", func(w http.ResponseWriter, r *http.Request) {
		revision, err := strconv.Atoi(r.PathValue("revision"))
		if err != nil || revision < 1 {
			h.fail(w, RuleError("Неверный номер снимка."))
			return
		}
		v, err := h.service.Store.Snapshot(revision)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, v)
	})
	h.mux.HandleFunc("POST /api/actions", func(w http.ResponseWriter, r *http.Request) {
		var a Action
		if err := bodyJSON(w, r, &a); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.Act(a); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/attempts", func(w http.ResponseWriter, r *http.Request) {
		var a RunRequest
		if err := bodyJSON(w, r, &a); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.Start(a); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/attempts/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.Cancel(r.PathValue("id")); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/attempts/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Result(r.PathValue("id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, v)
	})
	h.mux.HandleFunc("POST /api/coddy", func(w http.ResponseWriter, r *http.Request) {
		var body PrepareCoddy
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.PrepareCoddy(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/coddy/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.CoddyLocal(r.PathValue("id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, v)
	})
	h.mux.HandleFunc("POST /api/coddy/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		var body CoddyCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		v, err := h.service.CoddyCommand(r.Context(), r.PathValue("id"), body)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, v)
	})
	h.mux.HandleFunc("GET /api/coddy/{id}/candidates/{sha}", func(w http.ResponseWriter, r *http.Request) {
		data, err := h.service.CoddyCandidate(r.PathValue("id"), r.PathValue("sha"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, json.RawMessage(data))
	})
}
func (h *HTTP) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if name != "index.html" && !strings.HasPrefix(name, "assets/") && name != "favicon.ico" {
		http.NotFound(w, r)
		return
	}
	f, err := h.root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}
