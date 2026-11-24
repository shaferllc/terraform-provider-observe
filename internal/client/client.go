// Package client is a small JSON client for the Observe v1 API (https://observe.live/api/openapi.json).
package client

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

// DefaultEndpoint is the v1 API base URL.
const DefaultEndpoint = "https://observe.live/api/v1"

// Client calls the API with a personal access token.
type Client struct {
	Endpoint  string
	Token     string
	UserAgent string
	HTTP      *http.Client
	// MaxRetries bounds retries of 429 and 502–504 replies.
	MaxRetries int
	// Sleep waits between retries; tests replace it.
	Sleep func(context.Context, time.Duration) error
}

// New returns a client for endpoint (DefaultEndpoint when empty).
func New(endpoint, token, version string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		Token:      token,
		UserAgent:  "terraform-provider-observe/" + version,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		MaxRetries: 5,
		Sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		},
	}
}

// Error is a non-2xx reply.
type Error struct {
	Method, Path string
	Status       int
	Message      string
	Errors       map[string][]string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	for field, errs := range e.Errors {
		msg += fmt.Sprintf("\n  %s: %s", field, strings.Join(errs, "; "))
	}
	return msg
}

// IsNotFound reports whether err is a 404 reply.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// Do sends body as JSON and decodes the reply into out (both optional). It retries rate limits (honouring
// Retry-After) and gateway errors.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 502 && resp.StatusCode <= 504)
		if retryable && attempt < c.MaxRetries {
			if err := c.Sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode >= 300 {
			apiErr := &Error{Method: method, Path: path, Status: resp.StatusCode}
			_ = json.Unmarshal(raw, apiErr)
			var fields struct {
				Message string              `json:"message"`
				Errors  map[string][]string `json:"errors"`
			}
			if json.Unmarshal(raw, &fields) == nil {
				apiErr.Message, apiErr.Errors = fields.Message, fields.Errors
			}
			return apiErr
		}
		if out == nil || len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, time.Minute)
	}
	return min(time.Duration(1<<attempt)*time.Second, 30*time.Second)
}

// Object is a decoded JSON object.
type Object = map[string]any

// Data sends a request and returns the reply's "data" object.
func (c *Client) Data(ctx context.Context, method, path string, body any) (Object, error) {
	var reply struct {
		Data Object `json:"data"`
	}
	if err := c.Do(ctx, method, path, body, &reply); err != nil {
		return nil, err
	}
	return reply.Data, nil
}

// List returns every item of a list endpoint, following meta.last_page when it pages.
func (c *Client) List(ctx context.Context, path string, query url.Values) ([]Object, error) {
	var items []Object
	for page := 1; ; page++ {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		target := path
		if len(q) > 0 {
			target += "?" + q.Encode()
		}
		var reply struct {
			Data []Object `json:"data"`
			Meta struct {
				LastPage int `json:"last_page"`
			} `json:"meta"`
		}
		if err := c.Do(ctx, http.MethodGet, target, nil, &reply); err != nil {
			return nil, err
		}
		items = append(items, reply.Data...)
		if reply.Meta.LastPage <= page {
			return items, nil
		}
	}
}

// Find returns the item with id from a list endpoint, or a 404 Error.
func (c *Client) Find(ctx context.Context, path, id string) (Object, error) {
	items, err := c.List(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if fmt.Sprint(item["id"]) == id {
			return item, nil
		}
	}
	return nil, &Error{Method: http.MethodGet, Path: path + " (id " + id + ")", Status: http.StatusNotFound, Message: "not found"}
}
