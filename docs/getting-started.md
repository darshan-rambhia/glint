# Getting Started

This guide walks you through setting up Glint end-to-end: creating Proxmox API tokens, writing your config file, and deploying with Docker Compose.

## Prerequisites

Glint connects to Proxmox APIs using **API tokens** (not passwords). Tokens are scoped to specific permissions and can be revoked independently. Glint only needs **read-only access**.

| Integration | Role Required | Permissions |
|-------------|---------------|-------------|
| Proxmox VE  | `PVEAuditor`  | Read-only access to nodes, VMs, containers, disks, and SMART data |
| Proxmox Backup Server | `Audit` | Read-only access to datastores, snapshots, and tasks |

---

## 1. Create Proxmox API Tokens

### Proxmox VE (PVE)

SSH into your Proxmox VE host:

```bash
# Create a dedicated user in the PAM realm
pveum user add glint@pam --comment "Glint monitoring"

# Assign the built-in PVEAuditor role (read-only on all resources)
pveum aclmod / -user glint@pam -role PVEAuditor

# Create an API token (--privsep 0 inherits the user's role)
pveum user token add glint@pam monitor --privsep 0
```

!!! warning "Save the token secret"
    The token secret is only shown once. Copy it immediately.

**PVEAuditor grants read-only access to:** node status and metrics, LXC/QEMU lists, disk list and SMART data, cluster status, node discovery. It **cannot** start/stop VMs, change configuration, or access the console.

Verify with:

```bash
curl -k -H "Authorization: PVEAPIToken=glint@pam!monitor=YOUR_SECRET" \
  https://YOUR_PVE_IP:8006/api2/json/nodes
```

### Proxmox Backup Server (PBS)

SSH into your PBS host:

```bash
# Create a dedicated user
proxmox-backup-manager user create glint@pbs --comment "Glint monitoring"

# Assign the Audit role (read-only)
proxmox-backup-manager acl update / Audit --auth-id glint@pbs

# Create an API token
proxmox-backup-manager user generate-token glint@pbs monitor
```

Verify with:

```bash
curl -k -H "Authorization: PBSAPIToken=glint@pbs!monitor:YOUR_SECRET" \
  https://YOUR_PBS_IP:8007/api2/json/status/datastore-usage
```

---

## 2. Write Your Config File

Create `glint.yml` from the example:

```bash
cp glint.example.yml glint.yml
```

Here is a complete annotated config:

```yaml
# Server
listen: ":3800"                # Address to bind the HTTP server
db_path: "/data/glint.db"      # SQLite database path (must be writable)
log_level: "info"              # debug, info, warn, error
log_format: "text"             # "text" for human-readable, "json" for structured
history_hours: 48              # How many hours of sparkline history to keep
worker_pool_size: 4            # Max concurrent API calls across all collectors

# Proxmox VE instances (at least one required)
pve:
  - name: "main"                                       # Unique label
    host: "https://192.168.1.215:8006"                  # PVE API URL
    token_id: "glint@pam!monitor"                       # user@realm!tokenname
    token_secret: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" # Token secret
    insecure: true                                      # Allow self-signed TLS certs
    poll_interval: "15s"                                # How often to poll metrics
    disk_poll_interval: "1h"                            # How often to poll SMART data

# Proxmox Backup Server instances (optional)
pbs:
  - name: "main-pbs"
    host: "https://10.100.1.102:8007"
    token_id: "glint@pbs!monitor"
    token_secret: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    insecure: true
    datastores: ["homelab"]     # Which datastores to monitor (empty = all)
    poll_interval: "5m"

# Notification targets (optional)
notifications:
  - type: ntfy
    url: "http://10.100.1.104:8080"
    topic: "homelab-alerts"

# Alert thresholds (optional --- defaults are sensible)
alerts:
  node_cpu_high:
    threshold: 90
    duration: "5m"
    severity: "warning"
  guest_down:
    grace_period: "2m"
    severity: "critical"
  backup_stale:
    max_age: "36h"
    severity: "warning"
  disk_smart_failed:
    severity: "critical"
  datastore_full:
    threshold: 85
    severity: "warning"
```

!!! tip "Environment variables"
    For a single PVE + PBS instance, you can skip the config file entirely and use environment variables. See the [Configuration](configuration.md) page for details.

---

## 3. Deploy with Docker Compose

Create a `docker-compose.yml` alongside your `glint.yml`:

```yaml
services:
  glint:
    image: ghcr.io/darshan-rambhia/glint:latest
    container_name: glint
    restart: unless-stopped
    ports:
      - "3800:3800"
    volumes:
      - glint-data:/data
      - ./glint.yml:/etc/glint/glint.yml:ro
    command: ["glint", "--config", "/etc/glint/glint.yml"]
    deploy:
      resources:
        limits:
          memory: 128M
        reservations:
          memory: 32M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  glint-data:
```

!!! note "Scratch base image"
    The Glint image uses a `scratch` base (no shell, no utilities), so in-container `HEALTHCHECK` is not available. Use `curl http://localhost:3800/healthz` from the host or configure health checks in your reverse proxy.

Start it:

```bash
docker compose up -d

# Check logs
docker compose logs -f glint

# Verify health
curl http://localhost:3800/healthz
```

---

## 4. Verify the Installation

After starting Glint:

1. **Health check:** `curl http://localhost:3800/healthz`
2. **Dashboard:** Open `http://localhost:3800` in your browser
3. **Logs:** Check for collector startup messages --- you should see `PVE collection complete` within 15 seconds

---

## 5. Troubleshooting

| Issue | Solution |
|-------|----------|
| `401 Unauthorized` | Token ID or secret is wrong. Format: `user@realm!tokenname` |
| `403 Forbidden` | Token lacks permissions. Re-run `pveum aclmod / -user glint@pam -role PVEAuditor` |
| `Connection refused` | PVE/PBS host unreachable. Check network, firewall, and port (8006/8007) |
| `TLS handshake error` | Set `insecure: true` for self-signed certificates |
| `No nodes found` | Token may not have permissions on `/nodes`. Re-assign PVEAuditor on `/` |
| `No disks found` | SMART data is polled hourly. Wait up to 1 hour for initial disk data |
| `No data on dashboard` | Check logs for collector errors. Verify API tokens with `curl` (see step 1) |
| Container won't start | Check `docker compose logs glint`. Common: invalid YAML, missing required fields |

---

## Next Steps

- [Configuration](configuration.md) --- full reference for all YAML options and environment variables
- [Deployment](deployment.md) --- Podman, Quadlet, and systemd bare metal options
- [Logging](logging.md) --- structured logging for systemd/journald
