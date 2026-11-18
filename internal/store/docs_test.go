package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDocsVersionsSearchAndTrash(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
		d, err := s.CreateDoc(ctx, Doc{ID: "d1", Title: "Proposta Comercial — Açaí da Vila", Kind: DocKindDoc, Folder: "Clientes/Vila",
			Summary: "Preços e prazos", Content: "# Proposta\n\nO pacote mensal custa R$ 2.400 e inclui 12 posts."}, "", "agent:iris", t0)
		if err != nil || d.Version != 1 || d.Author != "agent:iris" || d.Size == 0 {
			t.Fatalf("create: %+v %v", d, err)
		}
		if _, err := s.CreateDoc(ctx, Doc{ID: "d1", Title: "x", Kind: DocKindDoc}, "", ActorAdmin, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("same id: %v", err)
		}
		if _, err := s.CreateDoc(ctx, Doc{ID: "d2", Title: "Tom da marca", Kind: DocKindDoc, Pinned: true, ProjectID: "loja",
			Content: "Falamos como amigos."}, "", ActorAdmin, t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}

		// Accents and case don't matter; every word must be there; the
		// snippet is the text around the hit.
		hits, err := s.ListDocs(ctx, DocFilter{Query: "acai PACOTE"})
		if err != nil || len(hits) != 1 || hits[0].ID != "d1" || !strings.Contains(hits[0].Snippet, "pacote mensal custa") || hits[0].Content != "" {
			t.Fatalf("search: %+v %v", hits, err)
		}
		if hits, _ := s.ListDocs(ctx, DocFilter{Query: "acai inexistente"}); len(hits) != 0 {
			t.Fatalf("all words: %+v", hits)
		}
		if list, _ := s.ListDocs(ctx, DocFilter{Folder: "Clientes"}); len(list) != 1 {
			t.Fatalf("folder and below: %+v", list)
		}
		other := "outro"
		if list, _ := s.ListDocs(ctx, DocFilter{Scope: &other}); len(list) != 1 || list[0].ID != "d1" {
			t.Fatalf("another project's agent: %+v", list)
		}
		if list, _ := s.ListDocs(ctx, DocFilter{Pinned: true}); len(list) != 1 || list[0].ID != "d2" {
			t.Fatalf("pinned: %+v", list)
		}

		// A change of content is a new version; a stale base is refused.
		text := "# Proposta\n\nO pacote mensal custa R$ 2.900."
		d, err = s.UpdateDoc(ctx, "d1", DocChange{Content: &text, Note: "novo preço", BaseVersion: 1}, ActorAdmin, t0.Add(time.Hour))
		if err != nil || d.Version != 2 || d.Editor != ActorAdmin || d.Author != "agent:iris" {
			t.Fatalf("update: %+v %v", d, err)
		}
		if _, err := s.UpdateDoc(ctx, "d1", DocChange{Content: &text, BaseVersion: 1}, ActorAdmin, t0); !errors.Is(err, ErrStale) {
			t.Fatalf("stale: %v", err)
		}
		pinned := true
		if d, err = s.UpdateDoc(ctx, "d1", DocChange{Pinned: &pinned, Content: &text}, ActorAdmin, t0.Add(2*time.Hour)); err != nil || d.Version != 2 || !d.Pinned {
			t.Fatalf("same content is no version: %+v %v", d, err)
		}
		old, err := s.GetDoc(ctx, "d1", 1)
		if err != nil || old.Shown != 1 || !strings.Contains(old.Content, "2.400") {
			t.Fatalf("old version: %+v %v", old, err)
		}
		vs, err := s.DocVersions(ctx, "d1")
		if err != nil || len(vs) != 2 || vs[0].Version != 2 || vs[0].Note != "novo preço" || vs[0].Content != "" {
			t.Fatalf("versions: %+v %v", vs, err)
		}
		if hits, _ := s.ListDocs(ctx, DocFilter{Query: "2.400"}); len(hits) != 0 {
			t.Fatalf("search reads the current version only: %+v", hits)
		}

		// Trash, restore, and the purge of what passed its days.
		if _, err := s.TrashDoc(ctx, "d1", true, ActorAdmin, t0.Add(3*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateDoc(ctx, "d1", DocChange{Content: &text}, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("edit in the trash: %v", err)
		}
		if list, _ := s.ListDocs(ctx, DocFilter{}); len(list) != 1 {
			t.Fatalf("live: %+v", list)
		}
		if list, _ := s.ListDocs(ctx, DocFilter{Trash: true}); len(list) != 1 || list[0].DeletedAt == nil {
			t.Fatalf("trash: %+v", list)
		}
		if _, err := s.TrashDoc(ctx, "d1", false, ActorAdmin, t0.Add(4*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.TrashDoc(ctx, "d1", true, ActorAdmin, t0.Add(5*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if n, err := s.PurgeDocs(ctx, "", t0.Add(5*time.Hour), ActorSystem, t0); err != nil || n != 0 {
			t.Fatalf("too recent to purge: %d %v", n, err)
		}
		if n, err := s.PurgeDocs(ctx, "", t0.Add(6*time.Hour), ActorSystem, t0); err != nil || n != 1 {
			t.Fatalf("purge: %d %v", n, err)
		}
		if _, err := s.GetDoc(ctx, "d1", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("gone: %v", err)
		}
		if _, err := s.PurgeDocs(ctx, "d2", t0, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a live doc is not purged: %v", err)
		}
	})
}

func TestDocSnippetKeepsAccents(t *testing.T) {
	got := docSnippet("Reunião com o cliente: decisão de lançar em março.", SearchWords("DECISAO"))
	if got != "Reunião com o cliente: decisão de lançar em março." {
		t.Fatalf("snippet: %q", got)
	}
}

func TestExtractDocLinks(t *testing.T) {
	md := strings.Join([]string{
		"# Guia",
		"Veja [[Tom da marca]] e [[  Plano de Lançamento  | o plano ]].",
		"Repetido [[tom da marca|outro texto]] e `[[Em código inline]]`.",
		"```bash",
		"if [[ $a == $b ]]; then echo [[Em bloco]]; fi",
		"```",
		"E também [[Página Futura]].",
	}, "\n")
	got := ExtractDocLinks(md)
	want := []string{"Tom da marca", "Plano de Lançamento", "Página Futura"}
	if !equal(got, want) {
		t.Fatalf("links %v, want %v", got, want)
	}
}

func TestDocLinksBacklinksResolveAndRestore(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

		// Create a doc linking to an existing target and a page not yet created.
		if _, err := s.CreateDoc(ctx, Doc{ID: "tom", Title: "Tom da marca", Kind: DocKindDoc,
			Content: "Voz direta e simples."}, "", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateDoc(ctx, Doc{ID: "camp", Title: "Campanha Q4", Kind: DocKindDoc, Folder: "Marketing",
			Content: "Siga [[Tom da marca|o guia de voz]] e consulte [[Roteiro]]."}, "", "agent:atlas", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}

		links, err := DocLinks(ctx, s, "camp")
		if err != nil || len(links) != 2 {
			t.Fatalf("doc links: %+v %v", links, err)
		}
		byTitle := map[string]string{}
		for _, l := range links {
			byTitle[l.ToTitle] = l.ToID
		}
		if byTitle["Tom da marca"] != "tom" || byTitle["Roteiro"] != "" {
			t.Fatalf("resolved map: %+v", byTitle)
		}

		back, err := DocBacklinks(ctx, s, "tom", nil)
		if err != nil || len(back) != 1 || back[0].ID != "camp" || back[0].Title != "Campanha Q4" ||
			back[0].Author != "agent:atlas" || !strings.Contains(back[0].Snippet, "o guia de voz") {
			t.Fatalf("backlinks: %+v %v", back, err)
		}

		// Creating the missing target resolves the pending link, preferring the same folder.
		if _, err := s.CreateDoc(ctx, Doc{ID: "rot-root", Title: "Roteiro", Kind: DocKindDoc,
			Content: "Roteiro geral."}, "", ActorAdmin, t0.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateDoc(ctx, Doc{ID: "rot-mkt", Title: "Roteiro", Kind: DocKindDoc, Folder: "Marketing",
			Content: "Roteiro de marketing."}, "", ActorAdmin, t0.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got, err := ResolveDoc(ctx, s, "roteiro", "Marketing", nil); err != nil || got.ID != "rot-mkt" {
			t.Fatalf("resolve same folder: %+v %v", got, err)
		}
		if got, err := ResolveDoc(ctx, s, "roteiro", "", nil); err != nil || got.ID != "rot-root" {
			t.Fatalf("resolve root: %+v %v", got, err)
		}
		if _, err := ResolveDoc(ctx, s, "Inexistente", "", nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("resolve missing: %v", err)
		}
		if back, err = DocBacklinks(ctx, s, "rot-mkt", nil); err != nil || len(back) != 1 || back[0].ID != "camp" {
			t.Fatalf("backlinks rebound to same-folder doc: %+v %v", back, err)
		}

		// Trashing the source doc hides it from backlinks until restored.
		if _, err := s.TrashDoc(ctx, "camp", true, ActorAdmin, t0.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if back, _ = DocBacklinks(ctx, s, "tom", nil); len(back) != 0 {
			t.Fatalf("trashed source still in backlinks: %+v", back)
		}
		if _, err := s.TrashDoc(ctx, "camp", false, ActorAdmin, t0.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if back, _ = DocBacklinks(ctx, s, "tom", nil); len(back) != 1 {
			t.Fatalf("restored source missing from backlinks: %+v", back)
		}

		// Version restore brings back old content (and its links), and caps history at 50.
		noLinks := "Texto atualizado sem links."
		if _, err := s.UpdateDoc(ctx, "camp", DocChange{Content: &noLinks, Note: "removeu links"}, ActorAdmin, t0.Add(6*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if back, _ = DocBacklinks(ctx, s, "tom", nil); len(back) != 0 {
			t.Fatalf("old links stayed after edit: %+v", back)
		}
		restored, err := RestoreDocVersion(ctx, s, "camp", 1, ActorAdmin, t0.Add(7*time.Minute))
		if err != nil || restored.Version != 3 || !strings.Contains(restored.Content, "[[Tom da marca|o guia de voz]]") {
			t.Fatalf("restore v1: %+v %v", restored, err)
		}
		if back, _ = DocBacklinks(ctx, s, "tom", nil); len(back) != 1 {
			t.Fatalf("backlinks not restored with v1: %+v", back)
		}

		for i := 0; i < 55; i++ {
			body := "Versão número " + strconv.Itoa(i)
			if _, err := s.UpdateDoc(ctx, "tom", DocChange{Content: &body}, ActorAdmin, t0.Add(time.Duration(10+i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		vs, err := s.DocVersions(ctx, "tom")
		if err != nil || len(vs) != MaxDocVersions {
			t.Fatalf("versions len %d, want %d (%v)", len(vs), MaxDocVersions, err)
		}
	})
}
