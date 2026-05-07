# mariadb-tools

Small command-line and HTTP utilities for operating MariaDB servers and
clusters.

The repository is a Go module and contains several independent binaries. Build
only the tools you need, or install them all at once.

## Tools

| Tool | Purpose |
| --- | --- |
| `mariadb-report` | Print a compact report of server configuration and runtime status. |
| `mariadb-status` | Show `sysstat`-style MariaDB activity counters at a fixed interval. |
| `mariadb-top` | Display a terminal process-list monitor similar to `mytop`. |
| `mariadb-msm` | Monitor MariaDB multi-source replication channels and optionally send email alerts. |
| `galeracheck` | Serve HTTP 200/503 health checks for Galera nodes, intended for load balancers. |
| `servercheck` | Serve HTTP 200/503 health checks for replicated servers based on replication delay. |

`mariadb-repmgr` has moved to
[signal18/replication-manager](https://github.com/signal18/replication-manager).

## Installation

### Binary Releases

Download prebuilt binaries from the
[GitHub releases page](https://github.com/tanji/mariadb-tools/releases), then
place the binary you need somewhere in your `PATH`, for example
`/usr/local/bin`.

### Build From Source

Go 1.21 or newer is required.

```bash
git clone https://github.com/tanji/mariadb-tools.git
cd mariadb-tools

go install ./mariadb-report ./mariadb-status ./mariadb-top ./mariadb-msm ./galeracheck ./servercheck
```

The binaries are installed into `GOBIN` when set, otherwise into
`$(go env GOPATH)/bin`.

To build a single tool locally instead:

```bash
go build ./mariadb-report
go build ./galeracheck
```

## Connection Options

`mariadb-report`, `mariadb-status`, and `mariadb-top` use command-line
credentials:

```bash
mariadb-report -user monitor -password secret
mariadb-report -host 127.0.0.1 -port 3306 -user monitor -password secret
```

If `-host` is not supplied, these tools connect through a Unix socket. The
default socket is `/var/run/mysqld/mysqld.sock`.

`mariadb-msm` also uses command-line credentials, but its `-user` option expects
`user:password`.

`galeracheck` and `servercheck` read credentials from a MySQL option file. They
look for a `[mysql]` section first, then `[client]`:

```ini
[mysql]
user=monitor
password=secret
socket=/run/mysqld/mysqld.sock
```

For TCP monitoring, use `host` and `port` instead of `socket`:

```ini
[mysql]
user=monitor
password=secret
host=127.0.0.1
port=3306
```

## Usage

### `mariadb-report`

Prints a one-shot report with general server details, InnoDB settings, MyISAM
cache usage, open table/file counters, and replication state.

```bash
mariadb-report -user monitor -password secret
mariadb-report -host db01.example.com -port 3306 -user monitor -password secret
```

Options: `-user`, `-password`, `-host`, `-port`, `-socket`, `-version`.

### `mariadb-status`

Prints changing server activity counters continuously.

```bash
mariadb-status -user monitor -password secret -interval 5 -average
```

Options: `-user`, `-password`, `-host`, `-port`, `-socket`, `-interval`,
`-average`, `-collect`, `-influxdb`, `-version`.

`-collect` is experimental and writes to an InfluxDB instance at
`localhost:8086`.

### `mariadb-top`

Shows a terminal process-list view that refreshes every few seconds.

```bash
mariadb-top -user monitor -password secret
```

Options: `-user`, `-password`, `-host`, `-port`, `-socket`, `-version`.

Press `Ctrl-Q` to quit and `Ctrl-S` to force a terminal sync.

### `mariadb-msm`

Checks every configured multi-source replication channel. It exits after one
check by default, or runs continuously when `-interval` is set.

```bash
mariadb-msm -user monitor:secret -socket /var/run/mysqld/mysqld.sock -verbose
mariadb-msm -user monitor:secret -host db01.example.com:3306 -email dba@example.com -interval 5
```

Options: `-user`, `-host`, `-socket`, `-email`, `-from`, `-interval`,
`-verbose`, `-version`.

`-interval` is in minutes. Email alerts are sent through SMTP on
`localhost:25`.

More details: [mariadb-msm/README.md](mariadb-msm/README.md).

### `galeracheck`

Runs an HTTP endpoint for load balancer health checks. A synced Galera node
returns HTTP 200; unavailable, disconnected, or manually disabled nodes return
HTTP 503.

```bash
galeracheck -config /etc/galeracheck/my.cnf -port 8000
galeracheck -config /etc/galeracheck/my.cnf -port 8000 -available-when-donor
galeracheck -config /etc/galeracheck/my.cnf -sentinel-file /run/galeracheck.disabled
```

Options: `-config`, `-port`, `-mysql-socket`, `-mysql-host`, `-mysql-port`,
`-available-when-donor`, `-disable-when-readonly`, `-sentinel-file`,
`-version`.

More details: [galeracheck/README.md](galeracheck/README.md).

### `servercheck`

Runs an HTTP endpoint for replicated server health checks. It returns HTTP 200
when replication is configured and `Seconds_Behind_Master` is within
`-maxdelay`; otherwise it returns HTTP 503.

```bash
servercheck -config /etc/servercheck/my.cnf -port 8000 -maxdelay 5
```

Options: `-config`, `-port`, `-mysql-socket`, `-mysql-host`, `-mysql-port`,
`-maxdelay`.

## Minimal Privileges

Use a dedicated monitoring account. The exact grants depend on the tool and
MariaDB version, but these utilities commonly need to read global status,
variables, process list, and replication status.

```sql
CREATE USER 'monitor'@'localhost' IDENTIFIED BY 'secret';
GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'monitor'@'localhost';
FLUSH PRIVILEGES;
```

For remote monitoring, create the user for the appropriate client host instead
of `localhost`.

## Development

Run the full package build:

```bash
go build ./...
```

Format changed Go files before committing:

```bash
gofmt -w <files>
```

## License

This project is licensed under the GNU General Public License version 2. See
[LICENSE](LICENSE).
