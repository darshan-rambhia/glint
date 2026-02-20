# API Reference

Glint exposes a JSON API alongside its htmx fragment endpoints. The full OpenAPI 2.0 specification is available at `/swagger/` when the server is running.

<div id="swagger-ui"></div>

<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({
  url: "./swagger/swagger.json",
  dom_id: "#swagger-ui",
  presets: [SwaggerUIBundle.presets.apis],
  layout: "BaseLayout",
  deepLinking: true,
  defaultModelsExpandDepth: 1,
  tryItOutEnabled: false,
});
</script>

## Endpoints Overview

### JSON API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check with collector status |
| `GET` | `/api/widget` | Cluster summary for dashboard widgets |
| `GET` | `/api/sparkline/node/{instance}/{node}` | Node metric sparkline data points |
| `GET` | `/api/sparkline/guest/{instance}/{vmid}` | Guest CPU sparkline data points |

### HTML Fragments (htmx)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Full dashboard page |
| `GET` | `/fragments/nodes` | Node status cards |
| `GET` | `/fragments/guests` | Guest table |
| `GET` | `/fragments/backups` | Backup status |
| `GET` | `/fragments/disks` | Disk health table |
| `GET` | `/fragments/disk/{wwn}` | Disk SMART detail |
| `GET` | `/fragments/sparkline/node/{instance}/{node}` | Node sparkline SVG |
| `GET` | `/fragments/sparkline/guest/{instance}/{vmid}` | Guest sparkline SVG |

### Swagger UI

When running locally, Swagger UI is available at `http://localhost:3800/swagger/`.

## Regenerating the Spec

```bash
task swagger
```

This runs `swag init` and outputs the OpenAPI spec to `docs/swagger/`.
