# Dashboard Widget Integration

Glint exposes a **`GET /api/widget`** endpoint that returns a JSON summary of your Proxmox cluster. Any homepage-style dashboard can consume it without CORS issues — the request is made server-side by the dashboard, not by the browser.

---

## Response Reference

```json
{
  "nodes":   { "total": 3, "online": 3, "offline": 0 },
  "guests":  { "total": 25, "running": 22, "stopped": 3, "vms": 15, "lxc": 10 },
  "cpu":     { "usage_pct": 23.4 },
  "memory":  { "used_bytes": 68719476736, "total_bytes": 274877906944, "usage_pct": 25.0 },
  "disks":   { "total": 8, "passed": 7, "failed": 0, "warning": 1, "unknown": 0 },
  "backups": { "total": 42, "last_backup_time": 1740009600 }
}
```

| Field | Description |
|---|---|
| `nodes.total/online/offline` | PVE node counts by status |
| `guests.total/running/stopped` | Guest counts by status |
| `guests.vms/lxc` | Guest counts by type |
| `cpu.usage_pct` | Average CPU % across **online** nodes |
| `memory.used_bytes / total_bytes / usage_pct` | Summed memory across **online** nodes |
| `disks.passed/failed/warning/unknown` | Disk counts by SMART health category |
| `backups.total` | Total backup snapshot count across all PBS instances |
| `backups.last_backup_time` | Most recent backup as a Unix timestamp |

!!! note "Offline nodes"
    Offline nodes are counted but excluded from CPU and memory aggregates.

---

## Authentication

All dashboard tools listed here make requests **server-side**, so CORS is not a concern.

If Glint is behind a reverse proxy with authentication, you have two options:

**Option A** — Exempt `/api/widget` from auth (same pattern as `/healthz`):

=== "Caddy"

    ```
    @public path /healthz /api/widget
    handle @public {
        reverse_proxy localhost:3800
    }
    ```

=== "nginx"

    ```nginx
    location ~ ^/(healthz|api/widget)$ {
        proxy_pass http://127.0.0.1:3800;
        proxy_set_header Host $host;
    }
    ```

=== "Traefik"

    Add `/api/widget` to the unauthenticated router rule:

    ```yaml
    - "traefik.http.routers.glint-public.rule=Host(`glint.yourdomain.com`) && PathRegexp(`^/(healthz|api/widget)$`)"
    ```

**Option B** — Pass credentials in the dashboard widget config (see per-dashboard examples below).

---

## Homepage

[Homepage](https://gethomepage.dev) uses the built-in `customapi` widget type. Requests are made by the Next.js server process — no CORS configuration needed.

```yaml
# services.yaml
- Proxmox:
    icon: proxmox.svg
    href: http://glint:8080
    description: Proxmox VE cluster
    widget:
      type: customapi
      url: http://glint:8080/api/widget
      mappings:
        - field: nodes.online
          label: Nodes
          format: number
        - field: guests.running
          label: Running
          format: number
        - field: cpu.usage_pct
          label: CPU
          format: float
          suffix: "%"
        - field: memory.usage_pct
          label: Memory
          format: float
          suffix: "%"
        - field: backups.last_backup_time
          label: Last Backup
          format: relativeDate
```

**With basic auth on the reverse proxy:**

```yaml
    widget:
      type: customapi
      url: http://glint:8080/api/widget
      username: alice
      password: secret
```

**Useful format options:**

| Field | Recommended format |
|---|---|
| `nodes.online` | `number` |
| `guests.running` | `number` |
| `cpu.usage_pct` | `float` + `suffix: "%"` |
| `memory.usage_pct` | `float` + `suffix: "%"` |
| `memory.used_bytes` | `bytes` |
| `backups.last_backup_time` | `relativeDate` |
| `backups.total` | `number` |

---

## Glance

[Glance](https://github.com/glanceapp/glance) uses a `custom-api` widget with Go template rendering. You have full control over the HTML layout.

```yaml
# glance.yml
- type: custom-api
  title: Proxmox
  cache: 1m
  url: http://glint:8080/api/widget
  template: |
    <div class="flex gap-4 justify-around">
      <div class="flex flex-col items-center">
        <div class="widget-content-block-title">{{ .JSON.Int "nodes.online" }}/{{ .JSON.Int "nodes.total" }}</div>
        <div class="color-subdue text-sm">Nodes</div>
      </div>
      <div class="flex flex-col items-center">
        <div class="widget-content-block-title">{{ .JSON.Int "guests.running" }}</div>
        <div class="color-subdue text-sm">Running</div>
      </div>
      <div class="flex flex-col items-center">
        <div class="widget-content-block-title">{{ .JSON.Float "cpu.usage_pct" | printf "%.1f" }}%</div>
        <div class="color-subdue text-sm">CPU</div>
      </div>
      <div class="flex flex-col items-center">
        <div class="widget-content-block-title">{{ .JSON.Float "memory.usage_pct" | printf "%.1f" }}%</div>
        <div class="color-subdue text-sm">Memory</div>
      </div>
    </div>
    {{ if gt (.JSON.Int "disks.failed") 0 }}
    <p class="color-negative text-sm margin-top-10">
      ⚠ {{ .JSON.Int "disks.failed" }} disk(s) failed SMART
    </p>
    {{ end }}
```

**With basic auth on the reverse proxy:**

```yaml
  headers:
    Authorization: "Basic dXNlcjpwYXNz"   # base64("user:pass")
```

**Accessing fields:**

| Type | Accessor |
|---|---|
| Integer | `{{ .JSON.Int "nodes.online" }}` |
| Float | `{{ .JSON.Float "cpu.usage_pct" }}` |
| Unix timestamp | `{{ toRelativeTime (parseTime "Unix" (printf "%d" (.JSON.Int "backups.last_backup_time"))) }}` |

---

## Dashy

[Dashy](https://dashy.to) uses the `api-response` widget type with a `fields` mapping.

```yaml
# conf.yml
sections:
  - name: Infrastructure
    items:
      - title: Proxmox Cluster
        icon: si-proxmox
        type: api-response
        options:
          url: http://glint:8080/api/widget
          updateInterval: 60
          fields:
            - label: Nodes Online
              key: nodes.online
            - label: Guests Running
              key: guests.running
            - label: CPU Usage
              key: cpu.usage_pct
            - label: Memory Usage
              key: memory.usage_pct
            - label: Disks Failed
              key: disks.failed
            - label: Last Backup
              key: backups.last_backup_time
```

**With basic auth on the reverse proxy:**

```yaml
        options:
          url: http://glint:8080/api/widget
          headers:
            Authorization: "Basic dXNlcjpwYXNz"
```

!!! tip "Dashy `useProxy` option"
    If you access Glint from a browser-side Dashy deployment and hit CORS errors,
    set `useProxy: true`. This routes the request through Dashy's server-side proxy.
    For Docker-network deployments where Dashy's Node.js server calls Glint directly,
    it is not needed.

---

## All dashboards at a glance

| Dashboard | Widget type | Auth support | Request origin |
|---|---|---|---|
| [Homepage](https://gethomepage.dev) | `customapi` | `username` / `password` fields | Server-side (Next.js) |
| [Glance](https://github.com/glanceapp/glance) | `custom-api` | `headers` field | Server-side (Go) |
| [Dashy](https://dashy.to) | `api-response` | `headers` field or `useProxy` | Server-side (Node.js) |
