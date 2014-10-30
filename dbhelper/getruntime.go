// get.go
package dbhelper

import (
	_ "database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"log"
)

func GetSlaveStatus(db *sqlx.DB) map[string]interface{} {
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

func GetSlaveHosts(db *sqlx.DB) map[string]interface{} {
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

func GetSlaveHostsDiscovery(db *sqlx.DB) []string {
	hosts := []string{}
	err := db.Select(&hosts, "select host from information_schema.processlist where command ='binlog dump'")
	if err != nil {
		log.Fatal(err)
	}
	return hosts
}

func GetStatus(db *sqlx.DB) map[string]string {
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
func GetStatusAsInt(db *sqlx.DB) map[string]int64 {
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

func GetVariables(db *sqlx.DB) map[string]string {
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
