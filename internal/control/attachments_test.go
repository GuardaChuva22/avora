package control

import (
	"archive/zip"
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func upload(t *testing.T, s *Server, name, mime string, data []byte) store.File {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/files?name="+name+"&conversation=c1", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+adminTestToken)
	r.Header.Set("Content-Type", mime)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("upload %s: %d %s", name, w.Code, w.Body.String())
	}
	return decode[store.File](t, w)
}

func TestAttachmentsReachTheAgent(t *testing.T) {
	s := chatServer(t, nil)
	s.workspace = t.TempDir()
	csv := upload(t, s, "clientes.csv", "text/csv", []byte("nome,cidade\nAna,Paraíso\n"))
	var doc bytes.Buffer
	zw := zip.NewWriter(&doc)
	fw, _ := zw.Create("word/document.xml")
	_, _ = fw.Write([]byte(`<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>Proposta</w:t></w:r></w:p><w:p><w:r><w:t>R$ 900 por mês</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	docx := upload(t, s, "proposta.docx", "application/octet-stream", doc.Bytes())
	png := upload(t, s, "print.png", "image/png", []byte("\x89PNG\r\n\x1a\n fake"))
	fake := upload(t, s, "x.png", "image/png", []byte("not an image"))
	if png.Mime != "image/png" || fake.Mime == "image/png" || csv.Size == 0 {
		t.Fatalf("mimes: %s %s", png.Mime, fake.Mime)
	}

	history := []store.ChatMessage{{Role: "user", Content: "olha isso [📎 clientes.csv](avora:file/" + csv.ID + "#clientes.csv) [📎 proposta.docx](avora:file/" +
		docx.ID + ") ![print](avora:file/" + png.ID + ")"}}
	tr := &turn{agent: store.Agent{ID: "nico", Name: "Nico"}}
	block := s.attachmentBlock(t.Context(), tr, history, false)
	for _, want := range []string{"Ana,Paraíso", "Proposta\nR$ 900 por mês", "anexos/" + csv.ID[:6] + "-clientes.csv", "não enxerga imagens"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block misses %q:\n%s", want, block)
		}
	}
	if !strings.Contains(s.attachmentBlock(t.Context(), tr, history, true), "você vê a imagem") {
		t.Fatal("a model that sees")
	}
	if imgs := s.turnImages(t.Context(), history); len(imgs) != 1 || imgs[0].Mime != "image/png" {
		t.Fatalf("images: %+v", imgs)
	}
	if s.attachmentBlock(t.Context(), tr, []store.ChatMessage{{Role: "user", Content: "oi"}}, false) != "" {
		t.Fatal("no files, no block")
	}
}
