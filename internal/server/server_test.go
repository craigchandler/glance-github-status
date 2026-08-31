package server

import "testing"

func TestValidateConfig(t *testing.T) {
	cfg := Config{
		Username: "octocat",
		Accounts: map[string]AccountConfig{
			"default": {TokenEnv: "GITHUB_TOKEN"},
		},
		Repositories: []RepositoryConfig{{Name: "octocat/example", Account: "default"}},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

func TestValidateConfigRejectsUnknownAccount(t *testing.T) {
	cfg := Config{
		Username: "octocat",
		Accounts: map[string]AccountConfig{
			"default": {TokenEnv: "GITHUB_TOKEN"},
		},
		Repositories: []RepositoryConfig{{Name: "octocat/example", Account: "work"}},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("ValidateConfig() expected error")
	}
}
