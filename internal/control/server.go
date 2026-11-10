// Package control implements Avora's authenticated proof-of-concept control plane.
// All executable jobs are strictly allowlisted; arbitrary commands are not supported.
package control

import (
 "crypto/rand"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "sort"
 "strings"
 "sync"
 "time"

 "github.com/GuardaChuva22/avora/internal/scheduler"
)

const OnlineTTL = 30*time.Second

type DeviceCredential struct {
 Token string `json:"token"`
 Target scheduler.Target `json:"target"`
}

type DeviceStatus struct {
 ID string `json:"id"`
 Target scheduler.Target `json:"target"`
 Online bool `json:"online"`
 LastSeen *time.Time `json:"last_seen,omitempty"`
}

type Job struct {
 ID string `json:"id"`
 Action string `json:"action"`
 Target scheduler.Target `json:"target"`
 DeviceID string `json:"device_id"`
 Status string `json:"status"`
 Output string `json:"output,omitempty"`
 Error string `json:"error,omitempty"`
 CreatedAt time.Time `json:"created_at"`
 CompletedAt *time.Time `json:"completed_at,omitempty"`
}
type JobRequest struct {
 Target string `json:"target"`
 Action string `json:"action"`
}
type JobResult struct {
 Success bool `json:"success"`
 Output string `json:"output"`
 Error string `json:"error"`
}

type Server struct {
 mu sync.Mutex
 adminToken string
 credentials map[string]DeviceCredential
 lastSeen map[string]time.Time
 jobs map[string]*Job
 order []string
 now func() time.Time
}

func NewServer(adminToken string, credentials map[string]DeviceCredential) (*Server,error) {
 if len(adminToken)<32 {return nil,errors.New("AVORA_ADMIN_TOKEN must have at least 32 characters")}
 if len(credentials)==0 {return nil,errors.New("configure at least one device")}
 copyCredentials:=make(map[string]DeviceCredential,len(credentials))
 for id,c:=range credentials {
  if id==""||strings.TrimSpace(id)!=id||len(c.Token)<32 {return nil,errors.New("invalid device ID or token (minimum 32 characters)")}
  if c.Target!=scheduler.Windows&&c.Target!=scheduler.VPS {return nil,errors.New("unsupported device target")}
  if secureEqual(c.Token,adminToken) {return nil,errors.New("admin token and device token must differ")}
  copyCredentials[id]=c
 }
 return &Server{adminToken:adminToken,credentials:copyCredentials,lastSeen:make(map[string]time.Time),jobs:make(map[string]*Job),now:time.Now},nil
}

func secureEqual(a,b string) bool {
 return subtle.ConstantTimeCompare([]byte(a),[]byte(b))==1
}

func (s *Server) Handler() http.Handler {
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,map[string]string{"status":"ok","service":"avora-server"})})
 mux.HandleFunc("GET /v1/capabilities",s.admin(s.capabilities))
 mux.HandleFunc("GET /v1/devices",s.admin(s.devices))
 mux.HandleFunc("POST /v1/jobs",s.admin(s.submitJob))
 mux.HandleFunc("GET /v1/jobs/{id}",s.admin(s.getJob))
 mux.HandleFunc("POST /v1/runtime/heartbeat",s.device(s.heartbeat))
 mux.HandleFunc("POST /v1/runtime/claim",s.device(s.claim))
 mux.HandleFunc("POST /v1/runtime/jobs/{id}/complete",s.device(s.completeJob))
 return mux
}

