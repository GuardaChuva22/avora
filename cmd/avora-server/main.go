package main

import (
 "encoding/json"
 "log"
 "net"
 "net/http"
 "os"
 "time"

 "github.com/GuardaChuva22/avora/internal/control"
)

func main() {
 address:=os.Getenv("AVORA_LISTEN_ADDR")
 if address=="" {address="127.0.0.1:8080"}
 host,_,err:=net.SplitHostPort(address)
 if err!=nil {log.Fatal("invalid AVORA_LISTEN_ADDR: ",err)}
 ip:=net.ParseIP(host)
 if host!="localhost"&&(ip==nil||!ip.IsLoopback()) {
  log.Fatal("Avora POC must bind to loopback; use an authenticated TLS reverse proxy or SSH tunnel")
 }
 var credentials map[string]control.DeviceCredential
 if err:=json.Unmarshal([]byte(os.Getenv("AVORA_DEVICES_JSON")),&credentials);err!=nil {
  log.Fatal("invalid AVORA_DEVICES_JSON: ",err)
 }
 service,err:=control.NewServer(os.Getenv("AVORA_ADMIN_TOKEN"),credentials)
 if err!=nil {log.Fatal(err)}
 server:=&http.Server{Addr:address,Handler:service.Handler(),ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second,IdleTimeout:60*time.Second}
 log.Printf("Avora control plane on %s: allowlisted ping/diagnostics only; state is in memory",address)
 log.Fatal(server.ListenAndServe())
}
