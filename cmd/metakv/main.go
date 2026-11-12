package main

import (
	"log"
	"net/http"
	"os"

	"github.com/devotionalist/open-message/internal/metakv"
)

func main() {
	addr := env("METAKV_ADDR", ":7700")
	s := metakv.NewServer()
	log.Printf("metakv listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, s.Handler()))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
