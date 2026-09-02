// Span — учёт времени, который заканчивается счётом.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dripips/span/internal/store"
	"github.com/dripips/span/internal/web"
)

func main() {
	addr := flag.String("addr", env("SPAN_ADDR", ":8080"), "адрес прослушивания")
	dbPath := flag.String("db", env("SPAN_DB", "span.db"), "файл базы SQLite")
	email := flag.String("email", env("SPAN_EMAIL", "you@example.com"), "почта владельца при первом запуске")
	password := flag.String("password", env("SPAN_PASSWORD", "span"), "пароль владельца при первом запуске")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	defer db.Close()

	hash, err := web.Hash(*password)
	if err != nil {
		log.Fatalf("пароль: %v", err)
	}
	created, err := db.EnsureUser(*email, hash)
	if err != nil {
		log.Fatalf("владелец: %v", err)
	}
	if created {
		log.Printf("создан владелец %s", *email)
	}

	srv, err := web.New(db)
	if err != nil {
		log.Fatalf("шаблоны: %v", err)
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Span слушает %s, база %s", *addr, *dbPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
