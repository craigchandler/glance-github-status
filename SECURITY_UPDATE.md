# GitHub security monitoring update

This update adds repository-scoped security monitoring to `glance-github-status`.

## Configuration

Add this block to `/etc/github-status.json`:

```json
"security": {
  "dependabot": true,
  "codeScanning": true,
  "secretScanning": true,
  "minimumSeverity": "medium"
}
```

Allowed minimum severities are `low`, `medium`, `high`, and `critical`.

Omit the `security` block to preserve the old behavior and make no security API calls.

## Fine-grained PAT permissions

For repositories where the checks are enabled, grant read-only repository permissions as applicable:

- Dependabot alerts: Read
- Code scanning alerts: Read
- Secret scanning alerts: Read

Secret scanning API access also requires the authenticated user to have sufficient repository/organization administration access according to GitHub's API rules.

A repository returning 404 for a security feature is treated as "feature unavailable/not enabled" and is not reported as an error. A 403 remains a partial-data error so missing token permissions are visible.

## Status output

`/status` now includes:

```json
{
  "securityAlerts": [
    {
      "type": "dependabot",
      "repo": "owner/repo",
      "title": "Example vulnerability",
      "severity": "high",
      "url": "https://github.com/...",
      "number": 1,
      "identifier": "GHSA-..."
    }
  ],
  "attention": [
    {
      "name": "repo",
      "status": "warning",
      "message": "Dependabot HIGH: Example vulnerability",
      "url": "https://github.com/..."
    }
  ],
  "counts": {
    "securityAlerts": 1,
    "dependabot": 1,
    "code-scanning": 0,
    "secret-scanning": 0
  }
}
```

Critical security findings use `critical` attention severity. Other included security findings use `warning`.

## Needs Attention integration

If you want to consume the GitHub `attention` array generically, use a `generic-json` source pointed at the GitHub status endpoint:

```json
{
  "name": "GitHub Security",
  "type": "generic-json",
  "url": "http://127.0.0.1:8794/status",
  "settings": {
    "rules": [
      {
        "forEach": "attention",
        "path": "status",
        "op": "truthy",
        "severityPath": "status",
        "message": "{{name}}: {{message}}"
      }
    ]
  }
}
```

If your existing `github-status` Needs Attention adapter already handles GitHub failures/reviews/issues, do not add both unless you are happy with duplicate non-security alerts. A later refinement can make the typed adapter consume `securityAlerts` directly.

## Replace files

The files changed by this update are:

- `internal/github/client.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `config.example.json`

`cmd/github-status/main.go` is included in the patch bundle for a complete buildable source tree, but its behavior is unchanged.
