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
	"net"
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
	version   = flag.Bool("version", false, "Return version")
	user      = flag.String("user", "", "User for MariaDB login, specified in the [user]:[password] format")
	masterUrl = flag.String("host", "", "MariaDB master host IP and port (optional), specified in the host:[port] format")
	socket    = flag.String("socket", "/var/run/mysqld/mysqld.sock", "Path of MariaDB unix socket")
	rpluser   = flag.String("rpluser", "", "Replication user in the [user]:[password] format")
	// command specific-options
	slaves      = flag.String("slaves", "", "List of slaves connected to MariaDB master, separated by a comma")
	interactive = flag.Bool("interactive", true, "Runs the MariaDB monitor in interactive mode")
	verbose     = flag.Bool("verbose", false, "Print detailed execution info")
)

var (
	dbUser     string
	dbPass     string
	rplUser    string
	rplPass    string
	masterHost string
	masterPort string
)

type SlaveMonitor struct {
	Host      string
	Port      string
	LogBin    string
	UsingGtid string
	IOThread  string
	SQLThread string
	Delay     sql.NullInt64
}

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
	masterHost, masterPort = splitHostPort(*masterUrl)
	if *user == "" {
		log.Fatal("ERROR: No master user/pair specified.")
	}
	dbUser, dbPass = splitPair(*user)
	if *rpluser == "" {
		log.Fatal("ERROR: No replication user/pair specified.")
	}
	rplUser, rplPass = splitPair(*rpluser)
	if *verbose {
		log.Printf("Connecting to master server %s:%s", masterHost, masterPort)
	}
	var err error
	master, err = dbhelper.MySQLConnect(dbUser, dbPass, dbhelper.GetAddress(masterHost, masterPort, *socket))
	if err != nil {
		log.Fatal("Error: could not connect to master server.")
	}
	defer master.Close()
	// If slaves option is empty, then attempt automatic discovery.
	// fmt.Println("Length of slaveList", len(slaveList))
	if len(slaveList) == 0 {
		slaveList = dbhelper.GetSlaveHostsDiscovery(master)
		if len(slaveList) == 0 {
			log.Fatal("Error: no slaves found. Please supply a list of slaves manually.")
		}
	}
	for _, v := range slaveList {
		slaveHost, slavePort := splitHostPort(v)
		if validateHostPort(slaveHost, slavePort) {
			var err error
			slave, err := dbhelper.MySQLConnect(dbUser, dbPass, dbhelper.GetAddress(slaveHost, slavePort, *socket))
			if err != nil {
				log.Fatal(err)
			}
			if *verbose {
				log.Printf("Checking if server %s is a slave of server %s", slaveHost, masterHost)
			}
			if dbhelper.IsSlaveof(slave, slaveHost, masterHost) == false {
				log.Fatalf("ERROR: Server %s is not a slave.", v)
			}
			slave.Close()
		}
	}

	err = termbox.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer termbox.Close()
	termboxChan := new_tb_chan()
	interval := time.Second
	ticker := time.NewTicker(interval * 3)
	drawMonitor()
	for exit == false {
		select {
		case <-ticker.C:
			status = dbhelper.GetStatusAsInt(master)
			variable = dbhelper.GetVariables(master)
			drawMonitor()
		case event := <-termboxChan:
			switch event.Type {
			case termbox.EventKey:
				if event.Key == termbox.KeyCtrlS {
					ticker.Stop()
					switchover()
					break
				}
				if event.Key == termbox.KeyCtrlQ {
					exit = true
				}
			}
			switch event.Ch {
			case 's':
				termbox.Sync()
			}
		}
	}
}

func drawMonitor() {
	termbox.Clear(termbox.ColorWhite, termbox.ColorBlack)
	printTb(0, 0, termbox.ColorWhite, termbox.ColorBlack|termbox.AttrReverse|termbox.AttrBold, "MariaDB Replication Monitor and Health Checker")
	printfTb(0, 2, termbox.ColorWhite, termbox.ColorBlack, "    %-25s%-20s\n", "GTID Binlog Position", variable["GTID_BINLOG_POS"])
	printfTb(0, 3, termbox.ColorWhite, termbox.ColorBlack, "    %-25s%-20s\n", "GTID Strict Mode", variable["GTID_STRICT_MODE"])
	printfTb(0, 5, termbox.ColorWhite|termbox.AttrBold, termbox.ColorBlack, "%15s %6s %7s %12s %20s", "Slave address", "Port", "Binlog", "Using GTID", "Replication Health")
	vy := 6
	for _, v := range slaveList {
		slave := new(SlaveMonitor)
		slave.init(v)
		printfTb(0, vy, termbox.ColorWhite, termbox.ColorBlack, "%15s %6s %7s %12s %20s", slave.Host, slave.Port, slave.LogBin, slave.UsingGtid, slave.healthCheck())
		vy += 2
		printTb(0, vy, termbox.ColorWhite, termbox.ColorBlack, "Ctrl-Q to quit, Ctrl-S to switch over")
		vy++
	}
	termbox.Flush()
	time.Sleep(time.Duration(1) * time.Second)
}

