package main

import (
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/mariadb-tools/common"
	"github.com/mariadb-tools/dbhelper"
	"github.com/nsf/termbox-go"
	"log"
	"strconv"
	"strings"
	"time"
)

var (
	status     map[string]int64
	prevStatus map[string]int64
	variable   map[string]string
	slaveList  []string
	exit       bool
)

var (
	master    *sqlx.DB
	slave     *sqlx.DB
	version   = flag.Bool("version", false, "Return version")
	user      = flag.String("user", "", "User for MariaDB login, specified in the [user]:[password] format")
	masterUrl = flag.String("host", "", "MariaDB master host IP and port (optional), specified in the host:[port] format")
	socket    = flag.String("socket", "/var/run/mysqld/mysqld.sock", "Path of MariaDB unix socket")
	rpluser   = flag.String("rpluser", "", "Replication user in the [user]:[password] format")
	// command specific-options
	slaves = flag.String("slaves", "", "List of slaves connected to MariaDB master, separated by a comma")
)

var (
	dbUser  string
	dbPass  string
	rplUser string
	rplPass string
)

func main() {
	flag.Parse()
	if *version == true {
		common.Version()
	}
	// if slaves option has been supplied, split into a slice.
	if *slaves != "" {
		slaveList = strings.Split(*slaves, ",")
	}
	if *masterUrl == "" {
		log.Fatal("ERROR: No master host specified.")
	}
	masterHost, masterPort := splitHostPort(*masterUrl)
	if *user == "" {
		log.Fatal("ERROR: No master user/pair specified.")
	}
	dbUser, dbPass = splitPair(*user)
	if *rpluser == "" {
		log.Fatal("ERROR: No replication user/pair specified.")
	}
	rplUser, rplPass = splitPair(*rpluser)
	master = dbhelper.Connect(dbUser, dbPass, dbhelper.GetAddress(masterHost, masterPort, *socket))
	// If slaves option is empty, then attempt automatic discovery.
	// fmt.Println("Length of slaveList", len(slaveList))
	if len(slaveList) == 0 {
		slaveList = dbhelper.GetSlaveHostsDiscovery(master)
		if len(slaveList) == 0 {
			log.Fatal("Error: no slaves found. Please supply a list of slaves manually.")
		}
	}

	defer master.Close()

	// slaveStatus := dbhelper.GetSlaveStatus(master)
	/*
		if slaveStatus != nil {
			log.Fatal("Server is configured as a slave, exiting")
		} */
	err := termbox.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer termbox.Close()
	termboxChan := new_tb_chan()
	interval := time.Second
	ticker := time.NewTicker(interval * 3)
	for exit == false {
		select {
		case <-ticker.C:
			drawMonitor()
		case event := <-termboxChan:
			switch event.Type {
			case termbox.EventKey:
				if event.Key == termbox.KeyCtrlS {
					termbox.Sync()
				}
				if event.Key == termbox.KeyCtrlQ {
					exit = true
				}
			}
			switch event.Ch {
			case 's':
				ticker.Stop()
				switchover()
				// exit = true
			}
		}
	}
}

func drawMonitor() {
	status = dbhelper.GetStatusAsInt(master)
	variable = dbhelper.GetVariables(master)
	termbox.Clear(termbox.ColorWhite, termbox.ColorBlack)
	printTb(0, 0, termbox.ColorWhite, termbox.ColorBlack|termbox.AttrReverse|termbox.AttrBold, "MariaDB Replication Monitor and Health Checker")
	printfTb(0, 2, termbox.ColorWhite, termbox.ColorBlack, "    %-25s%-20s\n", "GTID Binlog Position", variable["GTID_BINLOG_POS"])
	printfTb(0, 3, termbox.ColorWhite, termbox.ColorBlack, "    %-25s%-20s\n", "GTID Strict Mode", variable["GTID_STRICT_MODE"])
	printfTb(0, 5, termbox.ColorWhite|termbox.AttrBold, termbox.ColorBlack, "%15s %6s %7s %12s %20s", "Slave address", "Port", "Binlog", "Using GTID", "Replication Health")
	vy := 6
	for _, v := range slaveList {
		slaveItems := strings.Split(v, ":")
		if len(slaveItems) != 2 {
			log.Fatal("Slave definition incorrect:", v)
		}
		slave := dbhelper.Connect(dbUser, dbPass, "tcp("+v+")")
		slaveStatus := dbhelper.GetSlaveStatus(slave)
		slaveVariables := dbhelper.GetVariables(slave)
		printfTb(0, vy, termbox.ColorWhite, termbox.ColorBlack, "%15s %6s %7s %12s %20s", slaveItems[0], slaveItems[1], slaveVariables["LOG_BIN"], slaveStatus.Using_Gtid, slaveHealth(slaveStatus.Slave_IO_Running, slaveStatus.Slave_SQL_Running, slaveStatus.Seconds_Behind_Master))
		slave.Close()
		vy += 2
		printTb(0, vy, termbox.ColorWhite, termbox.ColorBlack, "Ctrl-Q to quit, Ctrl-S to switch over")
		vy++
	}
	termbox.Flush()
	time.Sleep(time.Duration(1) * time.Second)
}

