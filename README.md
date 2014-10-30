mariadb-tools
=============

Toolkit for MariaDB

Usage: mariadb-[command] [options]

List of available commands:
report	Generates a summary of MariaDB server configuration and runtime
status	sysstat-like MariaDB server activity
monitor Monitors GTID replication and switch slaves from master

List of available options:
  -host="": MariaDB host IP address or FQDN
  -interval=1: Sleep interval for repeated commands
  -password="": Password for MariaDB login
  -socket="": Path of MariaDB unix socket
  -user="": User for MariaDB login
  -port="": Port of MariaDB server
  -version: Return version
