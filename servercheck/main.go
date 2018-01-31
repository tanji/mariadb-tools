// servercheck.go
// check for server health and return http 503 or 200
// for use with load balancers (haproxy, aws...)

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	osuser "os/user"
	"strings"

	"github.com/go-ini/ini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/tanji/mariadb-tools/dbhelper"
)

var (
	cnffile     string
	port        int
	maxdelay    int64
	sockopt     string
	hostopt     string
	portopt     string
	user        string
	password    string
	mysqlsocket string
	mysqlhost   string
	mysqlport   string
)

func init() {
	flag.StringVar(&cnffile, "config", "~/.my.cnf", "MySQL Config file to use")
	flag.IntVar(&port, "port", 8000, "TCP port to listen on")
	flag.StringVar(&sockopt, "mysql-socket", "/run/mysqld/mysqld.sock", "Path to unix socket of monitored MySQL instance")
	flag.StringVar(&hostopt, "mysql-host", "", "Hostname or IP address of monitored MySQL instance")
	flag.StringVar(&portopt, "mysql-port", "3306", "Port of monitored MySQL instance")

	flag.Int64Var(&maxdelay, "maxdelay", 5, "Max replication delay to keep server in LB")
}

func main() {
	flag.Parse()

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

	ss, err := dbhelper.GetSlaveStatus(db)
	if err != nil {
		log.Println("Couldn't get Slave Status:", err)
		w.WriteHeader(503)
		fmt.Fprint(w, "503 No Replication")
		return
	}

	if !ss.Seconds_Behind_Master.Valid {
		// value is null. Replication is broken or stopped
		w.WriteHeader(503)
		fmt.Fprint(w, "503 Broken Replication")
		return
	}
	delay := ss.Seconds_Behind_Master.Int64
	if delay > maxdelay {
		w.WriteHeader(503)
		fmt.Fprintf(w, "503 Delayed Replication (%d)", delay)
		return
	}
	w.WriteHeader(200)
	fmt.Fprint(w, "200 Health OK")
	return
}
