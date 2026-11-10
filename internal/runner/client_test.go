package runner

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/GuardaChuva22/avora/internal/control"
 "github.com/GuardaChuva22/avora/internal/scheduler"
)

const testAdmin="admin-test-token-abcdefghijklmnopqrstuvwxyz1234"
const testDevice="device-test-token-abcdefghijklmnopqrstuvwxyz1234"

func TestRunnerEndToEnd(t *testing.T) {
 core,err:=control.NewServer(testAdmin,map[string]control.DeviceCredential{
  "my-pc":{Token:testDevice,Target:scheduler.Windows},
 })
 if err!=nil{t.Fatal(err)}
 server:=httptest.NewServer(core.Handler())
 defer server.Close()
 c,err:=NewClient(server.URL,"my-pc",testDevice)
 if err!=nil{t.Fatal(err)}
 ctx:=context.Background()
 if err:=c.Heartbeat(ctx);err!=nil{t.Fatal(err)}
 req,err:=http.NewRequest(http.MethodPost,server.URL+"/v1/jobs",nil)
 if err!=nil{t.Fatal(err)}
 // Submit a typed action through admin API, not arbitrary command.
 // Use explicit JSON body in a separate request.
 _=req
 jobReq,err:=http.NewRequest(http.MethodPost,server.URL+"/v1/jobs",stringsReader(`{"target":"windows","action":"ping"}`))
 if err!=nil{t.Fatal(err)}
 jobReq.Header.Set("Authorization","Bearer "+testAdmin)
 jobReq.Header.Set("Content-Type","application/json")
 res,err:=http.DefaultClient.Do(jobReq)
 if err!=nil{t.Fatal(err)}
 var job control.Job
 if err:=json.NewDecoder(res.Body).Decode(&job);err!=nil{t.Fatal(err)}
 res.Body.Close()
 if res.StatusCode!=http.StatusCreated{t.Fatalf("submit: %d",res.StatusCode)}
 if err:=c.Tick(ctx);err!=nil{t.Fatal(err)}
 statusReq,err:=http.NewRequest(http.MethodGet,server.URL+"/v1/jobs/"+job.ID,nil)
 if err!=nil{t.Fatal(err)}
 statusReq.Header.Set("Authorization","Bearer "+testAdmin)
 statusResp,err:=http.DefaultClient.Do(statusReq)
 if err!=nil{t.Fatal(err)}
 defer statusResp.Body.Close()
 if err:=json.NewDecoder(statusResp.Body).Decode(&job);err!=nil{t.Fatal(err)}
 if job.Status!="succeeded"||job.Output!="pong from my-pc"{t.Fatalf("job did not complete: %+v",job)}
}

func TestRefusePlaintextRemote(t *testing.T) {
 if _,err:=NewClient("http://example.org:8080","pc",testDevice);err==nil {t.Fatal("accepted insecure remote HTTP")}
 if _,err:=NewClient("https://example.org/evil","pc",testDevice);err==nil {t.Fatal("accepted URL path")}
}

func stringsReader(s string) *bytes.Reader {return bytes.NewReader([]byte(s))}
