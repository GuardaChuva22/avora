package scheduler

import "testing"

func TestWindowsPriority(t *testing.T) {
 got:=Route(Request{Engine:Codex,ComplexDevelopment:true},Availability{WindowsOnline:true,ProviderCloudReady:true,VPSReady:true})
 if !got.Allowed||got.Target!=Windows {t.Fatalf("unexpected decision %+v",got)}
}
func TestCloudFallback(t *testing.T) {
 got:=Route(Request{Engine:Claude},Availability{ProviderCloudReady:true,VPSReady:true})
 if !got.Allowed||got.Target!=ProviderCloud {t.Fatalf("unexpected decision %+v",got)}
}
func TestLocalFilesNotOnCloud(t *testing.T) {
 got:=Route(Request{Engine:Codex,RequiresLocalFiles:true},Availability{ProviderCloudReady:true,VPSReady:true})
 if got.Allowed {t.Fatalf("unsafe routing %+v",got)}
}
func TestManualNeverSilentlyFallback(t *testing.T) {
 got:=Route(Request{Engine:Codex,Preferred:Windows},Availability{ProviderCloudReady:true})
 if got.Allowed||got.Target!=Windows {t.Fatalf("unexpected decision %+v",got)}
}
func TestVPSFallback(t *testing.T) {
 got:=Route(Request{Engine:Codex},Availability{VPSReady:true})
 if !got.Allowed||got.Target!=VPS {t.Fatalf("unexpected decision %+v",got)}
}
