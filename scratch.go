package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

func main() {
	url := "postgresql://danzh_user:owx1KyJDQPef88ZaHlv3uck6pxSPIq16@dpg-d7glp2nlk1mc7398ck50-a.oregon-postgres.render.com/danzh"
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(context.Background(), "SELECT id, schedule_open, schedule_close FROM salon_settings")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var start, end string
		err = rows.Scan(&id, &start, &end)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("id: %d, schedule_open: %s, schedule_close: %s\n", id, start, end)
	}
}
