package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

type fakeWA struct {
	mu   sync.Mutex
	sent []string
	on   func(WAMessage)
}

func (f *fakeWA) Status() WAStatus {
	return WAStatus{Linked: true, Online: true, Phone: "+5516999990000"}
}
func (f *fakeWA) Connect(context.Context) error { return nil }
func (f *fakeWA) Logout(context.Context) error  { return nil }
func (f *fakeWA) QRPNG() ([]byte, error)        { return nil, errors.New("none") }
func (f *fakeWA) OnMessage(fn func(WAMessage))  { f.on = fn }
func (f *fakeWA) Chats(context.Context, string, int) ([]WAChat, error) {
	return []WAChat{{JID: "5516988887777@s.whatsapp.net", Name: "Clínica Sorriso", Phone: "+5516988887777", LastText: "oi"}}, nil
}
func (f *fakeWA) Messages(context.Context, string, int) ([]WAMessage, error) {
	return []WAMessage{{Sender: "Ana", Text: "Tenho interesse, qual o valor?", At: time.Now()}}, nil
}
func (f *fakeWA) Resolve(_ context.Context, to string) (WAChat, error) {
	return WAChat{JID: "5516988887777@s.whatsapp.net", Name: "Clínica Sorriso", Phone: "+5516988887777"}, nil
}
func (f *fakeWA) Send(_ context.Context, jid, text string) error {
	f.mu.Lock()
	f.sent = append(f.sent, jid+"|"+text)
	f.mu.Unlock()
	return nil
}
func (f *fakeWA) Media(context.Context, string, string) (WAMedia, error) {
	return WAMedia{Kind: "image", Mime: "image/png", Caption: "cardápio", Data: []byte("\x89PNG\r\n\x1a\n0000")}, nil
}
func (f *fakeWA) sentList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

func TestWhatsAppToolsAndOwnerChannel(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s := chatServer(t, e)
	wa := &fakeWA{}
	s.SetWhatsApp(wa)
	c := agentConversation(t, s, "nico")

	// Reading needs no approval; sending waits for the owner.
	setCalls(e, "Nico", call("whatsapp_ler", map[string]any{"conversa": "Clínica Sorriso"}),
		call("whatsapp_enviar", map[string]any{"para": "Clínica Sorriso", "texto": "Olá! O valor é R$ 900."}))
	answer := sendAsync(t, s, c.ID, "responde a clínica")
	ap := waitApproval(t, s)
	if ap.Action != "whatsapp.enviar" || !strings.Contains(ap.Summary, "Clínica Sorriso (+5516988887777): «Olá! O valor é R$ 900.»") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	resp := <-answer
	if !strings.Contains(resp.Assistant.Content, "Tenho interesse") || !strings.Contains(resp.Assistant.Content, "Mensagem enviada") {
		t.Fatalf("answer: %q", resp.Assistant.Content)
	}
	if sent := wa.sentList(); len(sent) != 1 || sent[0] != "5516988887777@s.whatsapp.net|Olá! O valor é R$ 900." {
		t.Fatalf("sent: %v", sent)
	}

	// The owner writes in their own chat: the chief answers there.
	e.mu.Lock()
	e.answers["Vega"] = "Anotado, vou cuidar disso."
	e.mu.Unlock()
	wa.on(WAMessage{ID: "m1", ChatJID: "5516999990000@s.whatsapp.net", Self: true, FromMe: true, Text: "me lembra de ligar pro contador"})
	for i := 0; i < 400 && len(wa.sentList()) < 2; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if sent := wa.sentList(); len(sent) != 2 || sent[1] != "5516999990000@s.whatsapp.net|Vega: Anotado, vou cuidar disso." {
		t.Fatalf("owner channel: %v", sent)
	}

	// A reply from someone in a goal's funnel wakes the goal.
	g, _ := s.store.SaveGoal(t.Context(), store.Goal{ID: "g1", AgentID: "nico", ConversationID: c.ID, Title: "Prospecção", Outcome: "5 reuniões",
		Status: store.GoalActive, Items: []store.GoalItem{{ID: "i1", Name: "Clínica Sorriso", Stage: "contatado", Contact: "(16) 98888-7777"}}}, store.ActorSystem, s.now())
	later := s.now().Add(5 * time.Hour)
	g.NextWake = &later
	_, _ = s.store.SaveGoal(t.Context(), g, store.ActorSystem, s.now())
	s.whatsappIncoming(WAMessage{ID: "m2", Phone: "+5516988887777", Text: "Pode ser quinta às 15h?"})
	got, _ := s.store.GetGoal(t.Context(), "g1")
	if got.NextWake == nil || got.NextWake.After(s.now()) || !strings.Contains(got.WakeReason, "Clínica Sorriso respondeu no WhatsApp: «Pode ser quinta às 15h?»") {
		t.Fatalf("goal: %+v", got)
	}
}

func TestWhatsAppMediaBecomesAttachment(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s := chatServer(t, e)
	s.SetWhatsApp(&fakeWA{})
	got := s.waMessageContent(t.Context(), WAMessage{ID: "ABCDEF123456", ChatJID: "x@s.whatsapp.net", Text: "[imagem] cardápio", Media: "image"}, "nico", "")
	ids := fileIDs(got)
	if !strings.HasPrefix(got, "cardápio\n\n![whatsapp-foto-ef123456.png](avora:file/") || len(ids) != 1 {
		t.Fatalf("content: %q", got)
	}
	if f, err := s.store.GetFile(t.Context(), ids[0]); err != nil || f.Mime != "image/png" {
		t.Fatalf("file: %+v %v", f, err)
	}
	// No ElevenLabs: a voice note says how to get it transcribed.
	if _, err := s.transcribe(t.Context(), "nico", store.File{Name: "a.ogg", Mime: "audio/ogg", Data: []byte("x")}); err == nil || !strings.Contains(err.Error(), "ElevenLabs") {
		t.Fatalf("transcribe: %v", err)
	}
}

func TestSamePhone(t *testing.T) {
	if !samePhone("(16) 98888-7777", "+5516988887777") || !samePhone("16 8888-7777", "+5516988887777") || samePhone("123", "123") {
		t.Fatal("samePhone")
	}
}
