package control

import (
	"net/url"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestDocsAPI(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	title, folder, content := "  Plano de   lançamento ", "Marketing / Q4/", "# Plano\n\n1. Teaser"
	w := request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &title, Folder: &folder, Content: &content})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	d := decode[store.Doc](t, w)
	if d.Title != "Plano de lançamento" || d.Folder != "Marketing/Q4" || d.Kind != "doc" || d.Version != 1 || d.Author != "admin" {
		t.Fatalf("doc: %+v", d)
	}
	bad := "x"
	if w := request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &bad, Kind: "exe"}); w.Code != 400 {
		t.Fatalf("bad kind: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Content: &content}); w.Code != 400 {
		t.Fatalf("no title: %d", w.Code)
	}

	next := content + "\n2. Lançamento"
	d = decode[store.Doc](t, request(t, s, "PATCH", "/v1/docs/"+d.ID, adminTestToken, DocRequest{Content: &next, Note: "passo 2", BaseVersion: 1}))
	if d.Version != 2 || d.Content != next {
		t.Fatalf("edit: %+v", d)
	}
	if w := request(t, s, "PATCH", "/v1/docs/"+d.ID, adminTestToken, DocRequest{Content: &content, BaseVersion: 1}); w.Code != 409 {
		t.Fatalf("stale edit: %d", w.Code)
	}
	if old := decode[store.Doc](t, request(t, s, "GET", "/v1/docs/"+d.ID+"?version=1", adminTestToken, nil)); old.Content != content || old.Shown != 1 {
		t.Fatalf("v1: %+v", old)
	}
	if vs := decode[[]store.DocVersion](t, request(t, s, "GET", "/v1/docs/"+d.ID+"/versions", adminTestToken, nil)); len(vs) != 2 || vs[0].Note != "passo 2" {
		t.Fatalf("versions: %+v", vs)
	}
	if list := decode[[]store.Doc](t, request(t, s, "GET", "/v1/docs?q=lancamento", adminTestToken, nil)); len(list) != 1 || list[0].Content != "" {
		t.Fatalf("search: %+v", list)
	}

	// An uploaded file becomes a doc with its text, for search.
	f, err := s.store.SaveFile(t.Context(), store.File{ID: "f0000001", Name: "contrato.txt", Mime: "text/plain", Data: []byte("Cláusula de rescisão em 30 dias.")})
	if err != nil {
		t.Fatal(err)
	}
	fd := decode[store.Doc](t, request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Kind: "file", FileID: f.ID}))
	if fd.Title != "contrato.txt" || fd.FileID != f.ID || fd.Mime != "text/plain" || !strings.Contains(fd.Content, "rescisão") {
		t.Fatalf("file doc: %+v", fd)
	}
	if w := request(t, s, "PATCH", "/v1/docs/"+fd.ID, adminTestToken, DocRequest{Content: &content}); w.Code != 400 {
		t.Fatalf("a file's text is not edited: %d", w.Code)
	}

	// Trash, the list of the trash, restore, delete for good.
	if w := request(t, s, "DELETE", "/v1/docs/"+fd.ID, adminTestToken, nil); w.Code != 200 {
		t.Fatalf("trash: %d", w.Code)
	}
	if list := decode[[]store.Doc](t, request(t, s, "GET", "/v1/docs?trash=1", adminTestToken, nil)); len(list) != 1 || list[0].ID != fd.ID {
		t.Fatalf("trash list: %+v", list)
	}
	if w := request(t, s, "DELETE", "/v1/docs/"+d.ID+"?forever=1", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("a live doc is not deleted for good: %d", w.Code)
	}
	request(t, s, "POST", "/v1/docs/"+fd.ID+"/restore", adminTestToken, nil)
	request(t, s, "DELETE", "/v1/docs/"+fd.ID, adminTestToken, nil)
	if w := request(t, s, "DELETE", "/v1/docs/"+fd.ID+"?forever=1", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete for good: %d", w.Code)
	}
}

