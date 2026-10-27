//go:build linux

package reportdelivery

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var ErrPending = errors.New("delivery remains pending")
var ErrRejected = errors.New("delivery requires operator correction")
var idPattern = regexp.MustCompile(`^delivery-[a-f0-9]{32}$`)

type Note struct {
	Target     string `json:"target"`
	Content    string `json:"content"`
	Conditions string `json:"conditions"`
}

type Entry struct {
	Version          int        `json:"version"`
	ID               string     `json:"id"`
	Note             Note       `json:"note"`
	NoteSHA256       string     `json:"note_sha256"`
	ExpectedRevision int        `json:"expected_revision"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	LastClass        string     `json:"last_class,omitempty"`
	ResponseRevision int        `json:"response_revision,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
}

type Client struct {
	base, token string
	http        *http.Client
}

func NewClient(base, token string, client *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("delivery requires a loopback HTTP server")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("delivery is limited to loopback")
	}
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("invalid operator credential")
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &copy}, nil
}

func digest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func directory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("outbox requires a private real directory")
	}
	return nil
}

func write(dir string, e Entry, exclusive bool) error {
	body, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".delivery-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if exclusive {
		err = os.Link(name, filepath.Join(dir, e.ID+".json"))
	} else {
		err = os.Rename(name, filepath.Join(dir, e.ID+".json"))
	}
	if err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func Queue(dir string, note Note) (Entry, error) {
	return queue(dir, "", note)
}

// QueueOnce lets a restarted producer identify the same immutable report.
func QueueOnce(dir, key string, note Note) (Entry, error) {
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return Entry{}, errors.New("invalid report key")
	}
	return queue(dir, key, note)
}

func queue(dir, key string, note Note) (Entry, error) {
	if err := directory(dir); err != nil {
		return Entry{}, err
	}
	if !validNote(note) {
		return Entry{}, errors.New("invalid technical note")
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Entry{}, err
	}
	e := Entry{Version: 1, ID: "delivery-" + hex.EncodeToString(b[:]), Note: note, NoteSHA256: digest(note), Status: "pending", CreatedAt: time.Now().UTC()}
	if key != "" {
		sum := sha256.Sum256([]byte("research-note-v1:" + key))
		e.ID = "delivery-" + hex.EncodeToString(sum[:16])
	}
	err := write(dir, e, true)
	if key != "" && errors.Is(err, os.ErrExist) {
		previous, readErr := Read(dir, e.ID)
		if readErr != nil {
			return Entry{}, readErr
		}
		if previous.NoteSHA256 != e.NoteSHA256 {
			return Entry{}, errors.New("report key already binds different content")
		}
		return previous, nil
	}
	return e, err
}

func validNote(note Note) bool {
	return strings.TrimSpace(note.Target) != "" && len(note.Target) <= 200 && strings.TrimSpace(note.Content) != "" && len(note.Content) <= 4000 && len(note.Conditions) <= 4000
}

func Read(dir, id string) (Entry, error) {
	var e Entry
	if !idPattern.MatchString(id) {
		return e, errors.New("invalid delivery identity")
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return e, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return e, errors.New("delivery record requires a private regular file")
	}
	body, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(body) > 32768 {
		return e, errors.New("invalid delivery record size")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&e) != nil || decoder.Decode(&struct{}{}) != io.EOF || e.Version != 1 || e.ID != id || e.NoteSHA256 != digest(e.Note) || !validNote(e.Note) || e.ExpectedRevision < 0 || e.Attempts < 0 || e.CreatedAt.IsZero() {
		return Entry{}, errors.New("delivery record integrity mismatch")
	}
	if e.Status != "pending" && e.Status != "unknown" && e.Status != "delivered" && e.Status != "rejected" {
		return Entry{}, errors.New("invalid delivery status")
	}
	if e.Status == "delivered" && (e.DeliveredAt == nil || e.ExpectedRevision < 1 || e.ResponseRevision < e.ExpectedRevision || e.Attempts < 1) {
		return Entry{}, errors.New("delivery acknowledgment is incomplete")
	}
	return e, nil
}

func Pending(dir string) ([]Entry, error) {
	if err := directory(dir); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(file.Name(), ".json")
		if !idPattern.MatchString(id) {
			continue
		}
		e, err := Read(dir, id)
		if err != nil {
			return nil, err
		}
		if e.Status == "pending" || e.Status == "unknown" {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	r, err := c.http.Do(req)
	if err != nil {
		return 0, nil, errors.New("local delivery transport failed")
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return r.StatusCode, nil, errors.New("local delivery response unavailable")
	}
	return r.StatusCode, data, nil
}

func (c *Client) Deliver(ctx context.Context, dir, id string) (Entry, error) {
	if err := directory(dir); err != nil {
		return Entry{}, err
	}
	if !idPattern.MatchString(id) {
		return Entry{}, errors.New("invalid identity")
	}
	lock, err := os.OpenFile(filepath.Join(dir, id+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return Entry{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return Entry{}, ErrPending
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	e, err := Read(dir, id)
	if err != nil {
		return e, err
	}
	if e.Status == "delivered" {
		return e, nil
	}
	if e.Status == "rejected" {
		return e, ErrRejected
	}
	if strings.Contains(e.Note.Content, c.token) || strings.Contains(e.Note.Conditions, c.token) {
		return e, errors.New("operator credential in note refused")
	}
	if e.ExpectedRevision == 0 {
		status, body, err := c.request(ctx, "GET", "/api/bootstrap", nil)
		var snapshot struct {
			State struct {
				Revision int `json:"revision"`
			} `json:"state"`
		}
		if err != nil || status != 200 || json.Unmarshal(body, &snapshot) != nil || snapshot.State.Revision < 1 {
			return e, ErrPending
		}
		e.ExpectedRevision = snapshot.State.Revision
	}
	// Persist the exact id and revision before sending; an ambiguous reply never rebases it.
	e.Attempts++
	e.Status = "unknown"
	e.LastClass = "delivery_outcome_unknown"
	if err = write(dir, e, false); err != nil {
		return e, err
	}
	payload := struct {
		ExpectedRevision int    `json:"expected_revision"`
		RequestID        string `json:"request_id"`
		Target           string `json:"target"`
		Kind             string `json:"kind"`
		Content          string `json:"content"`
		Conditions       string `json:"conditions"`
	}{e.ExpectedRevision, e.ID, e.Note.Target, "note", e.Note.Content, e.Note.Conditions}
	body, _ := json.Marshal(payload)
	status, response, err := c.request(ctx, "POST", "/api/memory", body)
	if err == nil && status == 200 {
		var ack struct {
			State struct {
				Revision int `json:"revision"`
			} `json:"state"`
		}
		if json.Unmarshal(response, &ack) == nil && ack.State.Revision >= e.ExpectedRevision {
			now := time.Now().UTC()
			e.Status = "delivered"
			e.DeliveredAt = &now
			e.ResponseRevision = ack.State.Revision
			e.LastClass = "acknowledged"
			return e, write(dir, e, false)
		}
	}
	if err == nil && status == 409 {
		e.Status = "pending"
		e.ExpectedRevision = 0
		e.LastClass = "revision_conflict"
	} else if err == nil && (status == 400 || status == 401 || status == 403 || status == 413) {
		e.Status = "rejected"
		e.LastClass = fmt.Sprintf("http_%d", status)
	}
	if err = write(dir, e, false); err != nil {
		return e, err
	}
	if e.Status == "rejected" {
		return e, ErrRejected
	}
	return e, ErrPending
}
