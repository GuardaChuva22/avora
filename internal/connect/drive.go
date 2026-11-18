package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const (
	gDoc   = "application/vnd.google-apps.document"
	gSheet = "application/vnd.google-apps.spreadsheet"
	gSlide = "application/vnd.google-apps.presentation"
	gDir   = "application/vnd.google-apps.folder"
)

func (g *Google) driveTools() []Tool {
	return []Tool{
		{Name: "drive_buscar", Risk: Read,
			Description: "Busca arquivos no Google Drive do dono pelo nome ou conteúdo; vazio = os mais recentes. Mostra id, tipo, data e link.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"},
	"quantidade": {"type": "integer", "description": "Padrão 15, até 50."}}}`,
			Summary: func(a Args) string { return "arquivos do Drive" + quoted(a.Str("busca")) }},
		{Name: "drive_ler", Risk: Read,
			Description: "Lê o conteúdo de um arquivo do Drive pelo id: Documentos e Apresentações como texto, Planilhas como CSV (primeira aba), arquivos de texto como estão.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"},
	"parte": {"type": "integer"}}, "required": ["id"]}`,
			Summary: func(a Args) string { return "leu o arquivo " + a.Str("id") + " do Drive" }},
		{Name: "drive_criar", Risk: Write,
			Description: "Cria um arquivo no Drive do dono a partir de texto: um Documento Google (padrão) ou um arquivo .txt. Devolve o link.",
			Params: `{"type": "object", "properties": {
	"nome": {"type": "string"},
	"conteudo": {"type": "string"},
	"formato": {"type": "string", "enum": ["documento", "texto"]},
	"pasta_id": {"type": "string", "description": "Id de uma pasta do Drive; vazio = Meu Drive."}
}, "required": ["nome", "conteudo"]}`,
			Summary: func(a Args) string { return "criou «" + cut(a.Str("nome"), 100) + "» no Drive" }},
	}
}

type driveFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	ModifiedTime time.Time `json:"modifiedTime"`
	WebViewLink  string    `json:"webViewLink"`
	Size         string    `json:"size"`
}

func driveKind(m string) string {
	switch m {
	case gDoc:
		return "documento"
	case gSheet:
		return "planilha"
	case gSlide:
		return "apresentação"
	case gDir:
		return "pasta"
	}
	return m
}

func driveQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func (g *Google) drive(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	loc := env.zone()
	fields := "id,name,mimeType,modifiedTime,webViewLink,size"
	switch tool {
	case "drive_buscar":
		q := url.Values{"pageSize": {fmt.Sprint(a.Int("quantidade", 15, 1, 50))}, "fields": {"files(" + fields + ")"},
			"supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"}}
		if s := a.Str("busca"); s != "" {
			q.Set("q", fmt.Sprintf("(name contains %s or fullText contains %s) and trashed = false", driveQuote(s), driveQuote(s)))
		} else {
			q.Set("q", "trashed = false")
			q.Set("orderBy", "modifiedTime desc")
		}
		var r struct {
			Files []driveFile `json:"files"`
		}
		if err := g.api(ctx, env, request{URL: g.url(apisHost, "/drive/v3/files?"+q.Encode())}, &r); err != nil {
			return "", err
		}
		if len(r.Files) == 0 {
			return "Nenhum arquivo encontrado.", nil
		}
		lines := []string{}
		for _, f := range r.Files {
			lines = append(lines, fmt.Sprintf("[%s] %s · %s · %s\n   %s", f.ID, f.Name, driveKind(f.MimeType), f.ModifiedTime.In(loc).Format("02/01/2006"), f.WebViewLink))
		}
		return strings.Join(lines, "\n"), nil

	case "drive_ler":
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		var f driveFile
		path := "/drive/v3/files/" + url.PathEscape(id)
		if err := g.api(ctx, env, request{URL: g.url(apisHost, path+"?supportsAllDrives=true&fields="+fields)}, &f); err != nil {
			return "", err
		}
		var src string
		switch {
		case f.MimeType == gDoc || f.MimeType == gSlide:
			src = path + "/export?mimeType=text%2Fplain"
		case f.MimeType == gSheet:
			src = path + "/export?mimeType=text%2Fcsv"
		case strings.HasPrefix(f.MimeType, "text/") || f.MimeType == "application/json" || strings.HasSuffix(f.MimeType, "+xml") || f.MimeType == "application/xml":
			src = path + "?alt=media&supportsAllDrives=true"
		default:
			return fmt.Sprintf("%s é %s; só leio documentos, planilhas, apresentações e arquivos de texto. Link: %s", f.Name, driveKind(f.MimeType), f.WebViewLink), nil
		}
		data, err := g.raw(ctx, env, request{URL: g.url(apisHost, src)})
		if err != nil {
			return "", err
		}
		head := fmt.Sprintf("%s (%s, %s)\n%s\n", f.Name, driveKind(f.MimeType), f.ModifiedTime.In(loc).Format("02/01/2006 15:04"), f.WebViewLink)
		return withPart(head, strings.ToValidUTF8(string(data), "�"), a.Int("parte", 1, 1, 1000)), nil

	case "drive_criar":
		name, err := a.Need("nome")
		if err != nil {
			return "", err
		}
		content := a.Str("conteudo")
		meta := map[string]any{"name": name, "mimeType": gDoc}
		if a.Str("formato") == "texto" {
			meta["mimeType"] = "text/plain"
		}
		if p := a.Str("pasta_id"); p != "" {
			meta["parents"] = []string{p}
		}
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		mj, _ := json.Marshal(meta)
		p1, _ := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
		p1.Write(mj)
		p2, _ := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}})
		p2.Write([]byte(content))
		w.Close()
		var f driveFile
		if err := g.api(ctx, env, request{Method: "POST", URL: g.url(apisHost, "/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields="+fields),
			Body: buf.Bytes(), ContentType: "multipart/related; boundary=" + w.Boundary()}, &f); err != nil {
			return "", err
		}
		return fmt.Sprintf("Criado no Drive: %s (%s, id %s)\n%s", f.Name, driveKind(f.MimeType), f.ID, f.WebViewLink), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
