package server

import (
	"testing"
	"time"

	gh "github.com/craigchandler/glance-github-status/internal/github"
)

func TestValidateConfig(t *testing.T) {
	cfg := Config{Username: "octocat", Accounts: map[string]AccountConfig{"default": {TokenEnv: "GITHUB_TOKEN"}}, Repositories: []RepositoryConfig{{Name: "octocat/example", Account: "default"}}, Security: &SecurityConfig{Dependabot: true, MinimumSeverity: "medium"}}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}
func TestValidateConfigRejectsUnknownAccount(t *testing.T) {
	cfg := Config{Username: "octocat", Accounts: map[string]AccountConfig{"default": {TokenEnv: "GITHUB_TOKEN"}}, Repositories: []RepositoryConfig{{Name: "octocat/example", Account: "work"}}}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("ValidateConfig() expected error")
	}
}
func TestValidateConfigRejectsBadSeverity(t *testing.T) {
	cfg := Config{Username: "octocat", Accounts: map[string]AccountConfig{"default": {TokenEnv: "GITHUB_TOKEN"}}, Repositories: []RepositoryConfig{{Name: "octocat/example", Account: "default"}}, Security: &SecurityConfig{Dependabot: true, MinimumSeverity: "severe"}}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected severity validation error")
	}
}
func TestSeverityRank(t *testing.T) {
	if severityRank("critical") <= severityRank("high") {
		t.Fatal("critical should outrank high")
	}
	if normalizeCodeSeverity("warning") != "medium" {
		t.Fatal("warning should map to medium")
	}
}

func TestCurrentFailedRunsIgnoresFailureSupersededBySuccess(t *testing.T) {
	then := time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC)
	runs := []gh.WorkflowRun{
		{Name: "Build", HeadBranch: "main", Status: "completed", Conclusion: "failure", UpdatedAt: then, HTMLURL: "https://example.test/failed"},
		{Name: "Build", HeadBranch: "main", Status: "completed", Conclusion: "success", UpdatedAt: then.Add(time.Minute), HTMLURL: "https://example.test/success"},
	}

	if got := currentFailedRuns("owner/repo", runs); len(got) != 0 {
		t.Fatalf("currentFailedRuns() = %#v, want no failed runs", got)
	}
}

func TestCurrentFailedRunsKeepsIndependentFailures(t *testing.T) {
	then := time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC)
	runs := []gh.WorkflowRun{
		{Name: "Build", HeadBranch: "main", Status: "completed", Conclusion: "failure", UpdatedAt: then, HTMLURL: "https://example.test/main-failed"},
		{Name: "Build", HeadBranch: "main", Status: "completed", Conclusion: "success", UpdatedAt: then.Add(time.Minute), HTMLURL: "https://example.test/main-success"},
		{Name: "Build", HeadBranch: "release", Status: "completed", Conclusion: "failure", UpdatedAt: then.Add(2 * time.Minute), HTMLURL: "https://example.test/release-failed"},
		{Name: "Lint", HeadBranch: "main", Status: "completed", Conclusion: "cancelled", UpdatedAt: then.Add(3 * time.Minute), HTMLURL: "https://example.test/lint-cancelled"},
	}

	got := currentFailedRuns("owner/repo", runs)
	if len(got) != 2 {
		t.Fatalf("currentFailedRuns() returned %d runs, want 2: %#v", len(got), got)
	}
	if got[0].Name != "Lint" || got[0].Conclusion != "cancelled" || got[1].URL != "https://example.test/release-failed" {
		t.Fatalf("currentFailedRuns() = %#v, want current Lint and release failures", got)
	}
}
