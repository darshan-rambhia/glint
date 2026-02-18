# Deployment

Glint can be deployed using Docker, Podman, or as a systemd service directly on a Linux machine (no containers).

---

## Docker

### Docker Compose (Recommended)

This is the easiest way to run Glint. You need two files in the same folder:

1. A `docker-compose.yml` file (tells Docker how to run Glint)
2. A `glint.yml` file (tells Glint how to connect to your Proxmox server)

If you haven't created `glint.yml` yet, see the [Getting Started](getting-started.md) guide first.

Create a file called `docker-compose.yml` with this content:

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

Then start Glint:

```bash
docker compose up -d
```

!!! note "Scratch base image"
    The Glint image uses a `scratch` base (no shell, no utilities), so in-container `HEALTHCHECK` is not available. Use `curl http://localhost:3800/healthz` from the host or configure health checks in your reverse proxy.

### Docker Compose with Environment Variables

If you only have one Proxmox server and one PBS server, you can skip the config file entirely and pass settings as environment variables instead:

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
    environment:
      GLINT_PVE_URL: "https://192.168.1.215:8006"
      GLINT_PVE_TOKEN_ID: "glint@pam!monitor"
      GLINT_PVE_TOKEN_SECRET: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      GLINT_PBS_URL: "https://10.100.1.102:8007"
      GLINT_PBS_TOKEN_ID: "glint@pbs!monitor"
      GLINT_PBS_TOKEN_SECRET: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      GLINT_PBS_DATASTORE: "homelab"
      GLINT_NTFY_URL: "http://10.100.1.104:8080"
      GLINT_NTFY_TOPIC: "homelab-alerts"
      GLINT_LOG_FORMAT: "json"
    deploy:
      resources:
        limits:
          memory: 128M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  glint-data:
```

Replace the placeholder values with your actual Proxmox IP addresses and API tokens. See the [Getting Started](getting-started.md) guide for how to create API tokens.

### Docker Run (Without Compose)

If you prefer a single command instead of a compose file:

```bash
docker run -d \
  --name glint \
  --restart unless-stopped \
  -p 3800:3800 \
  -v glint-data:/data \
  -v $(pwd)/glint.yml:/etc/glint/glint.yml:ro \
  ghcr.io/darshan-rambhia/glint:latest \
  glint --config /etc/glint/glint.yml
```

!!! tip "What does each flag mean?"
    - `-d` --- run in the background (detached)
    - `--name glint` --- give the container a name so you can refer to it later
    - `--restart unless-stopped` --- automatically restart if it crashes or the server reboots
    - `-p 3800:3800` --- make port 3800 accessible from your browser
    - `-v glint-data:/data` --- store Glint's database in a named volume (persists across restarts)
    - `-v $(pwd)/glint.yml:...` --- mount your config file into the container (read-only)

### Updating Docker

To update Glint to the latest version:

```bash
# Pull the newest image
docker compose pull

# Restart with the new image (your data is preserved)
docker compose up -d
```

### Viewing Docker Logs

```bash
# Follow logs in real-time (press Ctrl+C to stop)
docker compose logs -f glint

# Show the last 50 lines
docker compose logs --tail=50 glint
```

### Stopping Docker

```bash
# Stop Glint (keeps your data)
docker compose down

# Stop and delete all data (start fresh)
docker compose down -v
```

---

## Podman

Podman works almost identically to Docker. If you already have Podman installed, you can use the same compose files from the Docker section above.

### Podman Run

```bash
# Create a volume for Glint's database
podman volume create glint-data

# Start Glint
podman run -d \
  --name glint \
  --restart unless-stopped \
  -p 3800:3800 \
  -v glint-data:/data \
  -v $(pwd)/glint.yml:/etc/glint/glint.yml:ro \
  ghcr.io/darshan-rambhia/glint:latest \
  glint --config /etc/glint/glint.yml