func switchover() {
	termbox.Clear(termbox.ColorWhite, termbox.ColorBlack)
	printTb(0, 0, termbox.ColorWhite, termbox.ColorBlack, "Starting switchover")
	/* Elect candidate from list of slaves. If there's only one slave it will be the de facto candidate. */
	candidate := electCandidate(slaveList)
	printfTb(0, 1, termbox.ColorWhite, termbox.ColorBlack, "Slave %s has been elected as a new master", candidate)
	if !checkSlaveSync(candidate) {
		printTb(0, 2, termbox.ColorWhite, termbox.ColorBlack, "Slave not in sync. Aborting switchover")
		termbox.Flush()
	} else {
		printTb(0, 2, termbox.ColorWhite, termbox.ColorBlack, "Slave in sync. Switching over")
		newMasterHost, newSlavePort := splitHostPort(candidate)
		slave := dbhelper.Connect(dbUser, dbPass, "tcp("+candidate+")")
		slave.Exec("STOP SLAVE")
		cm := "CHANGE MASTER TO master_host='" + newMasterHost + "', master_port=" + newSlavePort + ", master_user='" + rplUser + "', master_password='" + rplPass + "', master_use_gtid=current_pos"
		_, err := master.Exec(cm)
		if err != nil {
			log.Fatal("Change master failed:", cm)
		}
		master.Exec("START SLAVE")
		slave.Exec("RESET SLAVE ALL")
		printTb(0, 3, termbox.ColorWhite, termbox.ColorBlack, "Switchover complete")
		termbox.Flush()
	}
}

/* Returns two host and port items from a pair, e.g. host:port */
func splitHostPort(s string) (string, string) {
	items := strings.Split(s, ":")
	if len(items) == 1 {
		return items[0], "3306"
	} else {
		return items[0], items[1]
	}
}

/* Returns generic items from a pair, e.g. user:pass */
func splitPair(s string) (string, string) {
	items := strings.Split(s, ":")
	if len(items) == 1 {
		return items[0], ""
	} else {
		return items[0], items[1]
	}
}

/* Check if a slave is in sync with his master */
func checkSlaveSync(s string) bool {
	slave := dbhelper.Connect(dbUser, dbPass, "tcp("+s+")")
	defer slave.Close()
	slaveVar := dbhelper.GetVariables(slave)
	masterVar := dbhelper.GetVariables(master)
	if masterVar["GTID_BINLOG_POS"] == slaveVar["GTID_CURRENT_POS"] {
		return true
	} else {
		return false
	}
}

/* Returns a candidate from a list of slaves. If there's only one slave it will be the de facto candidate. */
func electCandidate(l []string) string {
	ll := len(l)
	if ll == 1 {
		return l[0]
	} else {
		/* Get a seqno for each slave */
		seqList := make([]uint64, ll)
		i := 0
		hiseq := 0
		for _, v := range l {
			slave := dbhelper.Connect(dbUser, dbPass, "tcp("+v+")")
			vars := dbhelper.GetVariables(slave)
			seqList[i] = getSeqFromGtid(vars["GTID_CURRENT_POS"])
			var max uint64
			if i == 0 {
				max = seqList[0]
			} else {
				if seqList[i] > max {
					max = seqList[i]
					hiseq = i
				}
			}
			slave.Close()
			i++
		}
		/* Return the slave with the highest seqno. */
		return l[hiseq]
	}
}

func getSeqFromGtid(gtid string) uint64 {
	e := strings.Split("-", gtid)
	s, _ := strconv.ParseUint(e[2], 10, 64)
	return s
}

/* Check replication health and return status string. */
func slaveHealth(iorun string, sqlrun string, sbm sql.NullInt64) string {
	if sbm.Valid == false {
		if sqlrun == "Yes" && iorun == "No" {
			return "NOT OK, IO Stopped"
		} else if sqlrun == "No" && iorun == "Yes" {
			return "NOT OK, SQL Stopped"
		} else {
			return "NOT OK, ALL Stopped"
		}
	} else {
		if sbm.Int64 > 0 {
			return "Running LATE: " + string(sbm.Int64) + " sec"
		}
		return "Running OK"
	}
}

func printTb(x, y int, fg, bg termbox.Attribute, msg string) {
	for _, c := range msg {
		termbox.SetCell(x, y, c, fg, bg)
		x++
	}
}

func printfTb(x, y int, fg, bg termbox.Attribute, format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	printTb(x, y, fg, bg, s)
}

func new_tb_chan() chan termbox.Event {
	termboxChan := make(chan termbox.Event)
	go func() {
		for {
			termboxChan <- termbox.PollEvent()
		}
	}()
	return termboxChan
}
