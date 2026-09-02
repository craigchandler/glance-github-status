package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string { return fmt.Sprintf("github %d: %s", e.StatusCode, e.Message) }
func IsNotFound(err error) bool {
	e, ok := err.(*APIError)
	return ok && e.StatusCode == http.StatusNotFound
}

// IsSecurityFeatureUnavailable returns true when GitHub is telling us that a
// repository security feature is not enabled or not available for the repo.
// These are expected per-repository conditions, not monitoring failures.
// Permission failures such as "Resource not accessible by personal access token"
// deliberately return false so they remain visible to the operator.
func IsSecurityFeatureUnavailable(err error) bool {
	e, ok := err.(*APIError)
	if !ok {
		return false
	}
	if e.StatusCode == http.StatusNotFound {
		return true
	}
	if e.StatusCode != http.StatusForbidden {
		return false
	}
	m := strings.ToLower(e.Message)
	return strings.Contains(m, "alerts are disabled for this repository") ||
		strings.Contains(m, "advanced security must be enabled") ||
		strings.Contains(m, "secret scanning is disabled") ||
		strings.Contains(m, "secret scanning is not enabled") ||
		strings.Contains(m, "code scanning is not enabled")
}

type Client struct {
	token string
	http  *http.Client
}

func New(token string, timeout time.Duration) *Client {
	return &Client{token: token, http: &http.Client{Timeout: timeout}}
}

func (c *Client) get(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "glance-github-status")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Message == "" {
			body.Message = resp.Status
		}
		return &APIError{StatusCode: resp.StatusCode, Message: body.Message}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type WorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HeadBranch   string    `json:"head_branch"`
}
type workflowRunsResponse struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

func (c *Client) WorkflowRuns(ctx context.Context, repo string, perPage int) ([]WorkflowRun, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs?per_page=%d", repo, perPage)
	var out workflowRunsResponse
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return out.WorkflowRuns, nil
}

type SearchItem struct {
	Number        int       `json:"number"`
	Title         string    `json:"title"`
	HTMLURL       string    `json:"html_url"`
	State         string    `json:"state"`
	RepositoryURL string    `json:"repository_url"`
	PullRequest   *struct{} `json:"pull_request"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type searchResponse struct {
	TotalCount int          `json:"total_count"`
	Items      []SearchItem `json:"items"`
}

func (c *Client) SearchIssues(ctx context.Context, query string, perPage int) ([]SearchItem, int, error) {
	endpoint := "https://api.github.com/search/issues?q=" + url.QueryEscape(query) + fmt.Sprintf("&sort=updated&order=desc&per_page=%d", perPage)
	var out searchResponse
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.TotalCount, nil
}

// DependabotAlert is the subset of GitHub's Dependabot alert schema used by the widget.
type DependabotAlert struct {
	Number     int       `json:"number"`
	State      string    `json:"state"`
	HTMLURL    string    `json:"html_url"`
	CreatedAt  time.Time `json:"created_at"`
	Dependency struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		ManifestPath string `json:"manifest_path"`
	} `json:"dependency"`
	SecurityAdvisory struct {
		GHSAID   string `json:"ghsa_id"`
		CVEID    string `json:"cve_id"`
		Summary  string `json:"summary"`
		Severity string `json:"severity"`
	} `json:"security_advisory"`
}

func (c *Client) DependabotAlerts(ctx context.Context, repo string) ([]DependabotAlert, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/dependabot/alerts?state=open&per_page=100", repo)
	var out []DependabotAlert
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type CodeScanningAlert struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	Rule      struct {
		ID                    string `json:"id"`
		Name                  string `json:"name"`
		Description           string `json:"description"`
		SecuritySeverityLevel string `json:"security_severity_level"`
		Severity              string `json:"severity"`
	} `json:"rule"`
}

func (c *Client) CodeScanningAlerts(ctx context.Context, repo string) ([]CodeScanningAlert, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/code-scanning/alerts?state=open&per_page=100", repo)
	var out []CodeScanningAlert
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type SecretScanningAlert struct {
	Number                int       `json:"number"`
	State                 string    `json:"state"`
	HTMLURL               string    `json:"html_url"`
	SecretType            string    `json:"secret_type"`
	SecretTypeDisplayName string    `json:"secret_type_display_name"`
	CreatedAt             time.Time `json:"created_at"`
}

func (c *Client) SecretScanningAlerts(ctx context.Context, repo string) ([]SecretScanningAlert, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/secret-scanning/alerts?state=open&per_page=100", repo)
	var out []SecretScanningAlert
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func RepoNameFromAPIURL(s string) string {
	const p = "https://api.github.com/repos/"
	if strings.HasPrefix(s, p) {
		return strings.TrimPrefix(s, p)
	}
	return s
}
