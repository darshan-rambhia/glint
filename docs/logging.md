# Logging

Glint uses Go's structured `slog` library. All log output goes to **stderr**.

## Log Format

Glint supports two output formats, configured via `log_format` in the config file or the `GLINT_LOG_FORMAT` environment variable.

=== "Text (default)"

    Human-readable key=value format. Best for local development and interactive debugging.

    ```
    time=2026-02-16T12:00:00.000Z level=INFO msg="starting glint" version=0.1.0 commit=abc1234 listen=:3800
    time=2026-02-16T12:00:00.015Z level=INFO msg="collector started" name=pve:main interval=15s
    time=2026-02-16T12:00:15.032Z level=DEBUG msg="PVE collection complete" instance=main nodes=1 guests=8
    ```

=== "JSON"

    Structured JSON, one object per line. Best for systemd/journald, Docker json-file driver, and log aggregators.

    ```json
    {"time":"2026-02-16T12:00:00.000Z","level":"INFO","msg":"starting glint","version":"0.1.0","commit":"abc1234","listen":":3800"}
    {"time":"2026-02-16T12:00:00.015Z","level":"INFO","msg":"collector started","name":"pve:main","interval":"15s"}
    ```

Set via config:

```yaml
log_format: "json"
```

Or environment variable:

```bash
export GLINT_LOG_FORMAT=json
```

---

## Log Levels

| Level | What it logs |
|-------|-------------|
| `error` | Failures that affect functionality (DB errors, API failures) |
| `warn`  | Degraded behavior (alerts fired, cluster detection fallback) |
| `info`  | Lifecycle events (startup, shutdown, prune results) |
| `debug` | Per-poll collection details, HTTP request logs |

Set via config (`log_level: "debug"`) or env var (`GLINT_LOG_LEVEL=debug`).

!!! tip "Production recommendation"
    Use `log_level: "info"` and `log_format: "json"` for production. This gives you structured logs that are easy to filter while keeping volume manageable.

---

## Docker Logging

Docker captures container stdout/stderr automatically. The `json-file` logging driver (default) stores logs on disk. The recommended `docker-compose.yml` includes rotation settings:

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"
    max-file: "3"
```

### Viewing Logs

```bash
# Follow logs
docker compose logs -f glint

# Filter by level (requires json log format)
docker compose logs glint | jq 'select(.level == "ERROR")'

# Show last 100 lines
docker compose logs --tail=100 glint
```

---

## systemd / journald

When running as a systemd service (bare metal or Podman Quadlet), set `log_format: "json"` for best integration. systemd captures stderr and stores it in the journal.

### Viewing Logs

```bash
# Follow logs
journalctl -u glint -f

# View logs since last boot
journalctl -u glint -b

# Filter by priority (requires json format for structured fields)
journalctl -u glint --priority=err

# Export as JSON
journalctl -u glint -o json

# Show logs from last hour
journalctl -u glint --since="1 hour ago"
```

### Recommended systemd Config

```yaml
log_level: "info"
log_format: "json"
```

This gives you structured logs that journald can index and filter, while keeping the volume manageable.

---

## Log Fields

All log entries include these base fields:

| Field | Description | Example |
|-------|-------------|---------|
| `time` | ISO 8601 timestamp | `2026-02-16T12:00:00.000Z` |
| `level` | Log level | `INFO`, `ERROR` |
| `msg` | Human-readable message | `"PVE collection complete"` |

Collector logs include additional context:

| Field | Description | Example |
|-------|-------------|---------|
| `instance` | PVE/PBS instance name | `"main"` |
| `name` | Collector identifier | `"pve:main"` |
| `interval` | Poll interval | `"15s"` |
| `nodes` | Number of nodes polled | `1` |
| `guests` | Number of guests found | `8` |
| `duration` | Collection duration | `"1.234s"` |

Alert logs include:

| Field | Description | Example |
|-------|-------------|---------|
| `alert_type` | Rule that fired | `"node_cpu_high"` |
| `severity` | Alert severity | `"warning"` |
| `subject` | What triggered it | `"main/pve"` |
