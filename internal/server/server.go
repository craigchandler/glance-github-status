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
type SecurityConfig struct {
	Dependabot      bool   `json:"dependabot"`
	CodeScanning    bool   `json:"codeScanning"`
	SecretScanning  bool   `json:"secretScanning"`
	MinimumSeverity string `json:"minimumSeverity"`
}
type Config struct {
	Username          string                   `json:"username"`
	Accounts          map[string]AccountConfig `json:"accounts"`
	Repositories      []RepositoryConfig       `json:"repositories"`
	MaxItems          int                      `json:"maxItems"`
	RecentRunsPerRepo int                      `json:"recentRunsPerRepo"`
	Security          *SecurityConfig          `json:"security,omitempty"`
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
type SecurityItem struct {
	Type       string    `json:"type"`
	Repo       string    `json:"repo"`
	Title      string    `json:"title"`
	Severity   string    `json:"severity"`
	URL        string    `json:"url"`
	Number     int       `json:"number"`
	Identifier string    `json:"identifier,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}
type AttentionItem struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
}
type Status struct {
	FailedRuns     []RepoRun       `json:"failedRuns"`
	RecentRuns     []RepoRun       `json:"recentRuns"`
	ReviewRequests []Item          `json:"reviewRequests"`
	OpenPRs        []Item          `json:"openPRs"`
	AssignedIssues []Item          `json:"assignedIssues"`
	SecurityAlerts []SecurityItem  `json:"securityAlerts,omitempty"`
	Attention      []AttentionItem `json:"attention,omitempty"`
	Counts         map[string]int  `json:"counts"`
	RefreshedAt    time.Time       `json:"refreshedAt"`
	Error          string          `json:"error,omitempty"`
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
	for name, a := range cfg.Accounts {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("account name cannot be empty")
		}
		if strings.TrimSpace(a.TokenEnv) == "" {
			return fmt.Errorf("account %q tokenEnv is required", name)
		}
	}
	for i, r := range cfg.Repositories {
		if strings.TrimSpace(r.Name) == "" {
			return fmt.Errorf("repositories[%d].name is required", i)
		}
		if _, ok := cfg.Accounts[r.Account]; !ok {
			return fmt.Errorf("repository %q references unknown account %q", r.Name, r.Account)
		}
	}
	if cfg.Security != nil && cfg.Security.MinimumSeverity != "" && severityRank(cfg.Security.MinimumSeverity) < 0 {
		return fmt.Errorf("security.minimumSeverity must be one of low, medium, high, critical")
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
			errors = append(errors, fmt.Sprintf("%s actions: %v", repo.Name, err))
		} else {
			for _, r := range runs {
				rr := RepoRun{Repo: repo.Name, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, URL: r.HTMLURL, UpdatedAt: r.UpdatedAt}
				st.RecentRuns = append(st.RecentRuns, rr)
			}
			st.FailedRuns = append(st.FailedRuns, currentFailedRuns(repo.Name, runs)...)
		}
		if s.cfg.Security != nil {
			s.collectSecurity(ctx, client, repo.Name, &st, &errors)
		}
	}
	sortRuns(st.RecentRuns)
	sortRuns(st.FailedRuns)
	st.RecentRuns = limitRuns(st.RecentRuns, max)
	st.FailedRuns = limitRuns(st.FailedRuns, max)
	sort.Slice(st.SecurityAlerts, func(i, j int) bool {
		ri, rj := severityRank(st.SecurityAlerts[i].Severity), severityRank(st.SecurityAlerts[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return st.SecurityAlerts[i].CreatedAt.After(st.SecurityAlerts[j].CreatedAt)
	})
	securityTotal := len(st.SecurityAlerts)
	if len(st.SecurityAlerts) > max {
		st.SecurityAlerts = st.SecurityAlerts[:max]
	}
	reviewQ := fmt.Sprintf("is:pr is:open review-requested:%s", s.cfg.Username)
	st.ReviewRequests, st.Counts["reviewRequests"], errors = s.searchAcrossAccounts(ctx, reviewQ, max, errors)
	prQ := fmt.Sprintf("is:pr is:open author:%s", s.cfg.Username)
	st.OpenPRs, st.Counts["openPRs"], errors = s.searchAcrossAccounts(ctx, prQ, max, errors)
	issueQ := fmt.Sprintf("is:issue is:open assignee:%s", s.cfg.Username)
	st.AssignedIssues, st.Counts["assignedIssues"], errors = s.searchAcrossAccounts(ctx, issueQ, max, errors)
	st.Counts["failedRuns"] = len(st.FailedRuns)
	st.Counts["securityAlerts"] = securityTotal
	for _, x := range st.SecurityAlerts {
		st.Attention = append(st.Attention, AttentionItem{Name: shortRepo(x.Repo), Status: attentionSeverity(x.Severity), Message: fmt.Sprintf("%s %s: %s", securityTypeLabel(x.Type), strings.ToUpper(x.Severity), x.Title), URL: x.URL})
	}
	st.Error = strings.Join(errors, "; ")
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	if st.Error != "" {
		return fmt.Errorf("%s", st.Error)
	}
	return nil
}

func (s *Server) collectSecurity(ctx context.Context, client *gh.Client, repo string, st *Status, errors *[]string) {
	cfg := s.cfg.Security
	if cfg == nil {
		return
	}
	min := cfg.MinimumSeverity
	if min == "" {
		min = "low"
	}
	if cfg.Dependabot {
		alerts, err := client.DependabotAlerts(ctx, repo)
		if err != nil {
			if !gh.IsSecurityFeatureUnavailable(err) {
				*errors = append(*errors, fmt.Sprintf("%s Dependabot: %v", repo, err))
			}
		} else {
			for _, a := range alerts {
				sev := normalizeSeverity(a.SecurityAdvisory.Severity)
				if severityRank(sev) < severityRank(min) {
					continue
				}
				title := a.SecurityAdvisory.Summary
				if title == "" {
					title = a.Dependency.Package.Name
				}
				id := a.SecurityAdvisory.GHSAID
				if id == "" {
					id = a.SecurityAdvisory.CVEID
				}
				st.SecurityAlerts = append(st.SecurityAlerts, SecurityItem{Type: "dependabot", Repo: repo, Title: title, Severity: sev, URL: a.HTMLURL, Number: a.Number, Identifier: id, CreatedAt: a.CreatedAt})
			}
		}
	}
	if cfg.CodeScanning {
		alerts, err := client.CodeScanningAlerts(ctx, repo)
		if err != nil {
			if !gh.IsSecurityFeatureUnavailable(err) {
				*errors = append(*errors, fmt.Sprintf("%s code scanning: %v", repo, err))
			}
		} else {
			for _, a := range alerts {
				sev := normalizeSeverity(a.Rule.SecuritySeverityLevel)
				if sev == "unknown" {
					sev = normalizeCodeSeverity(a.Rule.Severity)
				}
				if severityRank(sev) < severityRank(min) {
					continue
				}
				title := a.Rule.Description
				if title == "" {
					title = a.Rule.Name
				}
				if title == "" {
					title = a.Rule.ID
				}
				st.SecurityAlerts = append(st.SecurityAlerts, SecurityItem{Type: "code-scanning", Repo: repo, Title: title, Severity: sev, URL: a.HTMLURL, Number: a.Number, Identifier: a.Rule.ID, CreatedAt: a.CreatedAt})
			}
		}
	}
	if cfg.SecretScanning {
		alerts, err := client.SecretScanningAlerts(ctx, repo)
		if err != nil {
			if !gh.IsSecurityFeatureUnavailable(err) {
				*errors = append(*errors, fmt.Sprintf("%s secret scanning: %v", repo, err))
			}
		} else {
			for _, a := range alerts {
				sev := "critical"
				if severityRank(sev) < severityRank(min) {
					continue
				}
				title := a.SecretTypeDisplayName
				if title == "" {
					title = a.SecretType
				}
				st.SecurityAlerts = append(st.SecurityAlerts, SecurityItem{Type: "secret-scanning", Repo: repo, Title: title, Severity: sev, URL: a.HTMLURL, Number: a.Number, Identifier: a.SecretType, CreatedAt: a.CreatedAt})
			}
		}
	}
}

func normalizeSeverity(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "low", "medium", "high", "critical":
		return v
	}
	return "unknown"
}
func normalizeCodeSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "error":
		return "high"
	case "warning":
		return "medium"
	case "note":
		return "low"
	}
	return "unknown"
}
func severityRank(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	}
	return -1
}
func attentionSeverity(v string) string {
	if strings.EqualFold(v, "critical") {
		return "critical"
	}
	return "warning"
}
func securityTypeLabel(v string) string {
	switch v {
	case "dependabot":
		return "Dependabot"
	case "code-scanning":
		return "Code scanning"
	case "secret-scanning":
		return "Secret scanning"
	}
	return "Security"
}

func (s *Server) searchAcrossAccounts(ctx context.Context, query string, max int, errors []string) ([]Item, int, []string) {
	byURL := map[string]Item{}
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
func searchLabel(q string) string {
	switch {
	case strings.Contains(q, "review-requested:"):
		return "review requests"
	case strings.Contains(q, "author:"):
		return "open PRs"
	case strings.Contains(q, "assignee:"):
		return "assigned issues"
	}
	return "GitHub"
}
func sortRuns(in []RepoRun) {
	sort.Slice(in, func(i, j int) bool { return in[i].UpdatedAt.After(in[j].UpdatedAt) })
}

// currentFailedRuns returns failed runs whose workflow is still in a failed
// state on its branch. A later run of the same workflow on the same branch
// supersedes an earlier failure, while failures on other branches remain visible.
func currentFailedRuns(repo string, runs []gh.WorkflowRun) []RepoRun {
	latest := make(map[string]gh.WorkflowRun)
	for _, run := range runs {
		key := run.Name + "\x00" + run.HeadBranch
		previous, ok := latest[key]
		if !ok || run.UpdatedAt.After(previous.UpdatedAt) {
			latest[key] = run
		}
	}

	failed := make([]RepoRun, 0, len(latest))
	for _, run := range latest {
		if run.Status != "completed" || run.Conclusion == "success" || run.Conclusion == "skipped" || run.Conclusion == "neutral" {
			continue
		}
		failed = append(failed, RepoRun{Repo: repo, Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: run.HTMLURL, UpdatedAt: run.UpdatedAt})
	}
	sortRuns(failed)
	return failed
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
func (s *Server) snapshot() Status { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }
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
	attention := c["failedRuns"] + c["reviewRequests"] + c["assignedIssues"] + c["securityAlerts"]
	var b strings.Builder
	b.WriteString(`<div class="flex flex-column gap-10">`)
	if attention == 0 {
		b.WriteString(`<div class="flex justify-end"><span class="color-positive">CLEAR</span></div>`)
	} else {
		b.WriteString(`<div class="flex justify-end"><span class="color-highlight">` + fmt.Sprintf("%d ATTENTION", attention) + `</span></div>`)
	}
	b.WriteString(`<div class="flex justify-between"><span>Security alerts</span><strong>` + fmt.Sprint(c["securityAlerts"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Failed Actions</span><strong>` + fmt.Sprint(c["failedRuns"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Review requests</span><strong>` + fmt.Sprint(c["reviewRequests"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Open PRs</span><strong>` + fmt.Sprint(c["openPRs"]) + `</strong></div>`)
	b.WriteString(`<div class="flex justify-between"><span>Assigned issues</span><strong>` + fmt.Sprint(c["assignedIssues"]) + `</strong></div>`)
	if len(st.SecurityAlerts) > 0 {
		b.WriteString(`<div class="margin-top-10"><strong>Security</strong></div>`)
		for _, x := range st.SecurityAlerts {
			cls := "color-highlight"
			if x.Severity == "critical" {
				cls = "color-negative"
			}
			b.WriteString(`<div class="flex justify-between gap-10"><a class="text-truncate" href="` + html.EscapeString(x.URL) + `">` + html.EscapeString(shortRepo(x.Repo)) + ` · ` + html.EscapeString(x.Title) + `</a><span class="` + cls + `">` + html.EscapeString(strings.ToUpper(x.Severity)) + `</span></div>`)
		}
	}
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
		b.WriteString(`<details class="details margin-top-10"><summary class="summary color-highlight">Partial data</summary><div class="color-paragraph margin-top-5">` + html.EscapeString(st.Error) + `</div></details>`)
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