func TestAgentsUseDocs(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	decode[store.Project](t, request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Loja"}))
	request(t, s, "PUT", "/v1/agents/atlas/project", adminTestToken, MoveRequest{ProjectID: "loja"})
	title, sum, content := "Tom da marca", "Como a marca fala", "Falamos como amigos, sem jargão.\n\nNunca prometa prazo."
	pin := true
	brand := decode[store.Doc](t, request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &title, Summary: &sum, Content: &content, Pinned: &pin}))
	otherTitle, outro := "Segredo do outro projeto", "outro"
	decode[store.Project](t, request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Outro"}))
	secret := decode[store.Doc](t, request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &otherTitle, Content: &content, ProjectID: &outro}))

	// Pinned docs are in every agent's instructions.
	atlas, _ := s.store.GetAgent(t.Context(), "atlas")
	if block := s.docsBlock(t.Context(), atlas); !strings.Contains(block, "«Tom da marca» ("+brand.ID+"): Como a marca fala") {
		t.Fatalf("docs block: %s", block)
	}

	e.calls["Atlas"] = []llm.ToolCall{
		call("docs_buscar", map[string]any{"busca": "JARGAO"}),
		call("docs_ler", map[string]any{"id": secret.ID}),
		call("docs_criar", map[string]any{"titulo": "Roteiro do Reels", "conteudo": "# Roteiro\n\nCena 1: abertura.", "pasta": "Conteúdo/Reels", "resumo": "Reels de lançamento"}),
		call("docs_editar", map[string]any{"id": brand.ID, "trocar": []map[string]any{{"de": "Nunca prometa prazo.", "para": "Prometa só o que dá."}}, "acrescentar": "Use emoji com moderação."}),
		call("docs_editar", map[string]any{"id": brand.ID, "trocar": []map[string]any{{"de": "não existe", "para": "x"}}}),
	}
	c := agentConversation(t, s, "atlas")
	r := send(t, s, c.ID, "cria o roteiro e ajusta o tom")
	out := r.Assistant.Content
	if !strings.Contains(out, "«Tom da marca» · id "+brand.ID) || !strings.Contains(out, "Falamos como amigos, sem jargão") {
		t.Fatalf("search: %s", out)
	}
	if !strings.Contains(out, "Erro: doc não encontrado") {
		t.Fatalf("another project's doc: %s", out)
	}
	if !strings.Contains(out, "Doc «Roteiro do Reels» criado nos Docs, pasta Conteúdo/Reels") || !strings.Contains(out, "](avora:doc/") {
		t.Fatalf("create: %s", out)
	}
	if !strings.Contains(out, "salvo na versão 2") || !strings.Contains(out, "não aparece no doc") {
		t.Fatalf("edit: %s", out)
	}
	got, _ := s.store.GetDoc(t.Context(), brand.ID, 0)
	if got.Version != 2 || !strings.Contains(got.Content, "Prometa só o que dá.") || !strings.HasSuffix(got.Content, "Use emoji com moderação.\n") ||
		got.Editor != "agent:atlas" {
		t.Fatalf("edited: %+v", got)
	}
	list, _ := s.store.ListDocs(t.Context(), store.DocFilter{Query: "roteiro"})
	if len(list) != 1 || list[0].ProjectID != "loja" || list[0].Author != "agent:atlas" || list[0].ConversationID != c.ID {
		t.Fatalf("created doc: %+v", list)
	}

	// The owner cites a doc: its text comes with the message.
	e.calls["Atlas"] = nil
	e.answers = map[string]string{}
	msg := []store.ChatMessage{{Role: "user", Content: "olha esse [📄 Roteiro](avora:doc/" + list[0].ID + ")"}}
	if block := s.citedDocsBlock(t.Context(), &turn{agent: atlas, team: []store.Agent{atlas}}, msg); !strings.Contains(block, "### «Roteiro do Reels»") ||
		!strings.Contains(block, "Cena 1: abertura.") {
		t.Fatalf("cited: %s", block)
	}

	// Moving to the trash waits for the owner.
	e.calls["Atlas"] = []llm.ToolCall{call("docs_lixeira", map[string]any{"id": list[0].ID})}
	answer := sendAsync(t, s, c.ID, "apaga o roteiro")
	ap := waitApproval(t, s)
	if ap.Action != "docs_lixeira" || !strings.Contains(ap.Summary, "Mover o doc «Roteiro do Reels» para a lixeira") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	if r := <-answer; !strings.Contains(r.Assistant.Content, "foi para a lixeira") {
		t.Fatalf("trash: %s", r.Assistant.Content)
	}
}

