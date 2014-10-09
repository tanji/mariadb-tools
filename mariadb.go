package main

import (
	_ "database/sql"
	"flag"
	"fmt"
	"github.com/dustin/go-humanize"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"
)

var status map[string]string
var prevStatus map[string]string
var variable map[string]string

func main() {
	var version = flag.Bool("version", false, "Return version")
	var interval = flag.Int64("interval", 1, "Sleep interval for repeated commands")
	var user = flag.String("user", "", "User for MariaDB login")
	var password = flag.String("password", "", "Password for MariaDB login")
	var host = flag.String("host", "", "MariaDB host IP address or FQDN")
	var socket = flag.String("socket", "", "Path of MariaDB unix socket")
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
		address = "tcp(" + *host + ")"
	}

	command := flag.Arg(0)

	// Create the database handle, confirm driver is present
	db, _ := sqlx.Open("mysql", *user+":"+*password+"@"+address+"/")
	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	status = getStatus(db)
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
		fmt.Printf("### %-20s%s", "Kernel version", out)
		fmt.Printf("### %-20s%s\n", "System Time", time.Now().Format("2006-01-02 at 03:04 (MST)"))
		fmt.Println(drawHashline("General", 60))
		var server_version string
		fStr := "    %-20s%-20s\n"
		fInt := "    %-20s%-20d\n"
		db.QueryRow("SELECT VERSION()").Scan(&server_version)
		fmt.Printf(fStr, "Version", server_version)
		now := time.Now().Unix()
		uptime := toInt(status["UPTIME"])
		start_time := time.Unix(now-uptime, 0).Local()
		fmt.Printf(fStr, "Started", humanize.Time(start_time))
		var count int
		db.Get(&count, "SELECT COUNT(*) FROM information_schema.schemata")
		fmt.Printf(fInt, "Databases", count)
		db.Get(&count, "SELECT COUNT(*) FROM information_schema.tables")
		fmt.Printf(fInt, "Tables", count)
		fmt.Printf(fStr, "Datadir", variable["DATADIR"])
		fmt.Printf(fStr, "Binary Log", variable["LOG_BIN"])
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
			fmt.Printf("    %-20s%s\n", "Replication", slaveState)
		} else {
			fmt.Printf("    %-20s%s\n", "Replication", "Not configured")
		}

		// InnoDB
		fmt.Println(drawHashline("InnoDB", 60))
		ibps := humanize.IBytes(toUint(variable["INNODB_BUFFER_POOL_SIZE"]))
		fmt.Printf(fStr, "InnoDB Buffer Pool", ibps)
		ibpsPages := toFloat(status["INNODB_BUFFER_POOL_PAGES_TOTAL"])
		ibpsFree := toFloat(status["INNODB_BUFFER_POOL_PAGES_FREE"])
		ibpsUsed := toPctLow(ibpsFree, ibpsPages)
		fmt.Printf(fStr, "InnoDB Buffer Used", strconv.Itoa(ibpsUsed)+"%")
		ibpsDirty := toFloat(status["INNODB_BUFFER_POOL_PAGES_DIRTY"])
		ibpsDirtyPct := toPct(ibpsDirty, ibpsPages)
		fmt.Printf(fStr, "InnoDB Buffer Dirty", strconv.Itoa(ibpsDirtyPct)+"%")
		fmt.Printf(fStr, "InnoDB Log Files", string(variable["INNODB_LOG_FILES_IN_GROUP"])+" files of "+humanize.IBytes(toUint(variable["INNODB_LOG_FILE_SIZE"])))
		// MyISAM
		fmt.Println(drawHashline("MyISAM", 60))
		kbs := humanize.IBytes(toUint(variable["KEY_BUFFER_SIZE"]))
		fmt.Printf(fStr, "MyISAM Key Cache", kbs)
		kbs_free := toFloat(status["KEY_BLOCKS_UNUSED"])
		kbs_used := toFloat(status["KEY_BLOCKS_USED"])
		kbsUsedPct := int(((1 - (kbs_free / (kbs_free + kbs_used))) * 100) + 0.5)
		fmt.Printf(fStr, "MyISAM Cache Used", strconv.Itoa(kbsUsedPct)+"%")
		fmt.Printf(fInt, "Open tables", toUint(status["OPEN_TABLES"]))
		fmt.Printf(fInt, "Open files", toUint(status["OPEN_FILES"]))
	case "status":
		var iter uint64 = 0
		for {
			if (iter % 10) == 0 {
				fmt.Printf("  %-30s%-10s  %-10s  %-10s  %-10s  %-10s\n", "Queries", "Txns", "Threads", "Aborts", "Tables", "Files")
			}
			prevStatus = status
			status = getStatus(db)
			fmt.Printf("%5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s %5s\n", "Que", "Sel", "Ins", "Upd", "Del", "Com", "Rbk", "Con", "Thr", "Cli", "Con", "Opn", "Opd", "Opn", "Opd")
			// fmt.Println("Com_select", status["COM_SELECT"])
			fmt.Printf("%5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d %5d\n", getCounter("QUERIES"), getCounter("COM_SELECT"), getCounter("COM_INSERT"), getCounter("COM_UPDATE"), getCounter("COM_DELETE"),
				getCounter("COM_COMMIT"), getCounter("COM_ROLLBACK"), getStatic("THREADS_CONNECTED"), getStatic("THREADS_RUNNING"), getCounter("ABORTED_CLIENTS"), getCounter("ABORTED_CONNECTS"),
				getStatic("OPEN_TABLES"), getCounter("OPENED_TABLES"), getStatic("OPEN_FILES"), getCounter("OPENED_FILES"))
			time.Sleep(time.Duration(*interval) * time.Second)
			iter++
		}
	}
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
	return toInt(status[s]) - toInt(prevStatus[s])
}

func getStatic(s string) int64 {
	return toInt(status[s])
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
