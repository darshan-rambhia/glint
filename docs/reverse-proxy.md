# Reverse Proxy

Glint has **no built-in authentication**. Before exposing it outside your local network, put it behind a reverse proxy that handles TLS termination and access control.

This page covers three popular options: **Caddy**, **nginx**, and **Traefik**.

---

## Step 1: Bind Glint to localhost

By default Glint listens on `:3800` (all interfaces). When using a reverse proxy on the same host, change this so Glint only accepts connections from the proxy — not directly from the network.

In your `glint.yml`:

```yaml
listen: "127.0.0.1:3800"
```

For Docker deployments where proxy and Glint are on the same Docker network, use the container/service name instead and remove the host port binding:

```yaml
# docker-compose.yml
services:
  glint:
    image: ghcr.io/darshan-rambhia/glint:latest
    # No "ports:" here — only the proxy container can reach Glint
    networks:
      - proxy
```

---

## Step 2: Choose your proxy

=== "Caddy"

    [Caddy](https://caddyserver.com/) handles TLS certificates automatically via Let's Encrypt. It's the simplest option for most homelab setups.

    ### Install Caddy

    ```bash
    # Debian/Ubuntu
    sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
    sudo apt update && sudo apt install caddy
    ```

    ### Generate a password hash

    Caddy requires bcrypt-hashed passwords. Generate one with:

    ```bash
    caddy hash-password --plaintext 'your-secure-password'
    ```

    Copy the `$2a$...` hash — you'll paste it into the Caddyfile below.

    ### Caddyfile

    ```
    glint.yourdomain.com {
        # Exempt the health check endpoint from authentication
        @healthz path /healthz
        handle @healthz {
            reverse_proxy localhost:3800
        }

        # Require authentication for everything else
        @protected not path /healthz
        basicauth @protected {
            # username: the hash from `caddy hash-password`
            alice $2a$14$Zkx19XLiW6VYouLHR5NmfOFU0z2GTNmpkT/5qqR7hx4IjWJPDhjvG
        }

        reverse_proxy localhost:3800

        # HSTS: tell browsers to always use HTTPS for this domain
        header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
    }
    ```

    !!! tip "Multiple users"
        Add one `username hash` line per user inside the `basicauth` block.

    ### Reload Caddy

    ```bash
    sudo systemctl reload caddy
    ```

    Caddy will automatically obtain and renew a TLS certificate for your domain.

=== "nginx"

    ### Install nginx

    ```bash
    sudo apt install nginx
    ```

    ### Create a password file

    ```bash
    # Install htpasswd (part of apache2-utils)
    sudo apt install apache2-utils

    # Create the file and add a user (-c creates the file)
    sudo htpasswd -c /etc/nginx/.htpasswd alice

    # Add more users (omit -c to append)
    sudo htpasswd /etc/nginx/.htpasswd bob
    ```

    ### nginx site config

    Create `/etc/nginx/sites-available/glint`:

    ```nginx
    server {
        listen 80;
        server_name glint.yourdomain.com;
        return 301 https://$host$request_uri;
    }

    server {
        listen 443 ssl;
        server_name glint.yourdomain.com;

        ssl_certificate     /etc/letsencrypt/live/glint.yourdomain.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/glint.yourdomain.com/privkey.pem;

        # HSTS
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;

        # Health check endpoint — no authentication required
        location = /healthz {
            proxy_pass http://127.0.0.1:3800;
            proxy_set_header Host              $host;
            proxy_set_header X-Forwarded-Proto $scheme;
        }

        # All other paths require authentication
        location / {
            auth_basic           "Glint";
            auth_basic_user_file /etc/nginx/.htpasswd;

            proxy_pass       http://127.0.0.1:3800;
            proxy_set_header Host              $host;
            proxy_set_header X-Real-IP         $remote_addr;
            proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            # Allow htmx long-poll if you increase poll intervals
            proxy_read_timeout 60s;
        }
    }
    ```

    Enable the site and reload:

    ```bash
    sudo ln -s /etc/nginx/sites-available/glint /etc/nginx/sites-enabled/
    sudo nginx -t
    sudo systemctl reload nginx
    ```

    !!! info "TLS certificates with Certbot"
        If you don't have certificates yet, obtain them with:
        ```bash
        sudo apt install certbot python3-certbot-nginx
        sudo certbot --nginx -d glint.yourdomain.com
        ```
        Certbot will edit your nginx config to add the certificate paths and set up auto-renewal.

=== "Traefik"

    Traefik integrates natively with Docker. Labels on the Glint container configure routing, TLS, and middleware — no separate config files needed.

    ### Prerequisites

    You need a Traefik instance already running in your Docker environment. A minimal setup:

    ```yaml
    # traefik-compose.yml
    services:
      traefik:
        image: traefik:v3
        command:
          - --providers.docker=true
          - --providers.docker.exposedByDefault=false
          - --entrypoints.web.address=:80
          - --entrypoints.websecure.address=:443
          - --entrypoints.web.http.redirections.entrypoint.to=websecure
          - --certificatesresolvers.letsencrypt.acme.httpchallenge=true
          - --certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web
          - --certificatesresolvers.letsencrypt.acme.email=you@yourdomain.com
          - --certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json
        ports:
          - "80:80"
          - "443:443"
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock:ro
          - letsencrypt:/letsencrypt
        networks:
          - proxy

    volumes:
      letsencrypt:

    networks:
      proxy:
        external: true
    ```

    ### Create the shared network

    ```bash
    docker network create proxy
    ```

    ### Glint compose with Traefik labels

    First, generate a bcrypt password hash. Traefik requires the `htpasswd` format with `$$` to escape `$` in YAML:

    ```bash
    # Generate hash (requires apache2-utils)
    echo $(htpasswd -nB alice) | sed -e 's/\$/\$\$/g'
    ```

    Then add labels to your Glint service:

    ```yaml
    services:
      glint:
        image: ghcr.io/darshan-rambhia/glint:latest
        container_name: glint
        restart: unless-stopped
        # No ports: — Traefik reaches Glint via the shared network
        volumes:
          - glint-data:/data
          - ./glint.yml:/etc/glint/glint.yml:ro
        command: ["glint", "--config", "/etc/glint/glint.yml"]
        networks:
          - proxy
        labels:
          - "traefik.enable=true"

          # Router for the health check — no auth middleware
          - "traefik.http.routers.glint-healthz.rule=Host(`glint.yourdomain.com`) && Path(`/healthz`)"
          - "traefik.http.routers.glint-healthz.entrypoints=websecure"
          - "traefik.http.routers.glint-healthz.tls.certresolver=letsencrypt"
          - "traefik.http.routers.glint-healthz.service=glint-svc"

          # Router for everything else — with basicauth
          - "traefik.http.routers.glint.rule=Host(`glint.yourdomain.com`)"
          - "traefik.http.routers.glint.entrypoints=websecure"
          - "traefik.http.routers.glint.tls.certresolver=letsencrypt"
          - "traefik.http.routers.glint.middlewares=glint-auth,glint-hsts"
          - "traefik.http.routers.glint.service=glint-svc"

          # The upstream service
          - "traefik.http.services.glint-svc.loadbalancer.server.port=3800"

          # Basic auth middleware
          # Generate hash: echo $(htpasswd -nB alice) | sed -e 's/\$/\$\$/g'
          - "traefik.http.middlewares.glint-auth.basicauth.users=alice:$$2y$$05$$..."

          # HSTS middleware
          - "traefik.http.middlewares.glint-hsts.headers.stsSeconds=31536000"
          - "traefik.http.middlewares.glint-hsts.headers.stsIncludeSubdomains=true"
          - "traefik.http.middlewares.glint-hsts.headers.stsPreload=true"

    volumes:
      glint-data:

    networks:
      proxy:
        external: true
    ```

    !!! warning "Higher priority for healthz router"
        If both routers match (same host), Traefik picks the one with the longer/more specific rule — `Path(`/healthz`)` is more specific than the bare host rule, so the healthz router wins automatically.

---

## What each layer handles

| Concern | Handled by | Notes |
|---------|-----------|-------|
| TLS termination | Reverse proxy | Caddy/nginx/Traefik with Let's Encrypt |
| HSTS | Reverse proxy | Only valid on HTTPS responses — proxy sets this |
| Authentication | Reverse proxy | Basic auth, forward auth, Authelia, etc. |
| `X-Frame-Options: DENY` | Glint | Already set |
| `Content-Security-Policy` | Glint | Already set, including `upgrade-insecure-requests` |
| `X-Content-Type-Options` | Glint | Already set |
| `Cache-Control: no-store` | Glint | Already set |
| `/healthz` health checks | Both | Proxy exempts it from auth; Glint serves it |

!!! note "Don't set HSTS on Glint itself"
    Glint speaks plain HTTP internally (the proxy terminates TLS). If Glint set `Strict-Transport-Security`, browsers would receive it on unencrypted connections and might cache it incorrectly. Always set HSTS at the proxy level.

---

## Authentication beyond Basic Auth

Basic auth is the simplest option but sends credentials on every request. For a more robust setup:

- **[Authelia](https://www.authelia.com/)** — self-hosted SSO with MFA, integrates as a forward auth provider with Caddy, nginx, and Traefik
- **[Authentik](https://goauthentik.io/)** — similar to Authelia, with a richer UI
- **Cloudflare Access** — if your domain is on Cloudflare, Zero Trust Access provides SSO in front of any origin without running auth software yourself

All three work as forward authentication providers — the proxy calls the auth service before forwarding requests to Glint.

---

## Glint config changes for proxy deployments

```yaml
# glint.yml — recommended settings when behind a reverse proxy

# Bind to localhost only — proxy is the only allowed caller
listen: "127.0.0.1:3800"

# JSON logs integrate better with proxy access log aggregation
log_format: "json"
log_level: "info"
```
