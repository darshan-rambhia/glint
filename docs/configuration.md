# Configuration

Glint supports two configuration methods: a **YAML config file** (recommended for multi-instance setups) and **environment variables** (convenient for single-instance deployments).

## Config File

Pass a config file with `--config`:

```bash
glint --config /etc/glint/glint.yml
```

---

## Full Reference

### Server Settings

```yaml
listen: ":3800"           # (1)!
db_path: "/data/glint.db" # (2)!
log_level: "info"         # (3)!
log_format: "text"        # (4)!
history_hours: 48         # (5)!
worker_pool_size: 4       # (6)!
```

1. Address and port to bind the HTTP server. Default: `:3800`
2. Path to the SQLite database file. Must be writable. Default: `glint.db`
3. Log verbosity: `debug`, `info`, `warn`, `error`. Default: `info`
4. Log output format: `text` (human-readable) or `json` (structured). Default: `text`
5. Hours of metric history to retain for sparkline charts. Default: `48`
6. Maximum concurrent API calls across all collectors. Default: `4`

### PVE Instances

At least one PVE instance is required.

```yaml
pve:
  - name: "main"                    # (1)!
    host: "https://pve:8006"        # (2)!
    token_id: "glint@pam!monitor"   # (3)!
    token_secret: "xxx-xxx-xxx"     # (4)!
    insecure: true                  # (5)!
    poll_interval: "15s"            # (6)!
    disk_poll_interval: "1h"        # (7)!
    ssh:                            # (8)!
      host: "192.168.1.215"
      user: "root"
      key_path: "/config/ssh/id_ed25519"
```

1. Unique label for this instance. Used in UI and alerts.
2. PVE API endpoint URL (include port).
3. API token in `user@realm!tokenname` format.
4. Token secret from `pveum user token add`.
5. Skip TLS certificate verification. Set `true` for self-signed certs. Default: `false`
6. How often to poll node and guest metrics. Default: `15s`
7. How often to poll S.M.A.R.T. disk data (slow operation). Default: `1h`
8. Optional SSH connection for CPU temperature monitoring.

### PBS Instances

PBS monitoring is optional.

```yaml
pbs:
  - name: "main-pbs"                 # (1)!
    host: "https://pbs:8007"          # (2)!
    token_id: "glint@pbs!monitor"     # (3)!
    token_secret: "xxx-xxx-xxx"       # (4)!
    insecure: true                    # (5)!
    datastores: ["homelab"]           # (6)!
    poll_interval: "5m"               # (7)!
```

1. Unique label for this PBS instance.
2. PBS API endpoint URL (include port).
3. API token in `user@realm!tokenname` format.
4. Token secret from `proxmox-backup-manager user generate-token`.
5. Skip TLS certificate verification. Default: `false`
6. Datastores to monitor. Empty list = monitor all discovered datastores.
7. How often to poll backup data. Default: `5m`

### Notifications

```yaml
notifications:
  - type: ntfy                        # (1)!
    url: "http://ntfy:8080"           # (2)!
    topic: "homelab-alerts"           # (3)!

  - type: webhook                     # (4)!
    url: "https://hooks.example.com/glint"
    method: "POST"                    # (5)!
    headers:                          # (6)!
      Authorization: "Bearer xxx"
```

1. Provider type: `ntfy` or `webhook`.
2. ntfy server URL.
3. ntfy topic name.
4. Generic webhook --- POSTs the full alert as JSON.
5. HTTP method: `POST` (default) or `PUT`.
6. Custom headers for authentication.

### Alert Rules

All alert rules are optional. Defaults are applied if omitted.

```yaml
alerts:
  node_cpu_high:
    threshold: 90           # Percent CPU usage
    duration: "5m"          # Must sustain for this long
    severity: "warning"

  guest_down:
    grace_period: "2m"      # Ignore brief restarts
    severity: "critical"

  backup_stale:
    max_age: "36h"          # Alert if last backup older than this
    severity: "warning"

  disk_smart_failed:
    severity: "critical"

  datastore_full:
    threshold: 85           # Percent datastore usage
    severity: "warning"
```

