# galeracheck

HTTP health check service for Galera Cluster nodes. Returns HTTP 200 or 503 based on node state for use with load balancers (HAProxy, AWS ELB, etc.).

## Overview

galeracheck is a lightweight HTTP service that monitors the health of a Galera Cluster node and reports its availability status. Load balancers can poll this endpoint to determine whether to route traffic to the node.

## Installation

### From Debian Package (Recommended)

Download the latest release from [GitHub Releases](https://github.com/tanji/mariadb-tools/releases):

```bash
wget https://github.com/tanji/mariadb-tools/releases/download/1.0.1-galeracheck/galeracheck_1.0.1_amd64.deb
sudo dpkg -i galeracheck_1.0.1_amd64.deb
```

### From Source

```bash
go build
sudo cp galeracheck /usr/local/bin/
```

### Systemd Service

If installing from source, manually install the systemd service:

```bash
sudo cp galeracheck.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable galeracheck
sudo systemctl start galeracheck
```

### Building Debian Package Locally

To build a Debian package locally (requires [nfpm](https://nfpm.goreleaser.com/install/)):

```bash
# Build the binary
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags="-w -s -X main.version=1.0.1" \
  -o galeracheck .

# Build the package
VERSION=1.0.1 nfpm package --packager deb --target .

# Inspect the package
dpkg-deb --info galeracheck_1.0.1_amd64.deb
dpkg-deb --contents galeracheck_1.0.1_amd64.deb

# Install
sudo dpkg -i galeracheck_1.0.1_amd64.deb
```

## Usage

```bash
galeracheck [options]
```

### Options

```
-config string
    MySQL config file to use (default "~/.my.cnf")

-port int
    TCP port to listen on (default 8000)

-mysql-socket string
    Path to unix socket of monitored MySQL instance (default "/run/mysqld/mysqld.sock")

-mysql-host string
    Hostname or IP address of monitored MySQL instance (default: use unix socket)

-mysql-port string
    Port of monitored MySQL instance (default "3306")

-available-when-donor
    Keep node available in LB when in donor state (state 2)
    Useful during RSU (Rolling Schema Upgrade) operations

-disable-when-readonly
    Remove node from LB when read_only is set
    Useful for gracefully taking a node out of rotation
```

## Configuration

Create a MySQL config file (default `~/.my.cnf`) with credentials:

```ini
[mysql]
user=monitor
password=secret
socket=/run/mysqld/mysqld.sock
```

Or for TCP connections:

```ini
[mysql]
user=monitor
password=secret
host=localhost
port=3306
```

The `[client]` section is also supported if `[mysql]` is not present.

## MySQL User Privileges

The monitoring user needs minimal privileges:

```sql
CREATE USER 'monitor'@'localhost' IDENTIFIED BY 'secret';
GRANT PROCESS ON *.* TO 'monitor'@'localhost';
FLUSH PRIVILEGES;
```

## Health Check Logic

The service queries the following Galera status variables:
- `wsrep_local_state` - Node's current state
- `wsrep_cluster_size` - Number of nodes in cluster
- `read_only` (when `-disable-when-readonly` is used)

### Node States

- **State 4 (Synced)**: Node is synchronized and operational
- **State 2 (Donor/Desynced)**: Node is acting as donor for SST/IST or in RSU mode

### Availability Rules

The service returns **HTTP 200** (available) when:
1. Node is in state 4 (Synced)
2. OR `-available-when-donor` is enabled AND node is in state 2
3. OR `-disable-when-readonly` is enabled AND read_only is OFF AND state is 4
4. **OR cluster size is 1 AND state is 4** (failsafe for last remaining node)

Otherwise, returns **HTTP 503** (unavailable).

### Single-Node Failsafe

When the cluster degrades to a single node (`wsrep_cluster_size = 1`), that node remains available even without `-available-when-donor` enabled. This prevents complete service outage in degraded cluster scenarios.

## Load Balancer Integration

### HAProxy Example

```
backend galera_cluster
    mode tcp
    balance leastconn
    option httpchk

    server node1 10.0.1.11:3306 check port 8000
    server node2 10.0.1.12:3306 check port 8000
    server node3 10.0.1.13:3306 check port 8000
```

### AWS Application Load Balancer

Configure health check:
- Protocol: HTTP
- Port: 8000
- Path: /
- Success codes: 200

## Use Cases

### Standard Deployment

```bash
galeracheck -port 8000
```

Nodes in state 4 (Synced) are available. Donor nodes (state 2) are removed from LB.

### Rolling Schema Upgrade (RSU)

```bash
galeracheck -port 8000 -available-when-donor
```

Nodes remain available during RSU DDL operations (when in state 2).

### Graceful Node Removal

```bash
galeracheck -port 8000 -disable-when-readonly
```

Set `read_only=ON` on a node to remove it from the LB without triggering desync.

### TCP Connection

```bash
galeracheck -port 8000 -mysql-host 127.0.0.1 -mysql-port 3306
```

Monitor via TCP instead of unix socket.

## Response Format

**Available (HTTP 200)**
```
200 Galera Node is synced
```

**Unavailable (HTTP 503)**
```
503 Galera Node is not synced
```

**Connection Error (HTTP 503)**
```
503 No connection
```

**Query Error (HTTP 503)**
```
503 Cannot check cluster state: <error>
```

## Troubleshooting

### Connection Issues

Check MySQL credentials and socket/host configuration:
```bash
mysql --defaults-file=~/.my.cnf -e "SHOW STATUS LIKE 'wsrep%';"
```

### Permission Issues

Verify the monitoring user has PROCESS privilege:
```sql
SHOW GRANTS FOR 'monitor'@'localhost';
```

### Service Logs

```bash
sudo journalctl -u galeracheck -f
```

## License

See parent project license.
