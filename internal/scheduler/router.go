// Package scheduler contains execution-target policy, not command execution.
package scheduler

type Engine string
type Target string

const (
 Native Engine="native"
 Codex Engine="codex"
 Claude Engine="claude"
 Windows Target="windows"
 ProviderCloud Target="provider_cloud"
 VPS Target="vps"
)

type Request struct {
 Engine Engine
 Preferred Target
 ComplexDevelopment bool
 RequiresLocalFiles bool
}
type Availability struct {
 WindowsOnline bool
 ProviderCloudReady bool
 VPSReady bool
 FilesAvailableInCloud bool
 FilesAvailableOnVPS bool
}
type Decision struct {
 Engine Engine `json:"engine"`
 Target Target `json:"target"`
 Reason string `json:"reason"`
 Allowed bool `json:"allowed"`
}
func Route(req Request,a Availability) Decision {
 engine:=req.Engine
 if engine=="" {engine=Native}
 eligible:=func(target Target) bool {
  switch target {
  case Windows:return a.WindowsOnline
  case ProviderCloud:return a.ProviderCloudReady&&(!req.RequiresLocalFiles||a.FilesAvailableInCloud)
  case VPS:return a.VPSReady&&(!req.RequiresLocalFiles||a.FilesAvailableOnVPS)
  default:return false
  }
 }
 result:=func(target Target,reason string,allowed bool) Decision {return Decision{engine,target,reason,allowed}}
 if req.Preferred!="" {
  if eligible(req.Preferred) {return result(req.Preferred,"manual target selected",true)}
  return result(req.Preferred,"requested target unavailable or files inaccessible",false)
 }
 if eligible(Windows) {return result(Windows,"Windows preferred when online",true)}
 if (engine==Codex||engine==Claude)&&eligible(ProviderCloud) {return result(ProviderCloud,"provider cloud available",true)}
 if eligible(VPS) {return result(VPS,"VPS fallback",true)}
 if eligible(ProviderCloud) {return result(ProviderCloud,"cloud fallback",true)}
 return result("","no authorized environment with required files",false)
}
