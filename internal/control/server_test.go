package control

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/GuardaChuva22/avora/internal/scheduler"
)

const adminTestToken="admin-test-token-abcdefghijklmnopqrstuvwxyz1234"
const pcTestToken="pc-test-token-abcdefghijklmnopqrstuvwxyz123456"
const vpsTestToken="vps-test-token-abcdefghijklmnopqrstuvwxyz12345"

func makeServer(t *testing.T) *Server {
 t.Helper()
 s,err:=NewServer(adminTestToken,map[string]DeviceCredential{
  "pc":{Token:pcTestToken,Target:scheduler.Windows},
  "server":{Token:vpsTestToken,Target:scheduler.VPS},
 })
 if err!=nil {t.Fatal(err)}
 return s
}

func request(t *testing.T,s *Server,method,path,token,deviceID string,body any) *httptest.ResponseRecorder {
 t.Helper()
 var b []byte
 if body!=nil {var err error;b,err=json.Marshal(body);if err!=nil{t.Fatal(err)}}
 r:=httptest.NewRequest(method,path,bytes.NewReader(b))
 if token!="" {r.Header.Set("Authorization","Bearer "+token)}
 if deviceID!="" {r.Header.Set("X-Avora-Device-ID",deviceID)}
 w:=httptest.NewRecorder()
 s.Handler().ServeHTTP(w,r)
 return w
}

func TestAuthAndDevicePresence(t *testing.T) {
 s:=makeServer(t)
 if got:=request(t,s,"GET","/v1/devices","","",nil);got.Code!=401 {t.Fatalf("missing admin auth: %d",got.Code)}
 if got:=request(t,s,"POST","/v1/runtime/heartbeat",vpsTestToken,"pc",nil);got.Code!=401 {t.Fatalf("credential swap accepted: %d",got.Code)}
 if got:=request(t,s,"POST","/v1/runtime/heartbeat",pcTestToken,"pc",nil);got.Code!=200 {t.Fatalf("heartbeat failed: %d",got.Code)}
 response:=request(t,s,"GET","/v1/devices",adminTestToken,"",nil)
 var devices []DeviceStatus
 if err:=json.Unmarshal(response.Body.Bytes(),&devices);err!=nil{t.Fatal(err)}
 if len(devices)!=2||!devices[0].Online||devices[1].Online{t.Fatalf("wrong online state: %+v",devices)}
 s.mu.Lock()
 s.lastSeen["pc"]=time.Now().Add(-OnlineTTL-time.Second)
 s.mu.Unlock()
 response=request(t,s,"GET","/v1/devices",adminTestToken,"",nil)
 if err:=json.Unmarshal(response.Body.Bytes(),&devices);err!=nil{t.Fatal(err)}
 if devices[0].Online {t.Fatal("stale heartbeat still online")}
}

func TestJobLifecycleAndFallback(t *testing.T) {
 s:=makeServer(t)
 // Only VPS online => automatic job goes to VPS.
 request(t,s,"POST","/v1/runtime/heartbeat",vpsTestToken,"server",nil)
 response:=request(t,s,"POST","/v1/jobs",adminTestToken,"",JobRequest{Target:"auto",Action:"ping"})
 if response.Code!=201 {t.Fatalf("submit status=%d: %s",response.Code,response.Body.String())}
 var job Job
 if err:=json.Unmarshal(response.Body.Bytes(),&job);err!=nil{t.Fatal(err)}
 if job.DeviceID!="server"||job.Target!=scheduler.VPS||job.Status!="queued" {t.Fatalf("unexpected job: %+v",job)}
 if got:=request(t,s,"POST","/v1/runtime/claim",pcTestToken,"pc",nil);got.Code!=409 {t.Fatalf("offline worker claim: %d",got.Code)}
 response=request(t,s,"POST","/v1/runtime/claim",vpsTestToken,"server",nil)
 if response.Code!=200 {t.Fatalf("claim status: %d",response.Code)}
 if got:=request(t,s,"POST","/v1/runtime/jobs/"+job.ID+"/complete",pcTestToken,"pc",JobResult{Success:true});got.Code!=403 {t.Fatalf("cross worker complete: %d",got.Code)}
 response=request(t,s,"POST","/v1/runtime/jobs/"+job.ID+"/complete",vpsTestToken,"server",JobResult{Success:true,Output:"pong"})
 if response.Code!=200 {t.Fatalf("completion status=%d",response.Code)}
 response=request(t,s,"GET","/v1/jobs/"+job.ID,adminTestToken,"",nil)
 if err:=json.Unmarshal(response.Body.Bytes(),&job);err!=nil{t.Fatal(err)}
 if job.Status!="succeeded"||job.Output!="pong"||job.CompletedAt==nil {t.Fatalf("bad completion: %+v",job)}
 if got:=request(t,s,"POST","/v1/runtime/jobs/"+job.ID+"/complete",vpsTestToken,"server",JobResult{Success:true});got.Code!=409 {t.Fatalf("duplicate completion: %d",got.Code)}
 // PC joins and wins the next automatic assignment.
 request(t,s,"POST","/v1/runtime/heartbeat",pcTestToken,"pc",nil)
 response=request(t,s,"POST","/v1/jobs",adminTestToken,"",JobRequest{Target:"auto",Action:"diagnostics"})
 if err:=json.Unmarshal(response.Body.Bytes(),&job);err!=nil{t.Fatal(err)}
 if job.DeviceID!="pc"||job.Target!=scheduler.Windows {t.Fatalf("PC not preferred: %+v",job)}
}

func TestAllowlistAndManualTarget(t *testing.T) {
 s:=makeServer(t)
 for _,action:=range []string{"shell","rm -rf","codex"} {
  got:=request(t,s,"POST","/v1/jobs",adminTestToken,"",JobRequest{Action:action})
  if got.Code!=400 {t.Fatalf("%s was accepted: %d",action,got.Code)}
 }
 if got:=request(t,s,"POST","/v1/jobs",adminTestToken,"",JobRequest{Target:"windows",Action:"ping"});got.Code!=409 {
  t.Fatalf("offline manual target accepted: %d",got.Code)
 }
 if got:=request(t,s,"POST","/v1/jobs",adminTestToken,"",JobRequest{Target:"provider_cloud",Action:"ping"});got.Code!=400 {
  t.Fatalf("unimplemented cloud target accepted: %d",got.Code)
 }
}

func TestTokenValidation(t *testing.T) {
 _,err:=NewServer("short",map[string]DeviceCredential{"pc":{Token:pcTestToken,Target:scheduler.Windows}})
 if err==nil{t.Fatal("short admin token accepted")}
 _,err=NewServer(adminTestToken,map[string]DeviceCredential{"pc":{Token:"short",Target:scheduler.Windows}})
 if err==nil{t.Fatal("short device token accepted")}
}