```

### Podman Compose

Install `podman-compose` if you don't have it:

```bash
pip install podman-compose
```

Then use the same `docker-compose.yml` from the Docker section:

```bash
podman-compose up -d
```

### Quadlet (Podman + systemd)

Quadlet lets Podman containers be managed by systemd, so they start automatically on boot and can be controlled with `systemctl` commands.

**Step 1:** Create the Quadlet file at `/etc/containers/systemd/glint.container`:

```ini
[Unit]
Description=Glint Proxmox Monitor
After=network-online.target
Wants=network-online.target

[Container]
Image=ghcr.io/darshan-rambhia/glint:latest
ContainerName=glint
PublishPort=3800:3800
Volume=glint-data:/data
Volume=/etc/glint/glint.yml:/etc/glint/glint.yml:ro
Exec=glint --config /etc/glint/glint.yml

[Service]
Restart=always
TimeoutStartSec=120

[Install]
WantedBy=multi-user.target default.target
```

**Step 2:** Copy your config file into place:

```bash
sudo mkdir -p /etc/glint
sudo cp glint.yml /etc/glint/glint.yml
```

**Step 3:** Start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl start glint
sudo systemctl enable glint
```

**Step 4:** Check that it's running:

```bash
sudo systemctl status glint
```

To update: `podman pull ghcr.io/darshan-rambhia/glint:latest && sudo systemctl restart glint`

---

## Homebrew

