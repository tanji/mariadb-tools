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
	cnffile  string
	port     int
	maxdelay int64
	sockopt  string
	user     string
	password string
	socket   string
)

func init() {
	flag.StringVar(&cnffile, "config", "~/.my.cnf", "MySQL Config file to use")
	flag.IntVar(&port, "port", 8000, "TCP port to listen on")
	flag.StringVar(&sockopt, "socket", "/run/mysqld/mysqld.sock", "Path to mysqld unix socket file")
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

	socket = section.Key("socket").String()
	if socket == "" {
		socket = sockopt
	}

	httpAddr := fmt.Sprintf(":%v", port)

	log.Printf("Listening to %v", httpAddr)
	http.HandleFunc("/", clustercheck)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}

func clustercheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "text/html")
	dsn := fmt.Sprintf("%s:%s@unix(%s)/", user, password, socket)

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
