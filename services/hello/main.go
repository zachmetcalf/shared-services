package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

const kDefaultPort = "8081"

func pingHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "hello",
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = kDefaultPort
	}

	addr := port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	http.HandleFunc("/ping", pingHandler)

	log.Printf("hello listening, addr=%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Printf("hello stopped, err=%v", err)
	}
}
