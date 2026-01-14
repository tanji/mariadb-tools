// galeracheck.go
// check for galera cluster health and return http 503 or 200
// for use with load balancers (haproxy, aws...)

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	osuser "os/user"
	"strings"

	"github.com/go-ini/ini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var (
	version     = "dev"
	cnffile     string
	port        int
	sockopt     string
	hostopt     string
	portopt     string
	awd         bool
	dwr         bool
	user        string
	password    string
	mysqlsocket string
	mysqlhost   string
	mysqlport   string
)

func init() {
	var versionFlag bool
	flag.StringVar(&cnffile, "config", "~/.my.cnf", "MySQL Config file to use")
	flag.IntVar(&port, "port", 8000, "TCP port to listen on")
	flag.StringVar(&sockopt, "mysql-socket", "/run/mysqld/mysqld.sock", "Path to unix socket of monitored MySQL instance")
	flag.StringVar(&hostopt, "mysql-host", "", "Hostname or IP address of monitored MySQL instance")
	flag.StringVar(&portopt, "mysql-port", "3306", "Port of monitored MySQL instance")
	flag.BoolVar(&awd, "available-when-donor", false, "Available when donor")
	flag.BoolVar(&dwr, "disable-when-readonly", false, "Disable when read_only flag is set (desirable when wanting to take a node out of the cluster without desync)")
	flag.BoolVar(&versionFlag, "version", false, "Print version and exit")
	flag.Parse()

	if versionFlag {
		fmt.Printf("galeracheck version %s\n", version)
		os.Exit(0)
	}
}

func main() {
	usr, _ := osuser.Current()

	if strings.Contains(cnffile, "~/") {
		cnffile = strings.Replace(cnffile, "~", usr.HomeDir, 1)
	}

	cfg, err := ini.Load(cnffile)
	if err != nil {
		log.Fatalln("Could not load config file:", err)
	}

	section, err := cfg.GetSection("mysql")
	if err != nil {
		section, err = cfg.GetSection("client")
		if err != nil {
			log.Fatalln("No [mysql] or [client] sections found in config file", err)
		}
	}

	ukey, err := section.GetKey("user")
	if err != nil {
		log.Println("No user key found in config file. Using current OS user instead")
		user = usr.Username
	} else {
		user = ukey.String()
	}

	pkey, err := section.GetKey("password")
	if err != nil {
		log.Fatalln("No password key found in config file. Exiting")
	}

	password = pkey.String()

	mysqlsocket = section.Key("socket").String()
	if mysqlsocket == "" {
		mysqlsocket = sockopt
	}

	mysqlhost = section.Key("host").String()
	if mysqlhost == "" {
		mysqlhost = hostopt
	}

	mysqlport = section.Key("port").String()
	if mysqlport == "" {
		mysqlport = portopt
	}

	httpAddr := fmt.Sprintf(":%v", port)

	log.Printf("Listening to %v", httpAddr)
	http.HandleFunc("/", clustercheck)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}

func clustercheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "text/html")
	var dsn string
	if mysqlhost == "" {
		dsn = fmt.Sprintf("%s:%s@unix(%s)/", user, password, mysqlsocket)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/", user, password, mysqlhost, mysqlport)
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		log.Println("MySQL can't connect:", err)
		w.WriteHeader(503)
		fmt.Fprint(w, "503 No connection")
		return
	}
	defer db.Close()

	var (
		readonly     string
		state        int
		clusterSize  int
	)

	if dwr {
		err = db.QueryRow("select variable_value as readonly from information_schema.global_variables where variable_name='read_only'").Scan(&readonly)
		if err != nil {
			log.Println("Cannot check read_only:", err)
			w.WriteHeader(503)
			fmt.Fprintf(w, "503 Cannot check read_only: %v", err)
			return
		}
	}

	err = db.QueryRow("select variable_value as state from information_schema.global_status where variable_name='wsrep_local_state'").Scan(&state)
	if err != nil {
		log.Println("Cannot check cluster state:", err)
		w.WriteHeader(503)
		fmt.Fprintf(w, "503 Cannot check cluster state: %v", err)
		return
	}

	err = db.QueryRow("select variable_value as cluster_size from information_schema.global_status where variable_name='wsrep_cluster_size'").Scan(&clusterSize)
	if err != nil {
		log.Println("Cannot check cluster size:", err)
		w.WriteHeader(503)
		fmt.Fprintf(w, "503 Cannot check cluster size: %v", err)
		return
	}

	// Check if node is available
	// State 4: Synced, State 2: Donor/Desynced
	// Failsafe: if cluster size is 1, keep the last node available
	if (!dwr && state == 4) || (awd && state == 2) || (dwr && readonly == "OFF" && state == 4) || (!awd && clusterSize == 1 && state == 4) {
		w.WriteHeader(200)
		fmt.Fprint(w, "200 Galera Node is synced")
	} else {
		w.WriteHeader(503)
		fmt.Fprint(w, "503 Galera Node is not synced")
	}
}