func TestDocEdits(t *testing.T) {
	text, err := editedContent("a b a", docToolArgs{Trocar: []docEditArg{{De: "a", Para: "x", Todas: true}}, Acrescentar: "fim"})
	if err != nil || text != "x b x\n\nfim\n" {
		t.Fatalf("edit: %q %v", text, err)
	}
	if _, err := editedContent("a b a", docToolArgs{Trocar: []docEditArg{{De: "a", Para: "x"}}}); err == nil || !strings.Contains(err.Error(), "aparece 2 vezes") {
		t.Fatalf("ambiguous: %v", err)
	}
	if ids := docIDs("[a](avora:doc/0123456789abcdef) e [b](avora:doc/0123456789abcdef) [c](avora:doc/fedcba9876543210)"); len(ids) != 2 {
		t.Fatalf("ids: %v", ids)
	}
	if f, err := cleanDocFolder(" a /../ b// c "); err != nil || f != "a/b/c" {
		t.Fatalf("folder: %q %v", f, err)
	}
}

func TestDocsLinksBacklinksResolveAndRestoreAPI(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)

	t1, c1 := "Guia de Estilo", "# Guia\n\nRegras v1."
	guia := decode[store.Doc](t, request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &t1, Content: &c1}))

	t2, c2 := "Checklist de Post", "Antes de publicar, confira [[Guia de Estilo|o nosso guia]] e [[Agenda Editorial]]."
	post := decode[store.Doc](t, request(t, s, "POST", "/v1/docs", adminTestToken, DocRequest{Title: &t2, Content: &c2}))

	// Resolve existing and missing titles.
	w := request(t, s, "GET", "/v1/docs/resolve?title="+url.QueryEscape("guia de estilo"), adminTestToken, nil)
	if w.Code != 200 {
		t.Fatalf("resolve: %d %s", w.Code, w.Body.String())
	}
	if got := decode[store.Doc](t, w); got.ID != guia.ID {
		t.Fatalf("resolved %+v, want %s", got, guia.ID)
	}
	if w := request(t, s, "GET", "/v1/docs/resolve?title="+url.QueryEscape("Agenda Editorial"), adminTestToken, nil); w.Code != 404 {
		t.Fatalf("missing resolve: %d", w.Code)
	}
	if w := request(t, s, "GET", "/v1/docs/resolve", adminTestToken, nil); w.Code != 400 {
		t.Fatalf("empty resolve: %d", w.Code)
	}

	// Backlinks route returns the referring doc and snippet.
	back := decode[[]store.DocBacklink](t, request(t, s, "GET", "/v1/docs/"+guia.ID+"/backlinks", adminTestToken, nil))
	if len(back) != 1 || back[0].ID != post.ID || back[0].Title != "Checklist de Post" || !strings.Contains(back[0].Snippet, "o nosso guia") {
		t.Fatalf("backlinks: %+v", back)
	}
	if w := request(t, s, "GET", "/v1/docs/inexistente/backlinks", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("missing backlinks: %d", w.Code)
	}

	// Edit and restore an older version through POST /v1/docs/{id}/versions/{n}/restore.
	c1v2 := "# Guia\n\nRegras v2."
	guia = decode[store.Doc](t, request(t, s, "PATCH", "/v1/docs/"+guia.ID, adminTestToken, DocRequest{Content: &c1v2, Note: "v2"}))
	if guia.Version != 2 {
		t.Fatalf("v2: %+v", guia)
	}
	w = request(t, s, "POST", "/v1/docs/"+guia.ID+"/versions/1/restore", adminTestToken, nil)
	if w.Code != 200 {
		t.Fatalf("restore v1: %d %s", w.Code, w.Body.String())
	}
	restored := decode[store.Doc](t, w)
	if restored.Version != 3 || restored.Content != c1 {
		t.Fatalf("restored: %+v", restored)
	}
	if w := request(t, s, "POST", "/v1/docs/"+guia.ID+"/versions/99/restore", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("restore missing version: %d", w.Code)
	}

	// Agent tools: write_doc describes [[links]] and read_doc includes backlinks.
	foundLinkHint := false
	for _, tl := range docTools(false) {
		if tl.Function.Name == "docs_criar" && strings.Contains(tl.Function.Description, "[[Título]]") {
			foundLinkHint = true
		}
	}
	if !foundLinkHint {
		t.Fatal("docs_criar description missing [[Título]] explanation")
	}

	e.calls["Atlas"] = []llm.ToolCall{call("docs_ler", map[string]any{"id": guia.ID})}
	conv := agentConversation(t, s, "atlas")
	r := send(t, s, conv.ID, "leia o guia")
	if !strings.Contains(r.Assistant.Content, "Mencionado em:") || !strings.Contains(r.Assistant.Content, "«Checklist de Post»") {
		t.Fatalf("docs_ler missing backlinks: %s", r.Assistant.Content)
	}
}
