# Glint

Lightweight Proxmox monitoring dashboard. Single binary, ~10MB, ~30-50MB RAM.

**Go + templ + htmx** --- server-rendered HTML with 15-second live polling. SQLite for history. No JavaScript build step.

## Features

- **Node monitoring** --- CPU, memory, swap, root filesystem, load average, I/O wait, uptime
- **Guest monitoring** --- LXC containers and QEMU VMs with status, CPU, memory, disk, network
- **PBS backup tracking** --- datastore usage, backup snapshots, task history, stale backup detection
- **S.M.A.R.T. disk health** --- ATA and NVMe attribute parsing with Backblaze-derived failure rate thresholds
- **Alerting** --- ntfy and webhook notifications with configurable rules and deduplication
- **Multi-node ready** --- supports multiple PVE instances, clusters, and PBS servers
- **Temperature monitoring** --- optional SSH-based CPU temperature polling

## Quick Start

You need a Proxmox VE API token before starting. If you don't have one yet, follow the [Getting Started](getting-started.md) guide first --- it takes about 5 minutes.

**1. Create a config file** called `glint.yml`:

```yaml
listen: ":3800"
db_path: "/data/glint.db"

pve:
  - name: "main"
    host: "https://YOUR_PROXMOX_IP:8006"        # (1)!
    token_id: "glint@pam!monitor"                # (2)!
    token_secret: "xxxxxxxx-xxxx-xxxx-xxxx-xxxx" # (3)!
    insecure: true                               # (4)!
```

1. Replace with your Proxmox server's IP address
2. The API token ID you created (format: `user@realm!tokenname`)
3. The token secret shown when you created the token
4. Set to `true` if your Proxmox uses a self-signed certificate (most do)

**2. Start Glint:**

=== "Docker Compose"

    Create a `docker-compose.yml` in the same folder as `glint.yml`:

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

    volumes:
      glint-data:
    ```

    ```bash
    docker compose up -d
    ```

=== "Docker Run"

    ```bash
    docker run -d -p 3800:3800 \
      -v glint-data:/data \
      -v ./glint.yml:/etc/glint/glint.yml:ro \
      ghcr.io/darshan-rambhia/glint:latest \
      glint --config /etc/glint/glint.yml
    ```

=== "Homebrew"

    ```bash
    brew install darshan-rambhia/tap/glint
    glint --config glint.yml
    ```

=== "Go Install"

    Requires [Go 1.26+](https://go.dev/dl/) and a C compiler (for SQLite):

    ```bash
    CGO_ENABLED=1 go install github.com/darshan-rambhia/glint/cmd/glint@latest
    glint --config glint.yml
    ```

=== "Binary Download"

    Download the latest release from [GitHub Releases](https://github.com/darshan-rambhia/glint/releases), then:

    ```bash
    glint --config glint.yml
    ```

    See the [Deployment](deployment.md) guide for full binary install and systemd service setup.

**3. Open** `http://localhost:3800` --- you should see your Proxmox node within 15 seconds.

!!! tip "Want PBS backup monitoring too?"
    Add a `pbs:` section to your config. See the full [Configuration](configuration.md) reference for all options including PBS, alerting, and notifications.

## Documentation

| Guide | Description |
|-------|-------------|
| [Getting Started](getting-started.md) | Create API tokens, write your config, deploy with Docker Compose |
| [Configuration](configuration.md) | Full config reference --- YAML options, environment variables, defaults |
| [Deployment](deployment.md) | Docker, Podman, Quadlet, systemd bare metal |
| [Logging](logging.md) | Log formats, levels, systemd/journald integration |
| [Architecture](architecture.md) | How Glint works --- collectors, cache, store, alerter |
| [Testing](testing.md) | Unit tests, benchmarks, fuzz tests, coverage, linting |

## Inspiration

Glint draws heavily from two excellent projects:

- **[Pulse](https://github.com/rambhia-dev/pulse)** --- Instance + node two-level hierarchy, per-instance collectors with a bounded worker pool, and snapshot-based caching. Pulse covered Proxmox host metrics and PBS backup monitoring well but lacked S.M.A.R.T. disk health tracking.
- **[Scrutiny](https://github.com/AnalogJ/scrutiny)** --- WWN-based disk identity (globally unique, survives reboots and cable changes), protocol-aware SMART parsing (ATA vs NVMe vs SCSI), and Backblaze-derived failure rate thresholds for real-world risk assessment beyond manufacturer pass/fail.

## License

[MIT](https://github.com/darshan-rambhia/glint/blob/main/LICENSE)