/* Init a monitored slave object */
func (sm *SlaveMonitor) init(url string) error {
	sm.Host, sm.Port = splitHostPort(url)
	slave, err := dbhelper.MySQLConnect(dbUser, dbPass, "tcp("+url+")")
	defer slave.Close()
	if err != nil {
		return err
	}
	slaveStatus, err := dbhelper.GetSlaveStatus(slave)
	if err != nil {
		return err
	}
	sm.LogBin = dbhelper.GetVariableByName(slave, "LOG_BIN")
	sm.UsingGtid = slaveStatus.Using_Gtid
	sm.IOThread = slaveStatus.Slave_IO_Running
	sm.SQLThread = slaveStatus.Slave_SQL_Running
	sm.Delay = slaveStatus.Seconds_Behind_Master
	return err
}

/* Check replication health and return status string */
func (sm *SlaveMonitor) healthCheck() string {
	if sm.Delay.Valid == false {
		if sm.SQLThread == "Yes" && sm.IOThread == "No" {
			return "NOT OK, IO Stopped"
		} else if sm.SQLThread == "No" && sm.IOThread == "Yes" {
			return "NOT OK, SQL Stopped"
		} else {
			return "NOT OK, ALL Stopped"
		}
	} else {
		if sm.Delay.Int64 > 0 {
			return "Running LATE: " + string(sm.Delay.Int64) + " sec"
		}
		return "Running OK"
	}
}

func switchover() {
	termbox.Close()
	log.Println("Starting switchover")
	log.Println("Flushing tables on master")
	err := dbhelper.FlushTablesNoLog(master)
	if err != nil {
		log.Println("WARNING: Could not flush tables on master", err)
	}
	log.Println("Checking long running updates on master")
	if dbhelper.CheckLongRunningWrites(master, 10) > 0 {
		log.Fatal("ERROR: Long updates running on master. Cannot switchover")
	}
	log.Println("Electing a new master")
	candidate := electCandidate(slaveList)
	log.Printf("Slave %s has been elected as a new master", candidate)
	log.Printf("Rejecting updates on master")
	err = dbhelper.FlushTablesWithReadLock(master)
	if err != nil {
		log.Println("WARNING: Could not lock tables on master", err)
	}
	log.Println("Switching over")
	newMasterHost, newSlavePort := splitHostPort(candidate)
	newMaster := dbhelper.Connect(dbUser, dbPass, "tcp("+candidate+")")
	log.Println("Stopping slave thread on new master")
	newMaster.Exec("STOP SLAVE")
	cm := "CHANGE MASTER TO master_host='" + newMasterHost + "', master_port=" + newSlavePort + ", master_user='" + rplUser + "', master_password='" + rplPass + "', master_use_gtid=current_pos"
	log.Println("Switching old master as a slave")
	err = dbhelper.UnlockTables(master)
	if err != nil {
		log.Println("WARNING: Could not unlock tables on master", err)
	}
	_, err = master.Exec(cm)
	if err != nil {
		log.Fatal("Change master failed:", cm)
	}
	master.Exec("START SLAVE")
	log.Println("Resetting slave on new master and set read/write mode on")
	newMaster.Exec("RESET SLAVE ALL")
	newMaster.Exec("SET GLOBAL read_only=0")
	log.Println("Switching other slaves to the new master")
	for _, v := range slaveList {
		if v == candidate {
			continue
		}
		slaveHost, slavePort := splitHostPort(v)
		slave, err := dbhelper.MySQLConnect(dbUser, dbPass, dbhelper.GetAddress(slaveHost, slavePort, *socket))
		if err != nil {
			log.Printf("ERROR: Could not connect to slave %s, %s", v, err)
		} else {
			log.Printf("Change master on slave %s", v)
			_, err := slave.Exec(cm)
			if err != nil {
				log.Printf("Change master failed on slave %s, %s", v, err)
			}
		}
	}
	log.Println("Switchover complete")
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

/* Validate server host and port */
func validateHostPort(h string, p string) bool {
	if net.ParseIP(h) == nil {
		return false
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		/* Not an integer */
		return false
	}
	if port > 0 && port <= 65535 {
		return true
	} else {
		return false
	}
}

/* Returns a candidate from a list of slaves. If there's only one slave it will be the de facto candidate. */
func electCandidate(l []string) string {
	ll := len(l)
	if *verbose {
		log.Println("Processing %s candidates", ll)
	}
	seqList := make([]uint64, ll)
	i := 0
	hiseq := 0
	for _, v := range l {
		if *verbose {
			log.Println("Connecting to slave", v)
		}
		sl, err := dbhelper.MySQLConnect(dbUser, dbPass, "tcp("+v+")")
		if err != nil {
			log.Printf("WARNING: Server %s not online. Skipping", v)
			continue
		}
		sh, _ := splitHostPort(v)
		if dbhelper.CheckSlavePrerequisites(sl, sh, masterHost) == false {
			continue
		}
		if dbhelper.CheckSlaveSync(sl, master) == false {
			log.Printf("WARNING: Slave %s not in sync. Skipping", v)
			continue
		}
		seqList[i] = getSeqFromGtid(dbhelper.GetVariableByName(sl, "GTID_CURRENT_POS"))
		var max uint64
		if i == 0 {
			max = seqList[0]
		} else if seqList[i] > max {
			max = seqList[i]
			hiseq = i
		}
		sl.Close()
		i++
	}
	if i > 0 {
		/* Return the slave with the highest seqno. */
		return l[hiseq]
	} else {
		log.Fatal("ERROR: No suitable candidates found.")
		return "err"
	}
}

func getSeqFromGtid(gtid string) uint64 {
	e := strings.Split(gtid, "-")
	s, _ := strconv.ParseUint(e[2], 10, 64)
	return s
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
