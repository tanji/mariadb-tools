mariadb-tools
=============

Toolkit for MariaDB

Usage: mariadb-tools [options] [commands]

List of available commands:
report	Generates a summary of MariaDB server configuration and runtime
status	sysstat-like MariaDB server activity

List of available options:
  -host="": MariaDB host IP address or FQDN
  -interval=1: Sleep interval for repeated commands
  -password="": Password for MariaDB login
  -socket="": Path of MariaDB unix socket
  -user="": User for MariaDB login
  -version: Return version
