# glance-github-status

A small, read-only GitHub attention/status service for [Glance](https://github.com/glanceapp/glance).

It is designed for people who want a compact view of GitHub work that needs attention without turning Glance into a general activity feed.

The service exposes:

- `/health` - refresh health
- `/status` - structured JSON
- `/widget` - Glance extension HTML

The widget can show:

- failed GitHub Actions runs for selected repositories
- open pull requests requesting your review
- your open pull requests
- open issues assigned to you
- recent GitHub Actions status for selected repositories

## Features

- Read-only GitHub REST API access
- Fine-grained PAT support
- Multiple GitHub resource owners/accounts
- Explicit repository list to avoid noise
- De-duplicates PRs and issues visible through multiple tokens
- Linux amd64 and arm64 release binaries
- systemd service
- No GitHub CLI dependency

# Installation

The recommended installation method is to use a prebuilt binary from GitHub Releases.

## Requirements

For the native installation:

- Linux
- systemd
- Glance
- `curl`

No Go installation is required when using a release binary.

## 1. Download the latest release

Check your system architecture:

```bash
uname -m
```

Typical values are:

```text
x86_64   -> amd64
aarch64  -> arm64
```

### x86-64 / amd64

```bash
curl -L \
  -o github-status \
  https://github.com/craigchandler/glance-github-status/releases/latest/download/github-status-linux-amd64
```

### ARM64 / aarch64

```bash
curl -L \
  -o github-status \
  https://github.com/craigchandler/glance-github-status/releases/latest/download/github-status-linux-arm64
```

Install the binary:

```bash
sudo install -m 0755 github-status /usr/local/bin/github-status
rm github-status
```

Verify that it is installed:

```bash
github-status --version
```

## 2. Install the environment configuration

Download the default environment file:

```bash
sudo curl -L \
  -o /etc/github-status.env \
  https://raw.githubusercontent.com/craigchandler/glance-github-status/main/.env.example
```

Protect the file:

```bash
sudo chmod 600 /etc/github-status.env
```

Edit it:

```bash
sudo nano /etc/github-status.env
```

For a single GitHub resource owner, the default configuration is:

```dotenv
GITHUB_TOKEN=replace_me
LISTEN_ADDR=127.0.0.1:8794
REFRESH_INTERVAL=5m
HTTP_TIMEOUT=15s
CONFIG_FILE=/etc/github-status.json
```

Replace `replace_me` with a fine-grained GitHub personal access token. Do not commit the token or paste it into `github-status.json`.

## 3. Install the repository configuration

Download the example configuration:

```bash
sudo curl -L \
  -o /etc/github-status.json \
  https://raw.githubusercontent.com/craigchandler/glance-github-status/main/config.example.json
```

Edit it:

```bash
sudo nano /etc/github-status.json
```

A minimal one-account configuration is:

```json
{
  "username": "octocat",
  "accounts": {
    "default": {
      "tokenEnv": "GITHUB_TOKEN"
    }
  },
  "repositories": [
    {
      "name": "octocat/example",
      "account": "default"
    }
  ],
  "maxItems": 8,
  "recentRunsPerRepo": 3
}
```

Replace `octocat` and `octocat/example` with your GitHub username and repositories.

## 4. Install the systemd service

Create the dedicated service user:

```bash
sudo useradd --system \
  --home /var/lib/github-status \
  --shell /usr/bin/nologin \
  githubstatus 2>/dev/null || true
```

Create its state directory:

```bash
sudo install -d -o githubstatus -g githubstatus -m 0755 /var/lib/github-status
```

Download the systemd unit:

```bash
sudo curl -L \
  -o /etc/systemd/system/github-status.service \
  https://raw.githubusercontent.com/craigchandler/glance-github-status/main/systemd/github-status.service
```

Reload systemd:

```bash
sudo systemctl daemon-reload
```

Enable and start the service:

```bash
sudo systemctl enable --now github-status
```

Check its status:

```bash
systemctl status github-status
```

## 5. Verify the API

Health check:

```bash
curl http://127.0.0.1:8794/health
```

Retrieve the structured status:

```bash
curl http://127.0.0.1:8794/status
```

For formatted JSON, if `jq` is installed:

```bash
curl -s http://127.0.0.1:8794/status | jq
```

---

# Add the widget to Glance

## 1. Download the widget

If your Glance configuration is stored in `/etc/glance`:

```bash
sudo curl -L \
  -o /etc/glance/github-status.yml \
  https://raw.githubusercontent.com/craigchandler/glance-github-status/main/glance/github-status.yml
```

## 2. Include it in your Glance configuration

Add the widget to the desired column in `glance.yml`:

```yaml
columns:
  - size: small
    widgets:
      - $include: github-status.yml
```

You can place it in any Glance column.

Restart Glance:

```bash
sudo systemctl restart glance
```

The GitHub status widget should now appear on your dashboard.

## Authentication

Use a **fine-grained personal access token** with the least access required for the repositories you monitor.

For the features in this project, grant read-only access as applicable:

- **Actions: Read** - workflow runs
- **Pull requests: Read** - pull request visibility/search
- **Issues: Read** - assigned issues/search
- **Metadata: Read** - normally granted automatically

Limit each token to only the repositories the widget needs.

Never put a token in `github-status.json` and never commit a token to the repository. Tokens belong in `/etc/github-status.env` or another protected environment file.

## Multiple personal/company/organization accounts

A GitHub fine-grained PAT has a single resource owner. If you monitor repositories owned by different users or organizations, define multiple named account profiles.

For example:

```dotenv
GITHUB_TOKEN_PERSONAL=replace_me
GITHUB_TOKEN_WORK=replace_me
LISTEN_ADDR=127.0.0.1:8794
REFRESH_INTERVAL=5m
HTTP_TIMEOUT=15s
CONFIG_FILE=/etc/github-status.json
```

Then map repositories to those profiles:

```json
{
  "username": "octocat",
  "accounts": {
    "personal": {
      "tokenEnv": "GITHUB_TOKEN_PERSONAL"
    },
    "work": {
      "tokenEnv": "GITHUB_TOKEN_WORK"
    }
  },
  "repositories": [
    {
      "name": "octocat/personal-project",
      "account": "personal"
    },
    {
      "name": "example-company/private-project",
      "account": "work"
    }
  ],
  "maxItems": 8,
  "recentRunsPerRepo": 3
}
```

The profile names are arbitrary. You can use names such as `default`, `personal`, `work`, `client-a`, or `opensource`.

Workflow requests use the token assigned to each repository. PR review requests, authored PRs, and assigned-issue searches are performed through every configured account and de-duplicated by URL. This allows one widget to combine visibility from several GitHub resource owners.

If an organization requires approval for fine-grained PATs, its token may need organization approval before private repositories are visible.

## Glance configuration

Add the extension to your Glance page:

```yaml
- type: extension
  url: http://127.0.0.1:8794/widget
  cache: 5m
  allow-potentially-dangerous-html: true
```

## Test the service

```bash
curl http://127.0.0.1:8794/health | jq
curl http://127.0.0.1:8794/status | jq
curl http://127.0.0.1:8794/widget
```

A healthy `/health` response should report `"ok": true`.

## Configuration reference

### `username`

Your GitHub username. Used for:

- `review-requested:<username>`
- `author:<username>`
- `assignee:<username>`

### `accounts`

A map of arbitrary account/profile names to environment variables containing GitHub tokens.

```json
"accounts": {
  "default": {
    "tokenEnv": "GITHUB_TOKEN"
  }
}
```

The named environment variable must exist when the service starts.

### `repositories`

Repositories whose GitHub Actions runs should be shown.

```json
"repositories": [
  {
    "name": "owner/repository",
    "account": "default"
  }
]
```

`account` must match a key under `accounts`.

### `maxItems`

Maximum number of entries retained for each displayed list. Default: `8`.

### `recentRunsPerRepo`

Number of workflow runs requested from each configured repository before the combined recent-run list is sorted and limited. Default: `3`.

## Environment reference

| Variable | Default | Description |
| --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8794` | HTTP listen address |
| `REFRESH_INTERVAL` | `5m` | GitHub refresh interval |
| `HTTP_TIMEOUT` | `15s` | Per-request HTTP timeout |
| `CONFIG_FILE` | `/etc/github-status.json` | JSON configuration path |
| token variables | none | Named by each account's `tokenEnv` |

## Build from source

Requires Go 1.22 or later.

```bash
git clone https://github.com/craigchandler/glance-github-status.git
cd glance-github-status
go test ./...
go vet ./...
make build
```

Install the locally built binary and service files:

```bash
sudo ./scripts/install-systemd.sh ./github-status
```

Then edit:

```text
/etc/github-status.env
/etc/github-status.json
```

and start the service:

```bash
sudo systemctl enable --now github-status
```

## Publishing releases

Pushing a tag matching `v*` runs `.github/workflows/release.yml`.

For example:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow tests the code and publishes standalone binaries plus SHA-256 checksum files for:

- `github-status-linux-amd64`
- `github-status-linux-arm64`

## Upgrade

Download the latest binary for your architecture using the same command from the installation section, then replace the installed binary and restart the service.

For x86-64 / amd64:

```bash
curl -L \
  -o github-status \
  https://github.com/craigchandler/glance-github-status/releases/latest/download/github-status-linux-amd64

sudo install -m 0755 github-status /usr/local/bin/github-status
rm github-status
sudo systemctl restart github-status
```

For ARM64, use `github-status-linux-arm64` instead. Existing `/etc/github-status.env` and `/etc/github-status.json` files are not changed.

## Security

The helper is intentionally read-only. It performs GET requests to GitHub's REST API and does not create, edit, merge, re-run, or delete GitHub resources.

Recommended deployment controls:

- use fine-grained PATs
- grant only read permissions
- restrict each token to required repositories
- keep `/etc/github-status.env` mode `0600`
- bind to `127.0.0.1` unless remote access is explicitly required
- do not expose `/status` publicly without understanding that it can contain repository names, PR/issue titles, and URLs

## Troubleshooting

Service logs:

```bash
journalctl -u github-status -n 100 --no-pager
```

Configuration errors are reported at startup, including missing token environment variables and repositories referencing undefined account profiles.

If data is only partially available, `/status` and the widget include a partial-data error while retaining successful results from other accounts/repositories.
