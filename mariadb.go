package main

import (
	"bytes"
	_ "database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/dustin/go-humanize"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
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
var interval = flag.Int64("interval", 1, "Sleep interval for repeated commands")
var average = flag.Bool("average", false, "Average per second status data instead of aggregate")
var user = flag.String("user", "", "User for MariaDB login")
var password = flag.String("password", "", "Password for MariaDB login")
var host = flag.String("host", "", "MariaDB host IP address or FQDN")
var socket = flag.String("socket", "/var/run/mysqld/mysqld.sock", "Path of MariaDB unix socket")
var port = flag.String("port", "3306", "TCP Port of MariaDB server")

func main() {

	flag.Parse()
	if *version == true {
		fmt.Println("MariaDB Tools version 0.0.1")
		os.Exit(0)
	}
	var address string
	if *socket != "" {
		address = "unix(" + *socket + ")"
	}
	if *host != "" {
		address = "tcp(" + *host + ":" + *port + ")"
	}

	command := flag.Arg(0)

	// Create the database handle, confirm driver is present
	db, _ := sqlx.Open("mysql", *user+":"+*password+"@"+address+"/")
	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	status = getStatusAsInt(db)
	variable = getVariables(db)

	switch command {
	default:
		fmt.Println("Unknown command")
	case "report":
		out, err := exec.Command("uname", "-srm").Output()
		if err != nil {
			log.Fatal(err)
		}
		hostname, _ := os.Hostname()
		fmt.Printf("### MariaDB Server report for host %s\n", hostname)
		fmt.Printf("### %-25s%s", "Kernel version", out)
		fmt.Printf("### %-25s%s\n", "System Time", time.Now().Format("2006-01-02 at 03:04 (MST)"))
		fmt.Println(drawHashline("General", 60))
		var server_version string
		db.QueryRow("SELECT VERSION()").Scan(&server_version)
		pPrintStr("Version", server_version)
		now := time.Now().Unix()
		uptime := status["UPTIME"]
		start_time := time.Unix(now-uptime, 0).Local()
		pPrintStr("Started", humanize.Time(start_time))
		var count int64
		db.Get(&count, "SELECT COUNT(*) FROM information_schema.schemata")
		pPrintInt("Databases", count)
		db.Get(&count, "SELECT COUNT(*) FROM information_schema.tables")
		pPrintInt("Tables", count)
		pPrintStr("Datadir", variable["DATADIR"])
		pPrintStr("Binary Log", variable["LOG_BIN"])
		if variable["LOG_BIN"] == "ON" {
			pPrintStr("Binlog writes per hour", humanize.IBytes(uint64(status["BINLOG_BYTES_WRITTEN"]/status["UPTIME"])*3600))
		}

		slaveStatus := getSlaveStatus(db)
		if slaveStatus["Slave_IO_Running"] != nil {
			slaveIO := string(slaveStatus["Slave_IO_Running"].([]uint8))
			slaveSQL := string(slaveStatus["Slave_SQL_Running"].([]uint8))
			var slaveState string
			if slaveIO == "Yes" && slaveSQL == "Yes" {
				slaveState = "Slave configured, threads running"
			} else {
				slaveState = "Slave configured, threads stopped"
			}
			pPrintStr("Replication", slaveState)
		} else {
			pPrintStr("Replication", "Not configured")
		}

		// InnoDB
		fmt.Println(drawHashline("InnoDB", 60))
		ibps := humanize.IBytes(toUint(variable["INNODB_BUFFER_POOL_SIZE"]))
		pPrintStr("InnoDB Buffer Pool", ibps)
		ibpsPages := float64(status["INNODB_BUFFER_POOL_PAGES_TOTAL"])
		ibpsFree := float64(status["INNODB_BUFFER_POOL_PAGES_FREE"])
		ibpsUsed := toPctLow(ibpsFree, ibpsPages)
		pPrintStr("InnoDB Buffer Used", strconv.Itoa(ibpsUsed)+"%")
		ibpsDirty := float64(status["INNODB_BUFFER_POOL_PAGES_DIRTY"])
		ibpsDirtyPct := toPct(ibpsDirty, ibpsPages)
		pPrintStr("InnoDB Buffer Dirty", strconv.Itoa(ibpsDirtyPct)+"%")
		pPrintStr("InnoDB Log Files", string(variable["INNODB_LOG_FILES_IN_GROUP"])+" files of "+humanize.IBytes(toUint(variable["INNODB_LOG_FILE_SIZE"])))
		pPrintStr("InnoDB Log Buffer", humanize.IBytes(toUint(variable["INNODB_LOG_BUFFER_SIZE"])))
		var iftc string
		switch variable["INNODB_FLUSH_LOG_AT_TRX_COMMIT"] {
		case "0":
			iftc = "0 - Flush log and write buffer every sec"
		case "1":
			iftc = "1 - Write buffer and Flush log at each trx commit"
		case "2":
			iftc = "2 - Write buffer at each trx commit, Flush log every sec"
		}
		pPrintStr("InnoDB Flush Log", iftc)
		ifm := variable["INNODB_FLUSH_METHOD"]
		if ifm == "" {
			ifm = "fsync"
		}
		pPrintStr("InnoDB Flush Method", ifm)
		pPrintStr("InnoDB IO Capacity", variable["INNODB_IO_CAPACITY"])
		// MyISAM
		fmt.Println(drawHashline("MyISAM", 60))
		kbs := humanize.IBytes(toUint(variable["KEY_BUFFER_SIZE"]))
		pPrintStr("MyISAM Key Cache", kbs)
		kbs_free := float64(status["KEY_BLOCKS_UNUSED"])
		kbs_used := float64(status["KEY_BLOCKS_USED"])
		kbsUsedPct := int(((1 - (kbs_free / (kbs_free + kbs_used))) * 100) + 0.5)
		pPrintStr("MyISAM Cache Used", strconv.Itoa(kbsUsedPct)+"%")
		// Handlers
		pPrintInt("Open tables", status["OPEN_TABLES"])
		pPrintInt("Open files", status["OPEN_FILES"])
	case "status":
		var iter uint64 = 0
		for {
			if (iter % 10) == 0 {
				fmt.Printf("  %-30s%-10s  %-10s  %-10s  %-10s  %-10s\n", "Queries", "Txns", "Threads", "Aborts", "Tables", "Files")
			}
			prevStatus = status
			status = getStatusAsInt(db)
			fmt.Printf("%5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s\n", "Que", "Sel", "Ins", "Upd", "Del", "Com", "Rbk", "Con", "Thr", "Cli", "Con", "Opn", "Opd", "Opn", "Opd")
			// fmt.Println("Com_select", status["COM_SELECT"])
			fmt.Printf("%5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d\n", getCounter("QUERIES"), getCounter("COM_SELECT"), getCounter("COM_INSERT"), getCounter("COM_UPDATE"), getCounter("COM_DELETE"),
				getCounter("COM_COMMIT"), getCounter("COM_ROLLBACK"), getStatic("THREADS_CONNECTED"), getStatic("THREADS_RUNNING"), getCounter("ABORTED_CLIENTS"), getCounter("ABORTED_CONNECTS"),
				getStatic("OPEN_TABLES"), getCounter("OPENED_TABLES"), getStatic("OPEN_FILES"), getCounter("OPENED_FILES"))
			// storeStatus("Queries", getCounter("QUERIES"))
			time.Sleep(time.Duration(*interval) * time.Second)
			iter++
		}
	case "dumpstatus":
		var statuses map[string]int64
		statuses = getStatusAsInt(db)
		for k, v := range statuses {
			fmt.Println(k, v)
		}
	case "monitor":
		variable = getVariables(db)
		slaveStatus := getSlaveStatus(db)
		if len(slaveStatus) > 0 {
			log.Fatal("Server is configured as a slave, exiting")
		}
		fmt.Println("MariaDB Replication Monitor and Health Checker\n")
		pPrintStr("GTID Binlog Position", variable["GTID_BINLOG_POS"])
		pPrintStr("GTID Strict Mode", variable["GTID_STRICT_MODE"])
		slaves := getSlaveHostsDiscovery(db)
		for _, v := range slaves {
			pPrintStr("Slave server", v)
		}
	}
}

func pPrintStr(name string, value string) {
	fmt.Printf("    %-25s%-20s\n", name, value)
}

func pPrintInt(name string, value int64) {
	fmt.Printf("    %-25s%-20d\n", name, value)
}

func storeStatus(s string, u int64) {
	slice1 := []int64{u}
	slice2 := [][]int64{slice1}
	ts := timeSeries{s, []string{"value"}, slice2}
	// fmt.Printf("%s %d \n", ts.Name, ts.Points)
	b, err := json.Marshal(ts)
	js := string(b)
	js = "[" + js + "]"
	b = []byte(js)
	if err != nil {
		log.Fatal(err)
	}
	body := bytes.NewBuffer(b)
	r, _ := http.Post("http://localhost:8086/db/mariadb/series?u=root&p=root", "text/json", body)
	response, _ := ioutil.ReadAll(r.Body)
	fmt.Println(string(response))
}

func drawHashline(t string, l int) string {
	var hashline string
	hashline = "### " + t + " "
	l = l - len(hashline)
	for i := 0; i <= l; i++ {
		hashline = hashline + "#"
	}
	return hashline
}

func getCounter(s string) int64 {
	if *average == true && *interval > 1 {
		return (status[s] - prevStatus[s]) / *interval
	} else {
		return status[s] - prevStatus[s]
	}
}

func getStatic(s string) int64 {
	return status[s]
}

func toPct(q float64, d float64) int {
	return int(((q / d) * 100) + 0.5)
}

func toPctLow(q float64, d float64) int {
	return int(((1 - (q / d)) * 100) + 0.5)
}

func toUint(s string) uint64 {
	u, _ := strconv.ParseUint(s, 10, 64)
	return u
}

func toInt(s string) int64 {
	u, _ := strconv.ParseInt(s, 10, 64)
	return u
}

func toFloat(s string) float64 {
	u, _ := strconv.ParseFloat(s, 64)
	return u
}

func getSlaveStatus(db *sqlx.DB) map[string]interface{} {
	type Status struct {
		Key   string
		Value string
	}
	rows, err := db.Queryx("SHOW SLAVE STATUS")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	if err != nil {
		log.Fatal(err)
	}
	results := make(map[string]interface{})
	for rows.Next() {
		err = rows.MapScan(results)
		if err != nil {
			log.Fatal(err)
		}
		/* r := results["Master_Port"].([]uint8)
		s := string(r)
		fmt.Println(s) */
	}
	return results
}

func getSlaveHosts(db *sqlx.DB) map[string]interface{} {
	type Status struct {
		Key   string
		Value string
	}
	rows, err := db.Queryx("SHOW SLAVE HOSTS")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	if err != nil {
		log.Fatal(err)
	}
	results := make(map[string]interface{})
	for rows.Next() {
		err = rows.MapScan(results)
		if err != nil {
			log.Fatal(err)
		}
	}
	return results
}

func getSlaveHostsDiscovery(db *sqlx.DB) []string {
	hosts := []string{}
	err := db.Select(&hosts, "select host from information_schema.processlist where command ='binlog dump'")
	if err != nil {
		log.Fatal(err)
	}
	return hosts
}

func getStatus(db *sqlx.DB) map[string]string {
	type Variable struct {
		Variable_name string
		Value         string
	}
	vars := make(map[string]string)
	rows, err := db.Queryx("SELECT Variable_name AS variable_name, Variable_Value AS value FROM information_schema.global_status")
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var v Variable
		err := rows.Scan(&v.Variable_name, &v.Value)
		if err != nil {
			log.Fatal(err)
		}
		vars[v.Variable_name] = v.Value
	}
	return vars
}
func getStatusAsInt(db *sqlx.DB) map[string]int64 {
	type Variable struct {
		Variable_name string
		Value         int64
	}
	vars := make(map[string]int64)
	rows, err := db.Queryx("SELECT Variable_name AS variable_name, Variable_Value AS value FROM information_schema.global_status")
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var v Variable
		rows.Scan(&v.Variable_name, &v.Value)
		vars[v.Variable_name] = v.Value
	}
	return vars
}

func getVariables(db *sqlx.DB) map[string]string {
	type Variable struct {
		Variable_name string
		Value         string
	}
	vars := make(map[string]string)
	rows, err := db.Queryx("SELECT Variable_name AS variable_name, Variable_Value AS value FROM information_schema.global_variables")
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var v Variable
		err := rows.Scan(&v.Variable_name, &v.Value)
		if err != nil {
			log.Fatal(err)
		}
		vars[v.Variable_name] = v.Value
	}
	return vars
}
