package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestStatusSecurityAlertsListContract(t *testing.T) {
	for _, tt := range []struct {
		name  string
		items []SecurityItem
		count int
	}{
		{name: "initial or cleared nil slice"},
		{name: "explicit empty slice", items: []SecurityItem{}},
		{name: "active alert", items: []SecurityItem{{Repo: "owner/example", Title: "Example advisory", Severity: "high", URL: "https://example.test/security/1"}}, count: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{status: Status{SecurityAlerts: tt.items, Counts: map[string]int{"securityAlerts": tt.count}, Error: "example partial-data error"}}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
			if w.Code != 200 {
				t.Fatalf("status code %d", w.Code)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			raw, exists := document["securityAlerts"]
			if !exists || string(raw) == "null" {
				t.Fatalf("securityAlerts must be an array: %s", w.Body.String())
			}
			var items []map[string]any
			if err := json.Unmarshal(raw, &items); err != nil {
				t.Fatal(err)
			}
			if len(items) != tt.count {
				t.Fatalf("got %d list items, want %d", len(items), tt.count)
			}
			if tt.count > 0 && (items[0]["repo"] != "owner/example" || items[0]["title"] != "Example advisory" || items[0]["url"] != "https://example.test/security/1") {
				t.Fatal("Homepage field names changed")
			}
			var counts map[string]int
			if err := json.Unmarshal(document["counts"], &counts); err != nil || counts["securityAlerts"] != tt.count {
				t.Fatal("summary count changed")
			}
			var message string
			if err := json.Unmarshal(document["error"], &message); err != nil || message != "example partial-data error" {
				t.Fatal("partial-data error changed")
			}
			if tt.items == nil && s.status.SecurityAlerts != nil {
				t.Fatal("reading status mutated stored data")
			}
		})
	}
}
