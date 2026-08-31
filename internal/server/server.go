package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	gh "github.com/craigchandler/glance-github-status/internal/github"
)

type AccountConfig struct {
	TokenEnv string `json:"tokenEnv"`
}

type RepositoryConfig struct {
	Name    string `json:"name"`
	Account string `json:"account"`
}

type Config struct {
	Username          string                   `json:"username"`
	Accounts          map[string]AccountConfig `json:"accounts"`
	Repositories      []RepositoryConfig       `json:"repositories"`
	MaxItems          int                      `json:"maxItems"`
	RecentRunsPerRepo int                      `json:"recentRunsPerRepo"`
}

type RepoRun struct {
	Repo, Name, Status, Conclusion, URL string
	UpdatedAt                           time.Time
}

type Item struct {
	Repo, Title, URL string
	Number           int
	UpdatedAt        time.Time
}

type Status struct {
	FailedRuns     []RepoRun      `json:"failedRuns"`
	RecentRuns     []RepoRun      `json:"recentRuns"`
	ReviewRequests []Item         `json:"reviewRequests"`
	OpenPRs        []Item         `json:"openPRs"`
	AssignedIssues []Item         `json:"assignedIssues"`
	Counts         map[string]int `json:"counts"`
	RefreshedAt    time.Time      `json:"refreshedAt"`
	Error          string         `json:"error,omitempty"`
}

type Server struct {
	cfg     Config
	clients map[string]*gh.Client
	mu      sync.RWMutex
	status  Status
}

func New(cfg Config, clients map[string]*gh.Client) *Server {
	return &Server{cfg: cfg, clients: clients}
}

func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Username) == "" {
		return fmt.Errorf("config username is required")
	}
	if len(cfg.Accounts) == 0 {
		return fmt.Errorf("config must define at least one account")
	}
	for name, account := range cfg.Accounts {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("account name cannot be empty")
		}
		if strings.TrimSpace(account.TokenEnv) == "" {
			return fmt.Errorf("account %q tokenEnv is required", name)
		}
	}
	for i, repo := range cfg.Repositories {
		if strings.TrimSpace(repo.Name) == "" {
			return fmt.Errorf("repositories[%d].name is required", i)
		}
		if _, ok := cfg.Accounts[repo.Account]; !ok {
			return fmt.Errorf("repository %q references unknown account %q", repo.Name, repo.Account)
		}
	}
	return nil
}

func (s *Server) Refresh(ctx context.Context) error {
	st := Status{Counts: map[string]int{}, RefreshedAt: time.Now()}
	max := s.cfg.MaxItems
	if max <= 0 {
		max = 8
	}
	runsN := s.cfg.RecentRunsPerRepo
	if runsN <= 0 {
		runsN = 3
	}

	var errors []string
	for _, repo := range s.cfg.Repositories {
		client := s.clients[repo.Account]
		if client == nil {
			errors = append(errors, fmt.Sprintf("%s: no client for account %q", repo.Name, repo.Account))
			continue
		}
		runs, err := client.WorkflowRuns(ctx, repo.Name, runsN)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", repo.Name, err))
			continue
		}
		for _, r := range runs {
			rr := RepoRun{Repo: repo.Name, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, URL: r.HTMLURL, UpdatedAt: r.UpdatedAt}
			st.RecentRuns = append(st.RecentRuns, rr)
			if r.Status == "completed" && r.Conclusion != "success" && r.Conclusion != "skipped" && r.Conclusion != "neutral" {
				st.FailedRuns = append(st.FailedRuns, rr)
			}
		}
	}
	sortRuns(st.RecentRuns)
	sortRuns(st.FailedRuns)
	st.RecentRuns = limitRuns(st.RecentRuns, max)
	st.FailedRuns = limitRuns(st.FailedRuns, max)

	reviewQ := fmt.Sprintf("is:pr is:open review-requested:%s", s.cfg.Username)
	st.ReviewRequests, st.Counts["reviewRequests"], errors = s.searchAcrossAccounts(ctx, reviewQ, max, errors)

	prQ := fmt.Sprintf("is:pr is:open author:%s", s.cfg.Username)
	st.OpenPRs, st.Counts["openPRs"], errors = s.searchAcrossAccounts(ctx, prQ, max, errors)

	issueQ := fmt.Sprintf("is:issue is:open assignee:%s", s.cfg.Username)
	st.AssignedIssues, st.Counts["assignedIssues"], errors = s.searchAcrossAccounts(ctx, issueQ, max, errors)

	st.Counts["failedRuns"] = len(st.FailedRuns)
	st.Error = strings.Join(errors, "; ")

	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	if st.Error != "" {
		return fmt.Errorf("%s", st.Error)
	}
	return nil
}

