// clustercheck.go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var (
	awd    = flag.Bool("a", false, "Available when donor")
	dwr    = flag.Bool("d", false, "Disable when read_only flag is set (desirable when wanting to take a node out of the cluster without desync)")
	cnf    = flag.String("c", "~/.my.cnf", "MySQL Config file to use")
	port   = flag.Int("p", 8000, "TCP port to listen on")
	sock   = flag.String("s", "/run/mysqld/mysqld.sock", "Path to mysqld unix socket file")
	myvars map[string]string
)

func main() {
	flag.Parse()
	usr, _ := user.Current()
	dir := usr.HomeDir
	conf := *cnf
	if strings.Contains(conf, "~/") {
		conf = strings.Replace(conf, "~", dir, 1)
	}
	myvars = confParser(conf)
	if myvars["socket"] == "" {
		myvars["socket"] = *sock
	}
	dsn := fmt.Sprintf("%s:%s@unix(%s)/", myvars["user"], myvars["password"], myvars["socket"])
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		log.Fatalln(err)
		return
	}
	defer db.Close()
	httpAddr := fmt.Sprintf(":%v", *port)
	log.Printf("Listening to %v", httpAddr)
	http.HandleFunc("/", clustercheck(db))
	log.Fatalln(http.ListenAndServe(httpAddr, nil))
}

func clustercheck(db *sqlx.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		var (
			readonly string
			state    int
		)
		err := db.Ping()
		if err != nil {
			log.Println(err)
		}
		if *dwr {
			db.QueryRow("select variable_value as readonly from information_schema.global_variables where variable_name='read_only'").Scan(&readonly)
		}
		err = db.QueryRow("select variable_value as state from information_schema.global_status where variable_name='wsrep_local_state'").Scan(&state)
		if err != nil {
			w.WriteHeader(503)
			fmt.Fprintf(w, "Cannot check cluster state: %v", err)
		} else if (!*dwr && state == 4) || (*awd && state == 2) || (*dwr && readonly == "OFF" && state == 4) {
			fmt.Fprint(w, "MariaDB Cluster Node is synced.")
		} else {
			w.WriteHeader(503)
			fmt.Fprint(w, "MariaDB Cluster Node is not synced.")
		}
	}
}

func confParser(configFile string) map[string]string {
	names := []string{"user", "password", "host", "port", "socket"}
	params := make(map[string]string)
	file, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	lines := strings.Split(string(file), "\n")
	for _, line := range lines {
		for _, name := range names {
			if strings.Index(line, name) == 0 {
				res := strings.Split(line, "=")
				params[name] = strings.TrimSpace(res[1])
			}
		}
	}
	if params["user"] == "" {
		user, _ := user.Current()
		params["user"] = user.Username
	}
	return params
}
