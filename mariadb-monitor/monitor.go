package main

import (
	_ "database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/mariadb-tools/common"
	"github.com/mariadb-tools/dbhelper"
	"log"
)

var status map[string]int64
var prevStatus map[string]int64
var variable map[string]string

type timeSeries struct {
	Name    string
	Columns []string
	Points  [][]int64
}

var version = flag.Bool("version", false, "Return version")
var user = flag.String("user", "", "User for MariaDB login")
var password = flag.String("password", "", "Password for MariaDB login")
var host = flag.String("host", "", "MariaDB host IP address or FQDN")
var socket = flag.String("socket", "/var/run/mysqld/mysqld.sock", "Path of MariaDB unix socket")
var port = flag.String("port", "3306", "TCP Port of MariaDB server")

func main() {

	flag.Parse()
	if *version == true {
		common.Version()
	}
	var address string
	if *socket != "" {
		address = "unix(" + *socket + ")"
	}
	if *host != "" {
		address = "tcp(" + *host + ":" + *port + ")"
	}

	// Create the database handle, confirm driver is present
	db, _ := sqlx.Open("mysql", *user+":"+*password+"@"+address+"/")
	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	status = dbhelper.GetStatusAsInt(db)
	variable = dbhelper.GetVariables(db)

	variable = dbhelper.GetVariables(db)
	slaveStatus := dbhelper.GetSlaveStatus(db)
	if len(slaveStatus) > 0 {
		log.Fatal("Server is configured as a slave, exiting")
	}
	fmt.Println("MariaDB Replication Monitor and Health Checker\n")
	pPrintStr("GTID Binlog Position", variable["GTID_BINLOG_POS"])
	pPrintStr("GTID Strict Mode", variable["GTID_STRICT_MODE"])
	slaves := dbhelper.GetSlaveHostsDiscovery(db)
	for _, v := range slaves {
		pPrintStr("Slave server", v)
	}

}

func pPrintStr(name string, value string) {
	fmt.Printf("    %-25s%-20s\n", name, value)
}

func pPrintInt(name string, value int64) {
	fmt.Printf("    %-25s%-20d\n", name, value)
}
