package github

import (
	"net/http"
	"testing"
)

func TestIsSecurityFeatureUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"not found", &APIError{StatusCode: http.StatusNotFound, Message: "Not Found"}, true},
		{"dependabot disabled", &APIError{StatusCode: http.StatusForbidden, Message: "Dependabot alerts are disabled for this repository."}, true},
		{"advanced security disabled", &APIError{StatusCode: http.StatusForbidden, Message: "Advanced Security must be enabled for this repository to use code scanning."}, true},
		{"pat permission failure", &APIError{StatusCode: http.StatusForbidden, Message: "Resource not accessible by personal access token"}, false},
		{"other forbidden", &APIError{StatusCode: http.StatusForbidden, Message: "Forbidden"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSecurityFeatureUnavailable(tt.err); got != tt.want {
				t.Fatalf("IsSecurityFeatureUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}
