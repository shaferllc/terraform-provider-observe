package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetriesRateLimitsHonouringRetryAfter(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if calls < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"m1"}}`))
	}))
	defer server.Close()

	c := New(server.URL, "secret", "test")
	var waits []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	item, err := c.Data(context.Background(), http.MethodGet, "/monitors/m1", nil)
	if err != nil || item["id"] != "m1" {
		t.Fatalf("Data = %v, %v", item, err)
	}
	if len(waits) != 2 || waits[0] != 7*time.Second {
		t.Fatalf("waits = %v, want two of 7s", waits)
	}
}

func TestValidationErrorsAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"The url field is required.","errors":{"url":["The url field is required."]}}`))
	}))
	defer server.Close()
	c := New(server.URL, "t", "test")

	err := c.Do(context.Background(), http.MethodPost, "/monitors", map[string]any{}, nil)
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != 422 || apiErr.Errors["url"][0] != "The url field is required." {
		t.Fatalf("err = %#v", err)
	}
	if !IsNotFound(c.Do(context.Background(), http.MethodGet, "/missing", nil, nil)) {
		t.Fatal("404 is not IsNotFound")
	}
}

func TestListFollowsPagesAndFindMatchesID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"id":"b"}],"meta":{"last_page":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a"}],"meta":{"last_page":2}}`))
	}))
	defer server.Close()
	c := New(server.URL, "t", "test")

	items, err := c.List(context.Background(), "/monitors", nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("List = %v, %v", items, err)
	}
	if _, err := c.Find(context.Background(), "/monitors", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Find(context.Background(), "/monitors", "c"); !IsNotFound(err) {
		t.Fatalf("Find(c) = %v, want not found", err)
	}
}
