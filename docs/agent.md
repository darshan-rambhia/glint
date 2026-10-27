# Host Agent

`glint agent` reports package-update and reboot state from Debian and Ubuntu hosts that Glint cannot poll through the Proxmox API: the Proxmox host's own packages, a VPS, an edge box. The agent pushes a signed report to Glint, and Glint shows it in the **Hosts** section, adds it to `/api/widget`, and alerts on it.

The agent is the same `glint` binary. It only reads: it never runs apt for real, and it needs no root.

## What a report contains

| Field | Source |
|-------|--------|
| Pending and security updates, with the package list | `apt-get -s dist-upgrade` (simulation). A package counts as security when its origin includes a `-security` pocket. |
| apt lists age | Newest file in `/var/lib/apt/lists` |
| Last unattended-upgrades run | `/var/lib/apt/periodic/unattended-upgrades-stamp` |
| Reboot required, since when, and why | `/run/reboot-required` and `/run/reboot-required.pkgs` |
| Running and newest installed kernel | `/proc/sys/kernel/osrelease` and `/boot/vmlinuz-*` |
| OS, hostname, uptime | `/etc/os-release`, `/proc` |

A step that fails (for example apt missing) is listed in the report's `errors` field and shown as a tooltip on the host's status chip. The rest of the report still arrives.

The package list is capped at 300 entries, security updates first. Counts are never capped.

## Server configuration

Register each agent with its own secret:

```yaml
agents:
  - name: pve-host
    secret: "${GLINT_AGENT_PVE_SECRET}"
  - name: edge
    secret: "${GLINT_AGENT_EDGE_SECRET}"
    interval: 1h          # how often this agent reports (default 1h)
```

Secrets must be at least 32 characters. Generate one with `openssl rand -hex 32`. Each configured agent appears in the Hosts section right away, as "No data" until its first report.

The ingest endpoint, `POST /api/v1/agents/report`, is only registered when at least one agent is configured.

### Alerts

```yaml
alerts:
  agent_stale:
    missed_reports: 2     # intervals without a report
    severity: "warning"
  security_updates_pending:
    max_age: "72h"        # security updates pending this long
    severity: "warning"
  reboot_required:
    max_age: "48h"        # reboot-required flag set this long
    severity: "warning"
```

| Rule | Default | Fires when |
|------|---------|------------|
| `agent_stale` | 2 missed reports | No report for `missed_reports` x interval. An agent that has never reported is measured from Glint's start. |
| `security_updates_pending` | 72h | Security updates have been pending continuously since a report this long ago. Glint tracks the start time and keeps it across restarts. |
| `reboot_required` | 48h | `/run/reboot-required` is older than this. |

Host alerts repeat at most once every 24 hours, except `agent_stale`, which repeats every 6 hours.

## Running the agent

```bash
glint agent --server https://glint.example.com --name edge --secret-file /etc/glint-agent/secret
```

| Flag | Env | Default |
|------|-----|---------|
| `--server` | `GLINT_AGENT_SERVER` | required. Glint base URL; the agent appends `/api/v1/agents/report`. |
| `--name` | `GLINT_AGENT_NAME` | hostname |
| `--secret-file` | `GLINT_AGENT_SECRET_FILE` | `$CREDENTIALS_DIRECTORY/glint-agent-secret` |
| `--interval` | | `0`: report once and exit |
| `--timeout` | | `30s` per request |
| `--dry-run` | | print the report as JSON instead of sending it |

A failed send is retried twice with backoff. A `4xx` response other than `429` is not retried. Exit code `1` means the report did not reach Glint, `2` means a usage or configuration error.

### systemd

Run it from a timer so no process stays resident. `LoadCredential` hands the secret to the unit without exposing it in the environment, and `DynamicUser` runs it as a throwaway unprivileged user.

```ini
# /etc/systemd/system/glint-agent.service
[Unit]
Description=Glint host agent
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/glint agent --server https://glint.example.com --name edge
LoadCredential=glint-agent-secret:/etc/glint-agent/secret
DynamicUser=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
NoNewPrivileges=yes
CapabilityBoundingSet=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
```

```ini
# /etc/systemd/system/glint-agent.timer
[Unit]
Description=Run the Glint host agent hourly

[Timer]
OnBootSec=2min
OnUnitActiveSec=1h
RandomizedDelaySec=5min

[Install]
WantedBy=timers.target
```

Keep `/etc/glint-agent/secret` at mode `0600`, owned by root.

## Request signing

Every request carries four headers:

| Header | Value |
|--------|-------|
| `X-Glint-Agent` | agent name |
| `X-Glint-Timestamp` | unix seconds |
| `X-Glint-Nonce` | random hex, at most 64 characters |
| `X-Glint-Signature` | hex HMAC-SHA256 with the agent's secret |

The signature covers this string, with `\n` separators:

```text
glint-agent-v1
POST
/api/v1/agents/report
<timestamp>
<nonce>
<hex sha256 of the body>
```

Glint rejects a request with `401` when the agent is unknown, the timestamp is more than 5 minutes off, the signature does not match, or the nonce was already used within the skew window. All four cases return the same response. A report whose `agent` field differs from the signing agent gets `400`, and a body over 256 KiB gets `413`.

Signing protects the report even when TLS ends at a reverse proxy in front of Glint, because nothing on the inner hops can change or replay it. Still publish the endpoint over HTTPS: the report lists the host's pending packages.

## Exposing the endpoint

Agents on the same network can post to Glint directly. For a host outside it, publish only the ingest path through your reverse proxy, for example with Traefik:

```yaml
http:
  routers:
    glint-ingest:
      rule: "Host(`glint-ingest.example.com`) && Path(`/api/v1/agents/report`) && Method(`POST`)"
      service: glint
```

Keep the rest of Glint behind authentication, as described in [Reverse Proxy](reverse-proxy.md).
