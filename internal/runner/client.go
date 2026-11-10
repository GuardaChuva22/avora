// Package runner implements the Avora runtime's narrowly scoped task protocol.
// It deliberately does not execute shell commands or arbitrary code.
package runner

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "runtime"
 "strings"
 "time"

 "github.com/GuardaChuva22/avora/internal/control"
)

type Client struct {
 base string
 id string
 token string
 http *http.Client
}

func NewClient(rawURL,id,token string) (*Client,error) {
 u,err:=url.Parse(rawURL)
 if err!=nil||u==nil||u.Host=="" {return nil,errors.New("invalid AVORA_SERVER_URL")}
 if u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.Path!=""&&u.Path!="/" {return nil,errors.New("server URL must not contain credentials, path or query")}
 if u.Scheme!="https" {
  host:=u.Hostname()
  ip:=net.ParseIP(host)
  if u.Scheme!="http"||(host!="localhost"&&(ip==nil||!ip.IsLoopback())) {return nil,errors.New("remote server requires HTTPS")}
 }
 if id==""||len(token)<32 {return nil,errors.New("device ID and token (32+ characters) required")}
 return &Client{base:strings.TrimSuffix(rawURL,"/"),id:id,token:token,http:&http.Client{Timeout:10*time.Second}},nil
}

func (c *Client) post(ctx context.Context,path string,body any) (*http.Response,error) {
 var reader io.Reader
 if body!=nil {
  payload,err:=json.Marshal(body)
  if err!=nil{return nil,err}
  reader=bytes.NewReader(payload)
 }
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.base+path,reader)
 if err!=nil{return nil,err}
 req.Header.Set("Authorization","Bearer "+c.token)
 req.Header.Set("X-Avora-Device-ID",c.id)
 if body!=nil {req.Header.Set("Content-Type","application/json")}
 return c.http.Do(req)
}

func (c *Client) Heartbeat(ctx context.Context) error {
 resp,err:=c.post(ctx,"/v1/runtime/heartbeat",nil)
 if err!=nil{return err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return fmt.Errorf("heartbeat status %d",resp.StatusCode)}
 return nil
}
func (c *Client) Claim(ctx context.Context) (*control.Job,error) {
 resp,err:=c.post(ctx,"/v1/runtime/claim",nil)
 if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode==http.StatusNoContent{return nil,nil}
 if resp.StatusCode!=http.StatusOK{return nil,fmt.Errorf("claim status %d",resp.StatusCode)}
 var job control.Job
 if err:=json.NewDecoder(io.LimitReader(resp.Body,8192)).Decode(&job);err!=nil{return nil,err}
 if job.DeviceID!=c.id{return nil,errors.New("job assigned to unexpected device")}
 return &job,nil
}
func (c *Client) Complete(ctx context.Context,jobID string,result control.JobResult) error {
 if jobID==""||strings.ContainsAny(jobID,"/\\?#") {return errors.New("invalid job ID")}
 resp,err:=c.post(ctx,"/v1/runtime/jobs/"+jobID+"/complete",result)
 if err!=nil{return err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return fmt.Errorf("completion status %d",resp.StatusCode)}
 return nil
}
func Execute(job control.Job) control.JobResult {
 switch job.Action {
 case "ping":
  return control.JobResult{Success:true,Output:"pong from "+job.DeviceID}
 case "diagnostics":
  hostname,err:=os.Hostname()
  if err!=nil {return control.JobResult{Success:false,Error:"unable to read hostname"}}
  return control.JobResult{Success:true,Output:fmt.Sprintf("host=%s os=%s arch=%s",hostname,runtime.GOOS,runtime.GOARCH)}
 default:
  return control.JobResult{Success:false,Error:"unsupported action"}
 }
}
func (c *Client) Tick(ctx context.Context) error {
 if err:=c.Heartbeat(ctx);err!=nil{return err}
 job,err:=c.Claim(ctx)
 if err!=nil{return err}
 if job==nil{return nil}
 return c.Complete(ctx,job.ID,Execute(*job))
}
func (c *Client) Run(ctx context.Context,report func(error)) {
 ticker:=time.NewTicker(5*time.Second)
 defer ticker.Stop()
 for {
  if err:=c.Tick(ctx);err!=nil&&ctx.Err()==nil&&report!=nil {report(err)}
  select {
  case <-ctx.Done():return
  case <-ticker.C:
  }
 }
}