| Rule | Default Threshold | Default Severity | Description |
|------|-------------------|------------------|-------------|
| `node_cpu_high` | 90% for 5m | warning | Sustained high CPU |
| `guest_down` | 2m grace | critical | Guest not running |
| `backup_stale` | 36h | warning | No recent backup |
| `disk_smart_failed` | --- | critical | Manufacturer SMART failure |
| `datastore_full` | 85% | warning | PBS datastore near capacity |

---

## Environment Variables

For a single PVE + PBS instance, you can skip the config file entirely:

### PVE

| Variable | Description | Required |
|----------|-------------|----------|
| `GLINT_PVE_URL` | PVE API endpoint (e.g., `https://pve:8006`) | Yes |
| `GLINT_PVE_TOKEN_ID` | API token ID (e.g., `glint@pam!monitor`) | Yes |
| `GLINT_PVE_TOKEN_SECRET` | API token secret | Yes |
| `GLINT_PVE_INSECURE` | Skip TLS verification (`true`/`false`) | No |

### PBS

| Variable | Description | Required |
|----------|-------------|----------|
| `GLINT_PBS_URL` | PBS API endpoint (e.g., `https://pbs:8007`) | No |
| `GLINT_PBS_TOKEN_ID` | API token ID | With PBS URL |
| `GLINT_PBS_TOKEN_SECRET` | API token secret | With PBS URL |
| `GLINT_PBS_DATASTORE` | Datastore name to monitor | No |
| `GLINT_PBS_INSECURE` | Skip TLS verification | No |

### Notifications

| Variable | Description | Required |
|----------|-------------|----------|
| `GLINT_NTFY_URL` | ntfy server URL | No |
| `GLINT_NTFY_TOPIC` | ntfy topic name | With ntfy URL |

### Server

| Variable | Description | Default |
|----------|-------------|---------|
| `GLINT_LISTEN` | Bind address | `:3800` |
| `GLINT_DB_PATH` | SQLite database path | `glint.db` |
| `GLINT_LOG_LEVEL` | Log level | `info` |
| `GLINT_LOG_FORMAT` | Log format (`text` or `json`) | `text` |

!!! info "Config file takes precedence"
    When both a config file and environment variables are set, the config file values take precedence. Environment variables are only used to build a default single-instance config when no config file is provided.

---

## Cluster Setup

### Multi-Node PVE Cluster

Point Glint at **any one node** in your cluster. It auto-discovers all nodes via the `/nodes` endpoint and deduplicates guests by cluster ID.

```yaml
pve:
  - name: "prod-cluster"
    host: "https://ANY_CLUSTER_NODE:8006"
    token_id: "glint@pam!monitor"
    token_secret: "xxx"
    insecure: true
```

### Multiple Independent PVE Instances

For separate (non-clustered) Proxmox hosts, add multiple entries. Each needs its own API token.

```yaml
pve:
  - name: "homelab"
    host: "https://192.168.1.215:8006"
    token_id: "glint@pam!monitor"
    token_secret: "xxx"
    insecure: true

  - name: "remote"
    host: "https://10.0.0.50:8006"
    token_id: "glint@pam!monitor"
    token_secret: "yyy"
    insecure: true
```

---

## Version Info

Glint embeds build metadata in the binary:

```bash
glint --version
```

```
glint v0.1.0
  commit:    abc1234def5678 (clean)
  built:     2026-02-16T12:00:00Z
  go:        go1.26
  platform:  linux/amd64
```

At startup, the same info is logged:

```
level=INFO msg="starting glint" version=v0.1.0 commit=abc1234def5678 built=2026-02-16T12:00:00Z dirty=clean go=go1.26 listen=:3800
```

## Security Considerations

- **Tokens are read-only.** PVEAuditor and Audit roles cannot modify anything.
- **Use `insecure: true`** only for self-signed certificates. If you have proper TLS certs, set it to `false`.
- **Store tokens securely.** Use Docker secrets, environment variables from a secrets manager, or file-based secrets for production deployments.
- **Network isolation.** Glint only needs access to the PVE/PBS API ports (8006/8007). It does not need SSH access unless you enable temperature monitoring.
- **Revoke tokens** if compromised: `pveum user token remove glint@pam monitor`
