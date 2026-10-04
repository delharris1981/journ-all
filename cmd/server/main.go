package main

import (
	"log"
	"net/http"
	"os"

	"journall/internal/db"
	"journall/internal/handlers"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "/data/journall.db"
	}
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	h := handlers.New(database)
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, h.Routes()))
}
