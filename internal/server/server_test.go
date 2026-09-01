package server

import "testing"

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
