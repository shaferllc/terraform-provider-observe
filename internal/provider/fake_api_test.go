package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is an in-memory stand-in for the v1 API, shaped like the real replies (see openapi.json): monitors with
// their settings block, organization collections, secrets left out of channel replies.
type fakeAPI struct {
	t      *testing.T
	mu     sync.Mutex
	nextID int
	// collection path => id => item
	items map[string]map[string]map[string]any
	// method + path => request bodies, for asserting what was sent
	requests map[string][]map[string]any
}

const fakeOrg = "org1"

func newFakeAPI(t *testing.T) *httptest.Server {
	f := &fakeAPI{t: t, items: map[string]map[string]map[string]any{}, requests: map[string][]map[string]any{}}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	fakes[server.URL] = f
	return server
}

var fakes = map[string]*fakeAPI{}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	path := strings.TrimSuffix(r.URL.Path, "/")
	f.requests[r.Method+" "+path] = append(f.requests[r.Method+" "+path], body)

	switch path {
	case "/organizations":
		reply(w, 200, map[string]any{"data": []any{map[string]any{"id": fakeOrg, "name": "Acme", "slug": "acme", "plan": "team", "role": "owner"}}})
		return
	case "/regions":
		reply(w, 200, map[string]any{"data": []any{map[string]any{"id": "us-east"}, map[string]any{"id": "ap-southeast"}}})
		return
	}

	collection, id := path, ""
	if parts := strings.Split(strings.TrimPrefix(path, "/"), "/"); (parts[0] == "monitors" && len(parts) == 2) || len(parts) == 4 {
		collection, id = "/"+strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
	}
	store := f.items[collection]
	if store == nil {
		store = map[string]map[string]any{}
		f.items[collection] = store
	}

	switch {
	case r.Method == http.MethodGet && id == "":
		list := []any{}
		for _, key := range sortedKeys(store) {
			list = append(list, f.render(collection, store[key]))
		}
		reply(w, 200, map[string]any{"data": list, "meta": map[string]any{"current_page": 1, "last_page": 1}})
	case r.Method == http.MethodPost && id == "":
		f.nextID++
		item := map[string]any{"id": fmt.Sprintf("id%d", f.nextID)}
		f.write(collection, item, body)
		store[item["id"].(string)] = item
		reply(w, 201, map[string]any{"data": f.render(collection, item)})
	case store[id] == nil:
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodGet:
		reply(w, 200, map[string]any{"data": f.render(collection, store[id])})
	case r.Method == http.MethodPatch:
		f.write(collection, store[id], body)
		reply(w, 200, map[string]any{"data": f.render(collection, store[id])})
	case r.Method == http.MethodDelete && strings.HasSuffix(collection, "/maintenance-windows"):
		store[id]["status"] = "cancelled"
		reply(w, 200, map[string]any{"data": f.render(collection, store[id])})
	case r.Method == http.MethodDelete:
		delete(store, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// write applies a create or update body the way the API stores it.
func (f *fakeAPI) write(collection string, item, body map[string]any) {
	for k, v := range body {
		item[k] = v
	}
	switch {
	case collection == "/monitors":
		if item["organization_id"] == nil {
			item["organization_id"] = fakeOrg
		}
	case strings.HasSuffix(collection, "/status-pages"):
		if ids, ok := body["site_ids"]; ok {
			item["monitor_ids"] = ids
		}
		delete(item, "site_ids")
	case strings.HasSuffix(collection, "/maintenance-windows"):
		if item["status"] == nil {
			item["status"] = "scheduled"
		}
	}
}

// render shapes a stored item as the API returns it.
func (f *fakeAPI) render(collection string, item map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range item {
		out[k] = v
	}
	switch {
	case collection == "/monitors":
		settings := map[string]any{}
		for name, prop := range componentProperties("Monitor.settings") {
			settings[name] = defaultFor(name, prop)
		}
		top := map[string]any{"id": item["id"], "url": item["url"], "label": item["label"], "tags": orEmpty(item["tags"]), "critical": item["critical"] == true,
			"organization_id": item["organization_id"], "team_id": item["team_id"], "heartbeat": map[string]any{"ping_url": nil}, "status_page": map[string]any{"url": nil}}
		for k, v := range item {
			if _, isSetting := settings[k]; isSetting {
				settings[k] = v
			}
		}
		if lt, ok := item["lighthouse_thresholds"].(map[string]any); ok {
			merged := defaultFor("lighthouse_thresholds", jsonSchema{}).(map[string]any)
			for k, v := range lt {
				merged[k] = v
			}
			settings["lighthouse_thresholds"] = merged
		}
		if settings["heartbeat_enabled"] == true {
			top["heartbeat"] = map[string]any{"ping_url": "https://observe.live/heartbeat/abc"}
		}
		top["settings"] = settings
		return top
	case strings.HasSuffix(collection, "/notification-channels"):
		for _, secret := range []string{"webhook_url", "bot_token", "routing_key", "api_key", "api_token", "user_key", "headers", "email", "use_org_recipients", "payload_template"} {
			delete(out, secret)
		}
		out["destination"] = "hooks.example.com"
		out["signing_secret"] = "whsec_test"
		for _, k := range []string{"label", "team_id", "quiet_hours_start", "quiet_hours_end", "event_types", "tag_filter", "category_modes"} {
			if _, ok := out[k]; !ok {
				out[k] = nil
			}
		}
		if out["enabled"] == nil {
			out["enabled"] = true
		}
		if out["delivery_hours"] == nil {
			out["delivery_hours"] = "always"
		}
	case strings.HasSuffix(collection, "/status-pages"):
		out["url"] = "https://observe.live/status/" + fmt.Sprint(item["slug"])
		for k, v := range map[string]any{"locale": "en", "private": false, "auto_updates": false, "message": nil, "logo_url": nil, "accent_color": nil} {
			if out[k] == nil {
				out[k] = v
			}
		}
	case strings.HasSuffix(collection, "/escalation-policies"):
		if out["repeat_times"] == nil {
			out["repeat_times"] = 0
		}
		if _, ok := out["team_id"]; !ok {
			out["team_id"] = nil
		}
	case strings.HasSuffix(collection, "/on-call-schedules"):
		if out["handoff_emails"] == nil {
			out["handoff_emails"] = false
		}
		if _, ok := out["team_id"]; !ok {
			out["team_id"] = nil
		}
		if layers, ok := out["layers"].([]any); ok {
			for _, layer := range layers {
				l := layer.(map[string]any)
				for _, k := range []string{"restriction_start", "restriction_end"} {
					if _, ok := l[k]; !ok {
						l[k] = nil
					}
				}
			}
		}
	case strings.HasSuffix(collection, "/maintenance-windows"):
		for _, k := range []string{"description", "team_id", "recurrence", "timezone"} {
			if _, ok := out[k]; !ok {
				out[k] = nil
			}
		}
		for _, k := range []string{"monitor_ids", "tags"} {
			if _, ok := out[k]; !ok {
				out[k] = nil
			}
		}
	case strings.HasSuffix(collection, "/groups"):
		out["monitor_ids"] = orEmpty(out["monitor_ids"])
	case strings.HasSuffix(collection, "/teams"):
		out["members"] = []any{}
	}
	return out
}

func defaultFor(name string, prop jsonSchema) any {
	switch name {
	case "uptime_monitoring_enabled", "ssl_monitoring_enabled":
		return true
	case "uptime_interval_minutes":
		return 5
	case "lighthouse_strategy":
		return "mobile"
	case "lighthouse_thresholds":
		return map[string]any{"min_score": 50, "max_fcp_ms": 3000, "max_lcp_ms": 4000, "max_tbt_ms": 600, "max_cls": 0.25, "max_score_drop": 10}
	}
	t := prop.types()
	switch {
	case len(t) > 0 && t[0] == "boolean":
		return false
	case len(t) > 0 && t[0] == "array":
		return []any{}
	}
	return nil
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
