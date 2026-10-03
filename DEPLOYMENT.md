# Deployment

my-usage is one static binary in a `scratch` image. It reads one YAML file per
account from a directory, polls each provider, and serves Prometheus metrics on
`:9090`.

## 1. Build the image

Every push to the default branch publishes the image to GitHub's registry as
`ghcr.io/bloomyindev/my-usage`, tagged `latest` and with the 7-character commit
sha. Pull requests only build it. A new ghcr package is private: the host
needs `docker login ghcr.io` with a token that has `read:packages`, or make the
package public in its settings.

To build it yourself instead:

```
docker build -t my-usage:latest .
```

## 2. Run it

```yaml
# compose.yaml
services:
  my-usage:
    image: ghcr.io/bloomyindev/my-usage:latest
    restart: unless-stopped
    environment:
      # MY_USAGE_POLL_INTERVAL: 5m
      MY_USAGE_LOG_FORMAT: json
    volumes:
      - ./accounts:/accounts
    # No `ports:`. Prometheus reaches it over a shared network by service name.
    networks: [monitoring]

networks:
  monitoring:
    external: true
```

The `/accounts` volume must be **read-write**: the tool rewrites an account
file when it refreshes a token. It holds refresh tokens, so protect it like a
secret and never expose it. The container runs as root, so a bind mount needs
no ownership setup.

## 3. Configuration

Environment variables only. An unset or empty variable keeps its default; a
malformed one stops the process at start and names the variable.

| Variable | Default | Meaning |
|---|---|---|
| `MY_USAGE_ACCOUNTS_DIR` | `/accounts` in the image | Directory of account files |
| `MY_USAGE_LISTEN` | `:9090` in the image | Address of `/metrics` and `/healthz` |
| `MY_USAGE_POLL_INTERVAL` | `5m` | Time between polls of one account |
| `MY_USAGE_RESCAN_INTERVAL` | `1m` | Safety rescan of the directory |
| `MY_USAGE_LOG_FORMAT` | `text` | `text` or `json` |
| `MY_USAGE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## 4. Add an account

Drop a `.yaml` file in `accounts/`; delete it to remove the account. No restart.
`name` becomes the `account` label, so never use an email. Two files with the
same `provider` and `name` conflict: the first in name order runs.

```yaml
# accounts/ollama-main.yaml
provider: ollama
name: main
credentials:
  api_key: ...
```

```yaml
# accounts/codex-free.yaml
provider: codex
name: free
credentials:
  access_token: ...
  refresh_token: ...
  account_id: ...        # optional
```

```yaml
# accounts/claude-perso.yaml
provider: anthropic
name: perso
credentials:
  access_token: ...
  refresh_token: ...
  expires_at: 2026-10-03T14:00:00Z
  subscription_type: pro   # optional, shown as the plan
```

Write the files with mode `0600`.

### Claude and Codex: give the tool its own session

Refresh tokens are revoked when two sessions reuse one. Never copy the
credentials of the CLI you use on your PC. Log in once with a separate profile
(a throwaway container or another config directory), copy the tokens into the
YAML file, then leave that CLI logged in without ever running it again. From
then on only my-usage refreshes the session.

The refresh endpoints and client ids are taken from the public CLIs and have
not been exercised against a live refresh yet. Check the first refresh in the
logs before relying on it.

## 5. Check it works

```
curl -s localhost:9090/metrics | grep '^llm_'
```

(from a container on the same network if the port is not published). Per
account, expect `llm_quota_scrape_success 1` and one
`llm_quota_utilization_ratio` per window. Logs name each account as it starts.

## 6. Prometheus

```yaml
scrape_configs:
  - job_name: my-usage
    static_configs:
      - targets: ["my-usage:9090"]
```

Alerts worth having:

```yaml
- alert: QuotaScrapeFailing
  expr: llm_quota_scrape_success == 0
  for: 15m
  annotations:
    summary: "{{ $labels.provider }}/{{ $labels.account }}: poll failing (dead token, changed endpoint, or session revoked)"

- alert: QuotaAccountFileInvalid
  expr: llm_quota_invalid_account_files > 0
  for: 5m
```

`scrape_success` drops to 0 on any failure and the quota series disappear
instead of showing stale values. A permanent failure (rejected key, revoked
refresh token) also stops polling that account until its file changes. In
Grafana, use instant queries for "current value" panels.

## 7. Updating

Rebuild the image and `docker compose up -d`. Account files are untouched.