func bearer(r *http.Request) string {
 h:=r.Header.Get("Authorization")
 if !strings.HasPrefix(h,"Bearer ") {return ""}
 return strings.TrimPrefix(h,"Bearer ")
}
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
 return func(w http.ResponseWriter,r *http.Request) {
  if !secureEqual(bearer(r),s.adminToken) {http.Error(w,"unauthorized",http.StatusUnauthorized);return}
  next(w,r)
 }
}
func (s *Server) device(next func(http.ResponseWriter,*http.Request,string)) http.HandlerFunc {
 return func(w http.ResponseWriter,r *http.Request) {
  id:=r.Header.Get("X-Avora-Device-ID")
  credential,ok:=s.credentials[id]
  if !ok||!secureEqual(bearer(r),credential.Token) {http.Error(w,"unauthorized",http.StatusUnauthorized);return}
  next(w,r,id)
 }
}
func writeJSON(w http.ResponseWriter,status int,v any) {
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Cache-Control","no-store")
 w.WriteHeader(status)
 _=json.NewEncoder(w).Encode(v)
}
func readJSON(w http.ResponseWriter,r *http.Request,out any) bool {
 r.Body=http.MaxBytesReader(w,r.Body,8192)
 defer r.Body.Close()
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(out);err!=nil {http.Error(w,"invalid JSON payload",http.StatusBadRequest);return false}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF {http.Error(w,"unexpected content after JSON",http.StatusBadRequest);return false}
 return true
}
func (s *Server) isOnline(id string) bool {
 seen,ok:=s.lastSeen[id]
 return ok && s.now().Sub(seen)<OnlineTTL
}
func (s *Server) capabilities(w http.ResponseWriter,_ *http.Request) {
 writeJSON(w,http.StatusOK,map[string]any{"status":"proof_of_concept","execution_enabled":true,"allowed_actions":[]string{"ping","diagnostics"},"engines":[]string{"native"},"targets":[]string{"windows","vps"},"persistence":false})
}
func (s *Server) devices(w http.ResponseWriter,_ *http.Request) {
 s.mu.Lock()
 defer s.mu.Unlock()
 result:=make([]DeviceStatus,0,len(s.credentials))
 for id,c:=range s.credentials {
  status:=DeviceStatus{ID:id,Target:c.Target,Online:s.isOnline(id)}
  if seen,ok:=s.lastSeen[id];ok {at:=seen;status.LastSeen=&at}
  result=append(result,status)
 }
 sort.Slice(result,func(i,j int) bool{return result[i].ID<result[j].ID})
 writeJSON(w,http.StatusOK,result)
}
func (s *Server) heartbeat(w http.ResponseWriter,_ *http.Request,id string) {
 s.mu.Lock()
 s.lastSeen[id]=s.now()
 s.mu.Unlock()
 writeJSON(w,http.StatusOK,map[string]string{"status":"online","device_id":id})
}
func (s *Server) onlineDevice(target scheduler.Target) string {
 ids:=make([]string,0)
 for id,c:=range s.credentials {
  if c.Target==target&&s.isOnline(id) {ids=append(ids,id)}
 }
 sort.Strings(ids)
 if len(ids)==0{return ""}
 return ids[0]
}
func (s *Server) submitJob(w http.ResponseWriter,r *http.Request) {
 var req JobRequest
 if !readJSON(w,r,&req){return}
 if req.Action!="ping"&&req.Action!="diagnostics" {http.Error(w,"action not allowlisted",http.StatusBadRequest);return}
 if req.Target!=""&&req.Target!="auto"&&req.Target!="windows"&&req.Target!="vps" {http.Error(w,"unsupported target",http.StatusBadRequest);return}
 s.mu.Lock()
 defer s.mu.Unlock()
 windows:=s.onlineDevice(scheduler.Windows)
 vps:=s.onlineDevice(scheduler.VPS)
 routing:=scheduler.Request{Engine:scheduler.Native}
 if req.Target!=""&&req.Target!="auto" {routing.Preferred=scheduler.Target(req.Target)}
 decision:=scheduler.Route(routing,scheduler.Availability{WindowsOnline:windows!="",VPSReady:vps!=""})
 if !decision.Allowed {http.Error(w,"requested device unavailable",http.StatusConflict);return}
 assigned:=windows
 if decision.Target==scheduler.VPS {assigned=vps}
 bytes:=make([]byte,12)
 if _,err:=rand.Read(bytes);err!=nil {http.Error(w,"failed to create job",http.StatusInternalServerError);return}
 id:=hex.EncodeToString(bytes)
 job:=&Job{ID:id,Action:req.Action,Target:decision.Target,DeviceID:assigned,Status:"queued",CreatedAt:s.now().UTC()}
 s.jobs[id]=job
 s.order=append(s.order,id)
 writeJSON(w,http.StatusCreated,job)
}
func (s *Server) getJob(w http.ResponseWriter,r *http.Request) {
 s.mu.Lock()
 defer s.mu.Unlock()
 job,ok:=s.jobs[r.PathValue("id")]
 if !ok {http.Error(w,"not found",http.StatusNotFound);return}
 writeJSON(w,http.StatusOK,*job)
}
func (s *Server) claim(w http.ResponseWriter,_ *http.Request,id string) {
 s.mu.Lock()
 defer s.mu.Unlock()
 if !s.isOnline(id) {http.Error(w,"heartbeat required",http.StatusConflict);return}
 for _,jobID:=range s.order {
  job:=s.jobs[jobID]
  if job.Status=="queued"&&job.DeviceID==id {
   job.Status="running"
   writeJSON(w,http.StatusOK,*job)
   return
  }
 }
 w.WriteHeader(http.StatusNoContent)
}
func (s *Server) completeJob(w http.ResponseWriter,r *http.Request,id string) {
 var result JobResult
 if !readJSON(w,r,&result){return}
 if len(result.Output)>4096||len(result.Error)>1024 {http.Error(w,"result too large",http.StatusBadRequest);return}
 s.mu.Lock()
 defer s.mu.Unlock()
 job,ok:=s.jobs[r.PathValue("id")]
 if !ok {http.Error(w,"not found",http.StatusNotFound);return}
 if job.DeviceID!=id {http.Error(w,"job belongs to another device",http.StatusForbidden);return}
 if job.Status!="running" {http.Error(w,"job not running",http.StatusConflict);return}
 if result.Success {job.Status="succeeded"}else{job.Status="failed"}
 job.Output=result.Output
 job.Error=result.Error
 now:=s.now().UTC()
 job.CompletedAt=&now
 writeJSON(w,http.StatusOK,*job)
}
