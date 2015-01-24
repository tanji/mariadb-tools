mariadb-tools
=============

Tools for MariaDB

Usage: mariadb-[command] [options]

List of available commands:

**report**	Generates a summary of MariaDB server configuration and runtime

**status**	sysstat-like MariaDB server activity

**repmgr** 	GTID replication switchover and monitor utility

**top**	mytop clone

Repmgr example usage
--------------------
	mariadb-repmgr -host=db1:3306 -slaves=db2:3306,db3:3306 -user root -rpluser repl:lper


