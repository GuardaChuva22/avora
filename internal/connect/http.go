package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxBody bounds what one API answer may bring.
const maxBody = 8 << 20

var httpClient = &http.Client{Timeout: 90 * time.Second}

// APIError is an answer the API refused.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("a API respondeu %d: %s", e.Status, cut(e.Body, 300))
}

// request is one API call. Body may be nil, []byte (sent as is, with
// ContentType) or anything else (sent as JSON).
type request struct {
	Method      string
	URL         string
	Header      map[string]string
	Body        any
	ContentType string
}

// do sends req, retrying rate limits, server errors and dropped connections
// (Retry-After honoured, 3 tries), and returns the body of a 2xx answer.
// 401 becomes an AuthError: the connection needs the owner.
func do(ctx context.Context, req request) ([]byte, http.Header, error) {
	var payload []byte
	ctype := req.ContentType
	switch b := req.Body.(type) {
	case nil:
	case []byte:
		payload = b
	default:
		var err error
		if payload, err = json.Marshal(b); err != nil {
			return nil, nil, err
		}
		ctype = "application/json"
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		r, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
		if err != nil {
			return nil, nil, err
		}
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		r.Header.Set("User-Agent", "Avora/1.0")
		for k, v := range req.Header {
			r.Header.Set(k, v)
		}
		resp, err := httpClient.Do(r)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			last = errors.New("não consegui falar com a API (rede)")
			if !wait(ctx, attempt, "") {
				return nil, nil, ctx.Err()
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		if len(data) > maxBody {
			return nil, nil, errors.New("a resposta da API é grande demais")
		}
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return data, resp.Header, nil
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, nil, errReconnect
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			last = &APIError{Status: resp.StatusCode, Body: string(data)}
			if attempt == 2 || !wait(ctx, attempt, resp.Header.Get("Retry-After")) {
				return nil, nil, last
			}
		default:
			return nil, nil, &APIError{Status: resp.StatusCode, Body: string(data)}
		}
	}
	return nil, nil, last
}

func wait(ctx context.Context, attempt int, retryAfter string) bool {
	d := time.Duration(1<<attempt) * time.Second
	if s, err := strconv.ParseFloat(strings.TrimSpace(retryAfter), 64); err == nil && s >= 0 {
		d = time.Duration(min(s, 30) * float64(time.Second))
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// callJSON decodes a JSON answer into out (when not nil).
func callJSON(ctx context.Context, req request, out any) error {
	data, _, err := do(ctx, req)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("resposta inesperada da API: %v", err)
	}
	return nil
}
