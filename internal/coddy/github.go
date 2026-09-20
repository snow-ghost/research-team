package coddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type github struct {
	config Config
	token  string
	client *http.Client
}

type HTTPError struct {
	Status            int
	RetryAfterSeconds int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GitHub returned HTTP %d (retry-after seconds: %d); response body withheld", e.Status, e.RetryAfterSeconds)
}

func newGitHub(c Config, lookup func(string) (string, bool)) (*github, error) {
	token, ok := lookup(c.TokenEnv)
	if !ok || token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return nil, errors.New("GitHub credential unavailable or invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &github{config: c, token: token, client: &http.Client{
		Transport: transport, Timeout: time.Duration(c.TimeoutSeconds) * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}
func (g *github) path(suffix string) string { return "/repos/" + g.config.Repository + suffix }
func intString(n int64) string              { return strconv.FormatInt(n, 10) }

func (g *github) request(ctx context.Context, method, path string, input any, output any) error {
	var data []byte
	var err error
	if input != nil {
		data, err = json.Marshal(input)
		if err != nil || len(data) > 64*1024 {
			return ErrLimit
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(g.config.APIURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid GitHub request")
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", g.config.APIVersion)
	req.Header.Set("User-Agent", "research-team-coddy-connector")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("GitHub transport failed; response unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &HTTPError{Status: resp.StatusCode, RetryAfterSeconds: retry}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return errors.New("GitHub response interrupted")
	}
	if len(body) > 2<<20 {
		return ErrLimit
	}
	if json.Unmarshal(body, output) != nil {
		return ErrProtocol
	}
	return nil
}

func list[T any](ctx context.Context, g *github, path string, query url.Values) ([]T, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("per_page", "100")
	var result []T
	totalBytes := 0
	for page := 1; page <= g.config.MaxPages; page++ {
		query.Set("page", strconv.Itoa(page))
		var batch []T
		if err := g.request(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &batch); err != nil {
			return nil, err
		}
		if batch == nil || len(batch) > 100 {
			return nil, ErrProtocol
		}
		encoded, _ := json.Marshal(batch)
		totalBytes += len(encoded)
		if totalBytes > 8<<20 {
			return nil, ErrLimit
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
	return nil, ErrLimit
}

func (g *github) verifyActor(ctx context.Context) error {
	var user User
	if err := g.request(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return err
	}
	if !strings.EqualFold(user.Login, g.config.RequesterLogin) {
		return errors.New("credential owner does not match configured requester")
	}
	return nil
}
