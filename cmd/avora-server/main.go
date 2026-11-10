package main

import (
 "encoding/json"
 "log"
 "net/http"
 "os"
 "time"
)

func main() {
 mux := http.NewServeMux()
 mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
  w.Header().Set("Content-Type", "application/json")
  _ = json.NewEncoder(w).Encode(map[string]string{"status":"ok","service":"avora-server"})
 })
 mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
  w.Header().Set("Content-Type", "application/json")
  _ = json.NewEncoder(w).Encode(map[string]interface{}{"status":"scaffold","execution_enabled":false,"engines":[]string{"native","codex","claude"},"targets":[]string{"windows","provider_cloud","vps"}})
 })
 address:=os.Getenv("AVORA_LISTEN_ADDR")
 if address=="" { address="127.0.0.1:8080" }
 server:=&http.Server{Addr:address,Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second,IdleTimeout:60*time.Second}
 log.Printf("Avora development API on %s; task execution disabled",address)
 log.Fatal(server.ListenAndServe())
}
