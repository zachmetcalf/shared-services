package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": "hello",
		})
	})
	return mux
}

func main() {
	addr := os.Getenv("HELLO_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	log.Printf("hello listening addr=%s", addr)
	if err := http.ListenAndServe(addr, newHandler()); err != nil {
		log.Fatal(err)
	}
}