func (s *Server) searchAcrossAccounts(ctx context.Context, query string, max int, errors []string) ([]Item, int, []string) {
	byURL := make(map[string]Item)
	for accountName, client := range s.clients {
		items, _, err := client.SearchIssues(ctx, query, max)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s search via account %q: %v", searchLabel(query), accountName, err))
			continue
		}
		for _, item := range mapItems(items) {
			byURL[item.URL] = item
		}
	}
	out := make([]Item, 0, len(byURL))
	for _, item := range byURL {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	total := len(out)
	if len(out) > max {
		out = out[:max]
	}
	return out, total, errors
}

func searchLabel(query string) string {
	switch {
	case strings.Contains(query, "review-requested:"):
		return "review requests"
	case strings.Contains(query, "author:"):
		return "open PRs"
	case strings.Contains(query, "assignee:"):
		return "assigned issues"
	default:
		return "GitHub"
	}
}

func sortRuns(in []RepoRun) {
	sort.Slice(in, func(i, j int) bool { return in[i].UpdatedAt.After(in[j].UpdatedAt) })
}

func limitRuns(in []RepoRun, max int) []RepoRun {
	if len(in) > max {
		return in[:max]
	}
	return in
}

func mapItems(in []gh.SearchItem) []Item {
	out := make([]Item, 0, len(in))
	for _, x := range in {
		out = append(out, Item{Repo: gh.RepoNameFromAPIURL(x.RepositoryURL), Title: x.Title, URL: x.HTMLURL, Number: x.Number, UpdatedAt: x.UpdatedAt})
	}
	return out
}

func (s *Server) snapshot() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		st := s.snapshot()
		w.Header().Set("Content-Type", "application/json")
		ok := !st.RefreshedAt.IsZero() && time.Since(st.RefreshedAt) < 30*time.Minute
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "error": st.Error, "refreshedAt": st.RefreshedAt})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.snapshot())
	})
	mux.HandleFunc("/widget", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Widget-Title", "GitHub")
    w.Header().Set("Widget-Content-Type", "html")
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    _, _ = w.Write([]byte(renderWidget(s.snapshot())))
})
	return mux
}

func renderWidget(st Status) string {
	c := st.Counts
	attention := c["failedRuns"] + c["reviewRequests"] + c["assignedIssues"]
	var b strings.Builder
	b.WriteString(`<div class="flex flex-column gap-10">`)
  b.WriteString(`<div class="flex justify-end"><span class="color-positive">`)
	if attention == 0 {
		b.WriteString("CLEAR")
	} else {
		b.WriteString(fmt.Sprintf("%d ATTENTION", attention))
	}
	b.WriteString(`</span></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Failed Actions</span><strong>` + fmt.Sprint(c["failedRuns"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Review requests</span><strong>` + fmt.Sprint(c["reviewRequests"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Open PRs</span><strong>` + fmt.Sprint(c["openPRs"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Assigned issues</span><strong>` + fmt.Sprint(c["assignedIssues"]) + `</strong></div>`)
	if len(st.FailedRuns) > 0 {
		b.WriteString(`<div class="margin-top-10"><strong>Failed runs</strong></div>`)
		for _, r := range st.FailedRuns {
			b.WriteString(`<div class="flex justify-between gap-10"><a class="text-truncate" href="` + html.EscapeString(r.URL) + `">` + html.EscapeString(shortRepo(r.Repo)) + ` · ` + html.EscapeString(r.Name) + `</a><span>` + html.EscapeString(r.Conclusion) + `</span></div>`)
		}
	}
	if len(st.ReviewRequests) > 0 {
		b.WriteString(`<div class="margin-top-10"><strong>Awaiting your review</strong></div>`)
		for _, x := range st.ReviewRequests {
			b.WriteString(itemHTML(x))
		}
	}
	if len(st.RecentRuns) > 0 {
		b.WriteString(`<div class="margin-top-10"><strong>Recent Actions</strong></div>`)
		for _, r := range st.RecentRuns {
			mark := "…"
			if r.Status == "completed" {
				if r.Conclusion == "success" {
					mark = "✓"
				} else {
					mark = "✗"
				}
			}
			b.WriteString(`<div class="flex justify-between gap-10"><a class="text-truncate" href="` + html.EscapeString(r.URL) + `">` + html.EscapeString(shortRepo(r.Repo)) + ` · ` + html.EscapeString(r.Name) + `</a><span>` + mark + `</span></div>`)
		}
	}
	if st.Error != "" {
		b.WriteString(`<div class="color-negative">Partial data: ` + html.EscapeString(st.Error) + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func itemHTML(x Item) string {
	return `<div class="text-truncate"><a href="` + html.EscapeString(x.URL) + `">` + html.EscapeString(shortRepo(x.Repo)) + ` #` + fmt.Sprint(x.Number) + ` · ` + html.EscapeString(x.Title) + `</a></div>`
}

func shortRepo(r string) string {
	if i := strings.IndexByte(r, '/'); i >= 0 {
		return r[i+1:]
	}
	return r
}