Install Glint with [Homebrew](https://brew.sh/) on macOS or Linux:

```bash
brew install darshan-rambhia/tap/glint
```

This installs the `glint` binary and the SQLite dependency automatically.

**Run Glint:**

```bash
glint --config /path/to/glint.yml
```

**Update to the latest version:**

```bash
brew upgrade glint
```

**Uninstall:**

```bash
brew uninstall glint
brew untap darshan-rambhia/tap
```

---

## Go Install

If you have [Go 1.26+](https://go.dev/dl/) installed, you can install Glint directly from source:

```bash
CGO_ENABLED=1 go install github.com/darshan-rambhia/glint/cmd/glint@latest
```

!!! note "CGO is required"
    Glint uses SQLite, which requires a C compiler. `CGO_ENABLED=1` tells Go to use it. On most Linux systems, `gcc` is already installed. On Debian/Ubuntu, install it with `sudo apt install build-essential`. On macOS, install Xcode command line tools with `xcode-select --install`.

The binary is installed to `$GOPATH/bin` (usually `~/go/bin`). Make sure this is in your `PATH`:

```bash
# Check if it's already in your PATH
glint --version

# If not found, add it (add this line to your ~/.bashrc or ~/.zshrc to make it permanent)
export PATH=$PATH:$(go env GOPATH)/bin
```

To install a specific version:

```bash
CGO_ENABLED=1 go install github.com/darshan-rambhia/glint/cmd/glint@v0.1.0
```

Once installed, create a config file and run it:

```bash
glint --config glint.yml
```

To run it as a system service, continue to [Step 3: Create a System User](#step-3-create-a-system-user) below (skip the download step since you already have the binary --- just copy it to `/usr/local/bin`):

```bash
sudo cp $(go env GOPATH)/bin/glint /usr/local/bin/glint
```

---

## Binary Install (No Containers)

This section is for running Glint directly on a Linux machine without Docker or Podman. Glint is a single file --- you download it, create a config file, and run it.

### What You Need

- A Linux machine (Debian, Ubuntu, Proxmox, etc.)
- `curl` installed (usually pre-installed)
- `sudo` access (administrator privileges)
- Your Proxmox API tokens (see [Getting Started](getting-started.md))

### Step 1: Check Your Architecture

Glint is available for two CPU types. Run this command to find out which one your machine uses:

```bash
uname -m
```

- If it says `x86_64` --- you have a standard 64-bit Intel/AMD processor (most common)
- If it says `aarch64` --- you have an ARM processor (Raspberry Pi 4/5, Oracle Cloud free tier, etc.)

### Step 2: Download Glint

=== "x86_64 (Intel/AMD)"

    ```bash
    # Download the latest release
    VERSION=$(curl -s https://api.github.com/repos/darshan-rambhia/glint/releases/latest | grep tag_name | cut -d'"' -f4)
    curl -sL "https://github.com/darshan-rambhia/glint/releases/download/${VERSION}/glint_${VERSION#v}_linux_amd64.tar.gz" -o /tmp/glint.tar.gz

    # Extract the binary to /usr/local/bin (where Linux looks for programs)
    sudo tar -xzf /tmp/glint.tar.gz -C /usr/local/bin glint

    # Clean up the download
    rm /tmp/glint.tar.gz
    ```

=== "aarch64 (ARM)"

    ```bash
    # Download the latest release
    VERSION=$(curl -s https://api.github.com/repos/darshan-rambhia/glint/releases/latest | grep tag_name | cut -d'"' -f4)
    curl -sL "https://github.com/darshan-rambhia/glint/releases/download/${VERSION}/glint_${VERSION#v}_linux_arm64.tar.gz" -o /tmp/glint.tar.gz

    # Extract the binary to /usr/local/bin (where Linux looks for programs)
    sudo tar -xzf /tmp/glint.tar.gz -C /usr/local/bin glint

    # Clean up the download
    rm /tmp/glint.tar.gz
    ```

=== "Auto-detect"

    This script detects your architecture automatically:

    ```bash
    VERSION=$(curl -s https://api.github.com/repos/darshan-rambhia/glint/releases/latest | grep tag_name | cut -d'"' -f4)
    ARCH=$(uname -m)
    case $ARCH in
      x86_64)  ARCH=amd64 ;;
      aarch64) ARCH=arm64 ;;
    esac

    curl -sL "https://github.com/darshan-rambhia/glint/releases/download/${VERSION}/glint_${VERSION#v}_linux_${ARCH}.tar.gz" -o /tmp/glint.tar.gz
    sudo tar -xzf /tmp/glint.tar.gz -C /usr/local/bin glint
    rm /tmp/glint.tar.gz
    ```

Verify the download worked:

```bash
glint --version
```

You should see output like:

```
glint v0.1.0
  commit:    abc1234def5678 (clean)
  built:     2026-02-16T12:00:00Z
  go:        go1.26
  platform:  linux/amd64
```

!!! failure "Command not found?"
    If you see `glint: command not found`, the binary might not be in your PATH. Try running it with the full path: `/usr/local/bin/glint --version`. If that works, add `/usr/local/bin` to your PATH.

### Step 3: Create a System User

For security, Glint should run as its own user instead of as root. This limits what it can access on your system.

```bash
sudo useradd -r -s /usr/sbin/nologin -d /var/lib/glint glint
```

!!! info "What does this command do?"
    - `-r` --- creates a "system" user (no home directory login, lower UID)
    - `-s /usr/sbin/nologin` --- prevents anyone from logging in as this user
    - `-d /var/lib/glint` --- sets the home directory (where Glint stores its database)
    - `glint` --- the username

### Step 4: Create Directories

Glint needs two directories:

- `/var/lib/glint` --- where the SQLite database lives (read/write)
- `/etc/glint` --- where the config file lives (read-only)

```bash
# Create the data directory and give the glint user ownership
sudo mkdir -p /var/lib/glint
sudo chown glint:glint /var/lib/glint

# Create the config directory
sudo mkdir -p /etc/glint
```

### Step 5: Create the Config File

Create the config file at `/etc/glint/glint.yml`. You can use `nano` (a simple text editor) or any editor you prefer:

```bash
sudo nano /etc/glint/glint.yml
```

Paste the following content, replacing the placeholder values with your actual Proxmox details:

```yaml
# Where to store the database (must match the directory from Step 4)
db_path: "/var/lib/glint/glint.db"

# Web server settings
listen: ":3800"

# Logging (json is recommended for systemd, see the Logging page for details)
log_level: "info"
log_format: "json"

# Your Proxmox VE server
pve:
  - name: "main"
    host: "https://YOUR_PROXMOX_IP:8006"       # <-- Replace with your Proxmox IP
    token_id: "glint@pam!monitor"               # <-- Replace with your token ID
    token_secret: "YOUR_TOKEN_SECRET_HERE"       # <-- Replace with your token secret
    insecure: true                               # Set to true if using self-signed certs

# Your Proxmox Backup Server (remove this section if you don't use PBS)
pbs:
  - name: "main-pbs"
    host: "https://YOUR_PBS_IP:8007"            # <-- Replace with your PBS IP
    token_id: "glint@pbs!monitor"               # <-- Replace with your PBS token ID
    token_secret: "YOUR_PBS_TOKEN_SECRET_HERE"   # <-- Replace with your PBS token secret
    insecure: true
    datastores: ["homelab"]                      # <-- Replace with your datastore name(s)
```

!!! tip "Saving in nano"
    Press `Ctrl+O` then `Enter` to save. Press `Ctrl+X` to exit.

!!! warning "Don't have PBS?"
    If you don't use Proxmox Backup Server, delete the entire `pbs:` section from the config file. Glint works fine with just PVE.

Set secure permissions on the config file (it contains API tokens):

```bash
sudo chmod 640 /etc/glint/glint.yml
sudo chown root:glint /etc/glint/glint.yml
```

!!! info "What do these permissions mean?"
    - `640` means: the owner (root) can read and write, the group (glint) can read, and nobody else can access it
    - `root:glint` means: owned by root, but the glint group can read it --- so the Glint service can read the config but not modify it

### Step 6: Test the Config

Before setting up the service, make sure Glint can start with your config:

```bash
# Run Glint as the glint user to test (press Ctrl+C to stop)
sudo -u glint /usr/local/bin/glint --config /etc/glint/glint.yml
```

You should see log output like:

```
{"time":"...","level":"INFO","msg":"starting glint","version":"v0.1.0","listen":":3800"}
{"time":"...","level":"INFO","msg":"collector started","name":"pve:main","interval":"15s"}
```

If you see errors like `401 Unauthorized` or `connection refused`, check the [Troubleshooting](#troubleshooting) section below.

Press `Ctrl+C` to stop the test.

### Step 7: Create the Systemd Service

Systemd is the program that manages services on Linux. Creating a "service file" tells systemd how to run Glint, when to start it, and what to do if it crashes.

Create the service file at `/etc/systemd/system/glint.service`:

```bash
sudo nano /etc/systemd/system/glint.service
```

Paste this content:

```ini
[Unit]
# A human-readable description (shows up in "systemctl status")
Description=Glint Proxmox Monitor
Documentation=https://github.com/darshan-rambhia/glint

# Wait for the network to be available before starting
After=network-online.target
Wants=network-online.target

[Service]
# "simple" means the process itself is the service (no forking)
Type=simple

# Run as the dedicated glint user (not root)
User=glint
Group=glint

# The command to start Glint
ExecStart=/usr/local/bin/glint --config /etc/glint/glint.yml

# If Glint crashes, wait 5 seconds and restart it automatically
Restart=on-failure
RestartSec=5

# Allow up to 65536 open files (generous limit for SQLite + HTTP connections)
LimitNOFILE=65536

# --- Security hardening ---
# These settings restrict what the Glint process can do, reducing the impact
# if the process is ever compromised.

# Prevent the process from gaining new privileges
NoNewPrivileges=yes

# Make the entire filesystem read-only, except for specific paths below
ProtectSystem=strict

# Hide /home, /root, and /run/user from the process
ProtectHome=yes

# Allow writing ONLY to the data directory (for the SQLite database)
ReadWritePaths=/var/lib/glint

# Allow reading the config file
ReadOnlyPaths=/etc/glint

# Give the process its own /tmp (can't see other processes' temp files)
PrivateTmp=yes

# Hide physical devices (/dev) from the process
PrivateDevices=yes

# Prevent modifying kernel settings
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes

[Install]
# Start this service when the system reaches "multi-user" mode (normal boot)
WantedBy=multi-user.target
```

Save and exit (`Ctrl+O`, `Enter`, `Ctrl+X` in nano).

### Step 8: Start the Service

Tell systemd to load the new service file, enable it to start on boot, and start it now:

```bash
# Reload systemd so it sees the new file
sudo systemctl daemon-reload

# Enable = start automatically on every boot
sudo systemctl enable glint

# Start it right now
sudo systemctl start glint
```

### Step 9: Verify It's Running

```bash
sudo systemctl status glint
```

You should see output like:

```
● glint.service - Glint Proxmox Monitor
     Loaded: loaded (/etc/systemd/system/glint.service; enabled; preset: enabled)
     Active: active (running) since Sun 2026-02-16 12:00:00 UTC; 5s ago
       Docs: https://github.com/darshan-rambhia/glint
   Main PID: 12345 (glint)
      Tasks: 8 (limit: 4915)
     Memory: 34.2M
        CPU: 120ms
     CGroup: /system.slice/glint.service
             └─12345 /usr/local/bin/glint --config /etc/glint/glint.yml
```

!!! success "What to look for"
    - `Active: active (running)` --- Glint is running
    - `enabled` --- will start automatically on boot

Check the health endpoint:

```bash
curl http://localhost:3800/healthz
```

Open your browser and go to `http://YOUR_SERVER_IP:3800` to see the dashboard.

### Viewing Logs

Logs are stored in the systemd journal. Here are the most useful commands:

```bash
# Follow logs in real-time (press Ctrl+C to stop)
sudo journalctl -u glint -f

# Show the last 50 log lines
sudo journalctl -u glint -n 50

# Show logs from the last hour
sudo journalctl -u glint --since "1 hour ago"

# Show logs since the last system boot
sudo journalctl -u glint -b

# Show only errors
sudo journalctl -u glint --priority=err
```

### Stopping and Restarting

```bash
# Stop Glint
sudo systemctl stop glint

# Start Glint
sudo systemctl start glint

# Restart Glint (stop + start)
sudo systemctl restart glint

# Disable auto-start on boot
sudo systemctl disable glint
```

### Updating to a New Version

When a new version of Glint is released:

```bash
# 1. Download the new version (replace with actual version number)
VERSION=v0.2.0
ARCH=amd64  # or arm64 for ARM

curl -sL "https://github.com/darshan-rambhia/glint/releases/download/${VERSION}/glint_${VERSION#v}_linux_${ARCH}.tar.gz" -o /tmp/glint.tar.gz
sudo tar -xzf /tmp/glint.tar.gz -C /usr/local/bin glint
rm /tmp/glint.tar.gz

# 2. Restart the service to use the new version
sudo systemctl restart glint

# 3. Verify the new version is running
glint --version
sudo systemctl status glint
```

!!! tip "Auto-detect version script"
    To always download the latest version automatically:

    ```bash
    VERSION=$(curl -s https://api.github.com/repos/darshan-rambhia/glint/releases/latest | grep tag_name | cut -d'"' -f4)
    ARCH=$(uname -m)
    case $ARCH in
      x86_64)  ARCH=amd64 ;;
      aarch64) ARCH=arm64 ;;
    esac

    curl -sL "https://github.com/darshan-rambhia/glint/releases/download/${VERSION}/glint_${VERSION#v}_linux_${ARCH}.tar.gz" -o /tmp/glint.tar.gz
    sudo tar -xzf /tmp/glint.tar.gz -C /usr/local/bin glint
    rm /tmp/glint.tar.gz
    sudo systemctl restart glint
    echo "Updated to $(glint --version | head -1)"
    ```

### Uninstalling

To completely remove Glint from your system:

```bash
# 1. Stop and disable the service
sudo systemctl stop glint
sudo systemctl disable glint

# 2. Remove the service file
sudo rm /etc/systemd/system/glint.service
sudo systemctl daemon-reload

# 3. Remove the binary
sudo rm /usr/local/bin/glint

# 4. Remove config and data (WARNING: deletes your database and history)
sudo rm -rf /etc/glint
sudo rm -rf /var/lib/glint

# 5. Remove the system user
sudo userdel glint
```

---

## Troubleshooting

### Common Issues

| Problem | What it looks like | Solution |
|---------|-------------------|----------|
| **Wrong API token** | `401 Unauthorized` in logs | Double-check `token_id` and `token_secret` in your config. Format is `user@realm!tokenname`. |
| **Token lacks permissions** | `403 Forbidden` in logs | Re-run: `pveum aclmod / -user glint@pam -role PVEAuditor` |
| **Can't reach Proxmox** | `connection refused` in logs | Check that the IP and port are correct, and that Glint's machine can reach the Proxmox server. Try: `curl -k https://YOUR_PVE_IP:8006/api2/json/version` |
| **TLS/SSL error** | `TLS handshake error` in logs | Set `insecure: true` in your config if using self-signed certificates. |
| **Service won't start** | `Active: failed` in `systemctl status` | Check logs: `sudo journalctl -u glint -n 50`. Common causes: invalid YAML syntax, wrong file permissions, missing config file. |
| **Permission denied** | `permission denied` for database file | Make sure `/var/lib/glint` is owned by the glint user: `sudo chown glint:glint /var/lib/glint` |
| **Port already in use** | `bind: address already in use` | Something else is using port 3800. Either stop that service or change `listen` in your config to a different port, e.g., `listen: ":3801"`. |
| **No data on dashboard** | Dashboard loads but shows no metrics | Wait 15 seconds for the first poll. If still empty, check logs for collector errors. |
| **No disk data** | Dashboard shows nodes and guests but no disks | S.M.A.R.T. data is polled once per hour. Wait up to 1 hour after first start, or set `disk_poll_interval: "1m"` temporarily for testing. |

### Testing Your API Token

You can test your API token directly from the command line to verify it works:

```bash
# Test PVE token (replace with your values)
curl -k -H "Authorization: PVEAPIToken=glint@pam!monitor=YOUR_SECRET" \
  https://YOUR_PVE_IP:8006/api2/json/version
```

If the token is valid, you'll see a JSON response with version info. If not, you'll see a `401` error.

```bash
# Test PBS token (replace with your values)
curl -k -H "Authorization: PBSAPIToken=glint@pbs!monitor:YOUR_SECRET" \
  https://YOUR_PBS_IP:8007/api2/json/status/datastore-usage
```

!!! note "PVE uses `=` between token name and secret, PBS uses `:`"
    - PVE: `PVEAPIToken=user@realm!token=secret`
    - PBS: `PBSAPIToken=user@realm!token:secret`

### Checking if Glint Can Reach Proxmox

From the machine where Glint is running:

```bash
# Test network connectivity to PVE (should show "Connected")
curl -k -s -o /dev/null -w "HTTP %{http_code}\n" https://YOUR_PVE_IP:8006/api2/json/version

# Test network connectivity to PBS (if using)
curl -k -s -o /dev/null -w "HTTP %{http_code}\n" https://YOUR_PBS_IP:8007/api2/json/version
```

- `HTTP 200` or `HTTP 401` --- network is fine (401 just means you didn't pass a token)
- `HTTP 000` or `Connection refused` --- Glint can't reach the server (check firewall, IP, port)

---

## Resource Usage

Glint is designed to be lightweight:

| Resource | Typical | Limit |
|----------|---------|-------|
| RAM | 30-50 MB | 128 MB |
| CPU | Negligible | --- |
| Disk (binary) | ~10 MB | --- |
| Disk (data) | ~20-50 MB | Depends on history retention |
