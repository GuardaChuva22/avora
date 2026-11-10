package main

import (
 "context"
 "log"
 "os"
 "os/signal"
 "syscall"

 "github.com/GuardaChuva22/avora/internal/runner"
)

func main() {
 client,err:=runner.NewClient(os.Getenv("AVORA_SERVER_URL"),os.Getenv("AVORA_DEVICE_ID"),os.Getenv("AVORA_DEVICE_TOKEN"))
 if err!=nil {log.Fatal(err)}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
 defer stop()
 log.Printf("Avora runtime started for device %q; only ping/diagnostics are allowed",os.Getenv("AVORA_DEVICE_ID"))
 client.Run(ctx,func(err error){log.Printf("runtime error: %v",err)})
}
