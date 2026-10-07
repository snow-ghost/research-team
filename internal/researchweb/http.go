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
	workerKeys := map[string]bool{}
	for _, worker := range s.Options.Config.Workers {
		value, _ := s.Options.Lookup(worker.TokenEnv)
		if value == "" {
			continue
		}
		if len(value) < 32 || len(value) > 256 || strings.ContainsAny(value, " \t\r\n") || secureEqual(value, key) || workerKeys[value] {
			root.Close()
			return nil, errors.New("worker keys must be private, unique and distinct from the operator key")
		}
		workerKeys[value] = true
	}
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
	if strings.HasPrefix(r.URL.Path, "/api/workers/") {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			http.Error(w, "Неверный адрес.", 404)
			return
		}
		worker := h.service.worker(parts[2])
		key := ""
		if worker != nil {
			key, _ = h.service.Options.Lookup(worker.TokenEnv)
		}
		if len(key) < 32 || !secureEqual(r.Header.Get("Authorization"), "Bearer "+key) {
			http.Error(w, "Нужен ключ исполнителя.", 401)
			return
		}
		if h.service.ctx.Err() != nil {
			h.fail(w, errors.New("service stopped"))
			return
		}
		h.mux.ServeHTTP(w, r)
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
	for _, worker := range h.service.Options.Config.Workers {
		keys = append(keys, worker.TokenEnv)
	}
	for _, channel := range h.service.Options.Config.Telegram {
		keys = append(keys, channel.TokenEnv)
	}
	for _, p := range h.service.profileList() {
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
	if secret := databaseSecret(h.service.Options.Config, h.service.Options.Lookup); secret != "" {
		secrets = append(secrets, secret)
	}
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
	channels := []map[string]any{}
	for _, c := range h.service.Options.Config.Telegram {
		token, ok := h.service.Options.Lookup(c.TokenEnv)
		channels = append(channels, map[string]any{"id": c.ID, "label": c.Label, "available": ok && token != ""})
	}
	workers := []map[string]any{}
	for _, c := range h.service.Options.Config.Workers {
		key, _ := h.service.Options.Lookup(c.TokenEnv)
		workers = append(workers, map[string]any{"id": c.ID, "label": c.Label, "profiles": c.Profiles, "available": len(key) >= 32})
	}
	h.json(w, map[string]any{"state": v, "profiles": profiles, "workspaces": workspaces, "coddy": coddyInfo, "database": h.service.Store.Backend(), "operational": h.service.ctx.Err() == nil, "lean": map[string]any{"configured": h.service.Options.Checker != nil}, "telegram": channels, "workers": workers})
}
func (h *HTTP) routes() {
	h.mux.HandleFunc("POST /api/comparisons", func(w http.ResponseWriter, r *http.Request) {
		var b ComparisonRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ImportComparison(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/lemmas/signatures", func(w http.ResponseWriter, r *http.Request) {
		var b SignatureRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.IndexLemma(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/lemmas/search", func(w http.ResponseWriter, r *http.Request) {
		var b LemmaQuery
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		hits, err := h.service.SearchLemmas(b)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, map[string]any{"hits": hits})
	})
	h.mux.HandleFunc("POST /api/lemmas/applications", func(w http.ResponseWriter, r *http.Request) {
		var b ApplicabilityRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.CreateApplicability(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/planning", func(w http.ResponseWriter, r *http.Request) {
		var b PlanningRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.RequestPlanning(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/profile-templates", func(w http.ResponseWriter, r *http.Request) {
		h.json(w, h.service.Options.Profiles)
	})
	h.mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var b ProfileRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.CreateProfile(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/memory", func(w http.ResponseWriter, r *http.Request) {
		var b MemoryRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.AddMemory(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/memory/search", func(w http.ResponseWriter, r *http.Request) {
		hits, err := h.service.SearchMemory(r.URL.Query().Get("q"), r.URL.Query().Get("study"), r.URL.Query().Get("other_studies") == "true")
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, map[string]any{"hits": hits})
	})
	h.mux.HandleFunc("GET /api/research-methods", func(w http.ResponseWriter, r *http.Request) { h.json(w, researchMethods) })
	h.mux.HandleFunc("POST /api/branches", func(w http.ResponseWriter, r *http.Request) {
		var b BranchRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.CreateBranch(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/branches/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		var b BranchCommand
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlBranch(r.PathValue("id"), b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/entities/{id}/refutation-source", func(w http.ResponseWriter, r *http.Request) {
		var b ProofSourceRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SubmitRefutation(r.PathValue("id"), b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/refutations", func(w http.ResponseWriter, r *http.Request) {
		var b VerifyRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.StartRefutation(b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/refutations/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		var b RefutationReviewRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.BindRefutationReview(r.PathValue("id"), b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/refutations/{id}/accept", func(w http.ResponseWriter, r *http.Request) {
		var b RefutationAcceptanceRequest
		if err := bodyJSON(w, r, &b); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.AcceptRefutation(r.PathValue("id"), b); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		h.json(w, map[string]any{"storage": h.service.Store.Health(), "operational": h.service.ctx.Err() == nil})
	})
	h.mux.HandleFunc("POST /api/studies/{id}/budget", func(w http.ResponseWriter, r *http.Request) {
		var body BudgetRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SetStudyBudget(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/results", func(w http.ResponseWriter, r *http.Request) {
		var body ResultRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.BindResult(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/decompositions", func(w http.ResponseWriter, r *http.Request) {
		var body ProposalRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ImportDecomposition(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/decompositions/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		var body ProposalRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ApplyDecomposition(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/library", func(w http.ResponseWriter, r *http.Request) {
		var body PublishRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.PublishLemma(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/library/{id}/source", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Store.Read()
		if err != nil {
			h.fail(w, err)
			return
		}
		for _, l := range v.Library {
			if l.ID == r.PathValue("id") && l.Status == "ready" && libraryMatches(&v.Data, l) {
				h.json(w, map[string]any{"module": l.Module, "source": l.Source, "report": l.Report, "lemma": l.Lemma, "revision": l.LemmaRevision})
				return
			}
		}
		h.fail(w, RuleError("Действующий модуль не найден."))
	})
	h.mux.HandleFunc("GET /api/studies/{id}/observability", func(w http.ResponseWriter, r *http.Request) {
		value, err := h.service.ObserveStudy(r.PathValue("id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("GET /api/studies/{id}/report", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Store.Read()
		if err != nil {
			h.fail(w, err)
			return
		}
		for _, study := range v.Studies {
			if study.ID == r.PathValue("id") {
				h.json(w, map[string]any{"study": study.ID, "revision": v.Revision, "text": studyStatus(v.Data, study.ID)})
				return
			}
		}
		h.fail(w, RuleError("Исследование не найдено."))
	})
	h.mux.HandleFunc("POST /api/worker-bindings", func(w http.ResponseWriter, r *http.Request) {
		var body BindingRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.BindWorker(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/workers/{id}/claim", func(w http.ResponseWriter, r *http.Request) {
		var body WorkerRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		packet, err := h.service.ClaimWorker(r.PathValue("id"), body)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, packet)
	})
	h.mux.HandleFunc("POST /api/workers/{id}/attempts/{attempt}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var body WorkerRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.HeartbeatWorker(r.PathValue("id"), r.PathValue("attempt"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, map[string]bool{"ok": true})
	})
	h.mux.HandleFunc("POST /api/workers/{id}/attempts/{attempt}/result", func(w http.ResponseWriter, r *http.Request) {
		var body WorkerRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SubmitWorker(r.PathValue("id"), r.PathValue("attempt"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, map[string]bool{"ok": true})
	})
	h.mux.HandleFunc("POST /api/telegram/bindings", func(w http.ResponseWriter, r *http.Request) {
		var body BindingRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.BindTelegram(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/telegram/messages", func(w http.ResponseWriter, r *http.Request) {
		var body MessageRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SendStudyMessage(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/teams", func(w http.ResponseWriter, r *http.Request) {
		var body TeamRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.StartTeam(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/teams/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		var body TeamCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlTeam(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/entities/{id}/formal-goal", func(w http.ResponseWriter, r *http.Request) {
		var body FormalGoalRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SetFormalGoal(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/verifications", func(w http.ResponseWriter, r *http.Request) {
		var body VerifyRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.StartVerification(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/entities/{id}/proof-source", func(w http.ResponseWriter, r *http.Request) {
		var body ProofSourceRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SubmitProofSource(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/verifications/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		var body AttachVerificationRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.AttachVerification(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/attempts/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		after := 0
		if value := r.URL.Query().Get("after"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				h.fail(w, RuleError("Неверный номер события."))
				return
			}
			after = n
		}
		rows, err := h.service.Journal(r.PathValue("id"), after)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, rows)
	})
	h.mux.HandleFunc("POST /api/attempts/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		var body ResumeRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ResumeAttempt(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/cycles", func(w http.ResponseWriter, r *http.Request) {
		var request CycleRequest
		if err := bodyJSON(w, r, &request); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.StartCycle(request); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/cycles/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		var request CycleCommand
		if err := bodyJSON(w, r, &request); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlCycle(r.PathValue("id"), request); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
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
