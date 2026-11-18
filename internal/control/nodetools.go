package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Node tools (D26) are programs installed on a node (yt-dlp, ffmpeg,
// pandoc...) that agents run through fixed actions. Each action is an
// argument template: the agent fills the parameters, never flags, and
// files are relative paths inside the run's folder. The node rebuilds the
// command from this same catalog, so the server cannot make it run
// anything else. No shell: the program is executed directly (on the VPS,
// inside the bubblewrap sandbox).

// ToolParam is one parameter of an action.
type ToolParam struct {
	Name string `json:"name"`
	// Kind: "url" (http/https), "in" (a file in the folder), "ins" (a list
	// of them), "out" (a new file in the folder), "text" (Pattern).
	Kind     string   `json:"kind"`
	Desc     string   `json:"desc"`
	Optional bool     `json:"optional,omitempty"`
	Default  string   `json:"default,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	pattern  *regexp.Regexp
}

// ToolAction is one thing a tool does.
type ToolAction struct {
	Name   string      `json:"name"`
	Desc   string      `json:"desc"`
	Params []ToolParam `json:"params"`
	// Args is the command line; {name} is replaced by a parameter and
	// {name...} expands a list. {$model} is filled by the node.
	Args    []string `json:"-"`
	Net     bool     `json:"net,omitempty"`
	Minutes int      `json:"minutes"`
}

// NodeTool is a program a node may have.
type NodeTool struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	// Bins are the executable names tried in order; UnixBins only off
	// Windows (where "convert" is a disk tool).
	Bins        []string     `json:"-"`
	UnixBins    []string     `json:"-"`
	VersionArgs []string     `json:"-"`
	Actions     []ToolAction `json:"actions"`
	// Install says how the owner installs it, per OS ("windows", "linux").
	Install map[string]string `json:"install"`
}

// ToolInfo is a tool a node reports having.
type ToolInfo struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// ToolCall is what a tool job carries (in AgentSpec.Prompt).
type ToolCall struct {
	Tool   string            `json:"tool"`
	Action string            `json:"action"`
	Args   map[string]any    `json:"args"`
	Clean  map[string]string `json:"-"`
}

var (
	reTime  = regexp.MustCompile(`^\d{1,2}(:\d{1,2}){0,2}(\.\d{1,3})?$`)
	reLang  = regexp.MustCompile(`^[a-z]{2,3}(\+[a-z]{3})*$`)
	reSubs  = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z]{2,4})?(\.\*)?(,[a-zA-Z]{2,3}(-[a-zA-Z]{2,4})?(\.\*)?){0,4}$`)
	reSize  = regexp.MustCompile(`^\d{1,5}(x\d{1,5})?%?$`)
	rePages = regexp.MustCompile(`^(\d{1,5}|z)(-(\d{1,5}|z))?(,(\d{1,5}|z)(-(\d{1,5}|z))?){0,30}$`)
	// reFile is a relative path of plain names: letters, digits, space,
	// dot, dash, underscore and "/" between names. No ":" (no drive, URL
	// or ffmpeg protocol), no "%" (no yt-dlp template), no leading "-".
	placeholder = regexp.MustCompile(`\{\$?[a-z_]+\}`)
	reFile      = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_ .()\-]*(/[\p{L}\p{N}_][\p{L}\p{N}_ .()\-]*)*$`)
)

const (
	maxToolFiles = 20
	maxToolURL   = 2000
)

var ytdlpBase = []string{"--ignore-config", "--no-cache-dir", "--no-playlist", "--no-progress", "--no-warnings", "--encoding", "utf-8"}

func with(base []string, more ...string) []string {
	return append(append([]string{}, base...), more...)
}

// NodeToolCatalog is every tool Avora knows how to run.
var NodeToolCatalog = []NodeTool{
	{ID: "yt-dlp", Label: "yt-dlp", Desc: "baixa vídeo, áudio e legendas do YouTube, Instagram, X, TikTok e outros mil sites",
		Bins: []string{"yt-dlp"}, VersionArgs: []string{"--version"},
		Install: map[string]string{"windows": "winget install yt-dlp.yt-dlp",
			"linux": "sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64 -o /usr/local/bin/yt-dlp && sudo chmod +x /usr/local/bin/yt-dlp"},
		Actions: []ToolAction{
			{Name: "info", Desc: "título, canal, duração e data de um vídeo", Net: true, Minutes: 2,
				Params: []ToolParam{{Name: "url", Kind: "url", Desc: "link do vídeo"}},
				Args:   with(ytdlpBase, "--skip-download", "-O", "%(title)s\n%(uploader)s · %(duration_string)s · %(upload_date)s · %(view_count)s visualizações\n%(webpage_url)s", "{url}")},
			{Name: "baixar_video", Desc: "baixa o vídeo em mp4 (até 1080p)", Net: true, Minutes: 30,
				Params: []ToolParam{{Name: "url", Kind: "url", Desc: "link do vídeo"},
					{Name: "saida", Kind: "out", Desc: "nome do arquivo, ex.: video.mp4", Optional: true, Default: "%(title).80B [%(id)s].%(ext)s"}},
				Args: with(ytdlpBase, "-f", "bv*[height<=1080]+ba/b[height<=1080]/b", "--merge-output-format", "mp4", "-o", "{saida}", "{url}")},
			{Name: "baixar_audio", Desc: "baixa só o áudio em mp3", Net: true, Minutes: 20,
				Params: []ToolParam{{Name: "url", Kind: "url", Desc: "link do vídeo"},
					{Name: "saida", Kind: "out", Desc: "nome do arquivo sem extensão, ex.: audio", Optional: true, Default: "%(title).80B [%(id)s].%(ext)s"}},
				Args: with(ytdlpBase, "-x", "--audio-format", "mp3", "-o", "{saida}", "{url}")},
			{Name: "legendas", Desc: "baixa as legendas (também as automáticas) em .vtt, sem o vídeo", Net: true, Minutes: 3,
				Params: []ToolParam{{Name: "url", Kind: "url", Desc: "link do vídeo"},
					{Name: "idiomas", Kind: "text", Desc: "idiomas, ex.: pt.*,en.*", Optional: true, Default: "pt.*,en.*", pattern: reSubs},
					{Name: "saida", Kind: "out", Desc: "nome base do arquivo", Optional: true, Default: "%(title).80B [%(id)s]"}},
				Args: with(ytdlpBase, "--skip-download", "--write-subs", "--write-auto-subs", "--sub-langs", "{idiomas}", "--sub-format", "vtt/best", "-o", "{saida}", "{url}")},
		}},
	{ID: "ffmpeg", Label: "FFmpeg", Desc: "corta, converte e legenda vídeo e áudio",
		Bins: []string{"ffmpeg"}, VersionArgs: []string{"-version"},
		Install: map[string]string{"windows": "winget install Gyan.FFmpeg", "linux": "sudo dnf install ffmpeg (RPM Fusion) ou um build estático em /usr/local/bin"},
		Actions: []ToolAction{
			{Name: "cortar", Desc: "corta um trecho (preciso, recodifica)", Minutes: 20,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "vídeo ou áudio"},
					{Name: "inicio", Kind: "text", Desc: "HH:MM:SS", pattern: reTime},
					{Name: "fim", Kind: "text", Desc: "HH:MM:SS", pattern: reTime},
					{Name: "saida", Kind: "out", Desc: "arquivo novo, ex.: corte.mp4"}},
				Args: []string{"-hide_banner", "-nostdin", "-n", "-ss", "{inicio}", "-to", "{fim}", "-i", "{entrada}", "{saida}"}},
			{Name: "converter", Desc: "converte para o formato da extensão da saída (mp4, webm, mp3, wav, gif...)", Minutes: 30,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "arquivo de origem"}, {Name: "saida", Kind: "out", Desc: "arquivo novo"}},
				Args:   []string{"-hide_banner", "-nostdin", "-n", "-i", "{entrada}", "{saida}"}},
			{Name: "extrair_audio", Desc: "tira o áudio de um vídeo (wav 16 kHz serve para transcrever)", Minutes: 10,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "vídeo"}, {Name: "saida", Kind: "out", Desc: "ex.: audio.wav ou audio.mp3"}},
				Args:   []string{"-hide_banner", "-nostdin", "-n", "-i", "{entrada}", "-vn", "-ar", "16000", "-ac", "1", "{saida}"}},
			{Name: "legendar", Desc: "queima uma legenda .srt no vídeo", Minutes: 30,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "vídeo"}, {Name: "legenda", Kind: "in", Desc: "arquivo .srt (nome sem espaços)"},
					{Name: "saida", Kind: "out", Desc: "vídeo novo"}},
				Args: []string{"-hide_banner", "-nostdin", "-n", "-i", "{entrada}", "-vf", "subtitles={legenda}", "-c:a", "copy", "{saida}"}},
		}},
	{ID: "ffprobe", Label: "ffprobe", Desc: "lê duração, resolução e codecs de um arquivo de mídia",
		Bins: []string{"ffprobe"}, VersionArgs: []string{"-version"},
		Install: map[string]string{"windows": "vem com o FFmpeg", "linux": "vem com o FFmpeg"},
		Actions: []ToolAction{
			{Name: "info", Desc: "duração, tamanho e faixas de um arquivo", Minutes: 1,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "arquivo de mídia"}},
				Args: []string{"-hide_banner", "-v", "error", "-show_entries",
					"format=duration,size,bit_rate:stream=index,codec_type,codec_name,width,height,r_frame_rate",
					"-of", "default=noprint_wrappers=1", "{entrada}"}},
		}},
	{ID: "whisper", Label: "whisper.cpp", Desc: "transcreve áudio no próprio nó, sem mandar para a nuvem",
		Bins: []string{"whisper-cli"}, VersionArgs: nil,
		Install: map[string]string{"windows": "baixe o whisper-cli do whisper.cpp e um modelo ggml (ex.: ggml-small.bin) e defina AVORA_WHISPER_MODEL com o caminho do modelo",
			"linux": "compile o whisper.cpp (cmake), copie o whisper-cli para /usr/local/bin, baixe um modelo ggml e defina AVORA_WHISPER_MODEL"},
		Actions: []ToolAction{
			{Name: "transcrever", Desc: "gera <entrada>.txt (ou .srt/.vtt) ao lado do áudio; wav, mp3, flac ou ogg", Minutes: 60,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "áudio (use ffmpeg.extrair_audio para vídeo)"},
					{Name: "idioma", Kind: "text", Desc: "pt, en... ou auto", Optional: true, Default: "pt", pattern: regexp.MustCompile(`^([a-z]{2}|auto)$`)},
					{Name: "formato", Kind: "text", Desc: "txt, srt ou vtt", Optional: true, Default: "txt", Enum: []string{"txt", "srt", "vtt"}}},
				Args: []string{"-m", "{$model}", "-l", "{idioma}", "-o{formato}", "-np", "-f", "{entrada}"}},
		}},
	{ID: "pandoc", Label: "Pandoc", Desc: "converte documentos: Markdown, Word, HTML, ODT, EPUB",
		Bins: []string{"pandoc"}, VersionArgs: []string{"--version"},
		Install: map[string]string{"windows": "winget install JohnMacFarlane.Pandoc", "linux": "sudo dnf install pandoc"},
		Actions: []ToolAction{
			{Name: "converter", Desc: "converte pela extensão da saída (.docx, .md, .html, .odt, .epub)", Minutes: 5,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "documento"}, {Name: "saida", Kind: "out", Desc: "arquivo novo"}},
				Args:   []string{"--sandbox", "-o", "{saida}", "{entrada}"}},
		}},
	{ID: "magick", Label: "ImageMagick", Desc: "converte e redimensiona imagens",
		Bins: []string{"magick"}, UnixBins: []string{"convert"}, VersionArgs: []string{"-version"},
		Install: map[string]string{"windows": "winget install ImageMagick.ImageMagick", "linux": "sudo dnf install ImageMagick"},
		Actions: []ToolAction{
			{Name: "converter", Desc: "muda o formato pela extensão da saída (png, jpg, webp, pdf...)", Minutes: 5,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "imagem"}, {Name: "saida", Kind: "out", Desc: "imagem nova"}},
				Args:   []string{"{entrada}", "{saida}"}},
			{Name: "redimensionar", Desc: "muda o tamanho mantendo a proporção", Minutes: 5,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "imagem"},
					{Name: "tamanho", Kind: "text", Desc: "1080x1080, 1080 (largura) ou 50%", pattern: reSize},
					{Name: "saida", Kind: "out", Desc: "imagem nova"}},
				Args: []string{"{entrada}", "-resize", "{tamanho}", "{saida}"}},
		}},
	{ID: "tesseract", Label: "Tesseract", Desc: "lê o texto de imagens (OCR)",
		Bins: []string{"tesseract"}, VersionArgs: []string{"--version"},
		Install: map[string]string{"windows": "winget install UB-Mannheim.TesseractOCR (marque o idioma português)", "linux": "sudo dnf install tesseract tesseract-langpack-por"},
		Actions: []ToolAction{
			{Name: "ocr", Desc: "devolve o texto de uma imagem", Minutes: 3,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "imagem (png, jpg, tif)"},
					{Name: "idioma", Kind: "text", Desc: "por, eng ou por+eng", Optional: true, Default: "por", pattern: reLang}},
				Args: []string{"{entrada}", "stdout", "-l", "{idioma}"}},
		}},
	{ID: "qpdf", Label: "qpdf", Desc: "junta PDFs e extrai páginas",
		Bins: []string{"qpdf"}, VersionArgs: []string{"--version"},
		Install: map[string]string{"windows": "winget install QPDF.QPDF", "linux": "sudo dnf install qpdf"},
		Actions: []ToolAction{
			{Name: "juntar", Desc: "junta vários PDFs, na ordem dada", Minutes: 5,
				Params: []ToolParam{{Name: "entradas", Kind: "ins", Desc: "lista de PDFs"}, {Name: "saida", Kind: "out", Desc: "PDF novo"}},
				Args:   []string{"--empty", "--pages", "{entradas...}", "--", "{saida}"}},
			{Name: "paginas", Desc: "extrai páginas para um PDF novo", Minutes: 5,
				Params: []ToolParam{{Name: "entrada", Kind: "in", Desc: "PDF"},
					{Name: "paginas", Kind: "text", Desc: "ex.: 1-3,7 (z = última)", pattern: rePages},
					{Name: "saida", Kind: "out", Desc: "PDF novo"}},
				Args: []string{"{entrada}", "--pages", ".", "{paginas}", "--", "{saida}"}},
		}},
}

// FindTool returns a tool and one of its actions.
func FindTool(tool, action string) (NodeTool, ToolAction, bool) {
	for _, t := range NodeToolCatalog {
		if t.ID != tool {
			continue
		}
		for _, a := range t.Actions {
			if a.Name == action {
				return t, a, true
			}
		}
		return t, ToolAction{}, false
	}
	return NodeTool{}, ToolAction{}, false
}

// CleanFile checks a relative file path of a tool call.
func CleanFile(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimPrefix(p, "./")
	if p == "" || len(p) > 240 || !reFile.MatchString(p) {
		return "", fmt.Errorf("arquivo inválido: %q (um caminho relativo à pasta, com letras, números, espaço, ponto, hífen e _)", p)
	}
	for _, part := range strings.Split(p, "/") {
		if strings.Trim(part, ". ") == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", fmt.Errorf("arquivo inválido: %q", p)
		}
	}
	return p, nil
}

func cleanToolURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	if len(v) > maxToolURL || strings.ContainsAny(v, " \t\r\n\x00") || !(strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://")) {
		return "", errors.New("o link precisa começar com https://")
	}
	return v, nil
}

// Argv checks a call against the catalog and builds the command line (no
// program name). files are the paths the call reads (ins) and writes
// (outs), for the node to check inside the folder.
func (c *ToolCall) Argv() (argv, ins, outs []string, err error) {
	_, action, ok := FindTool(c.Tool, c.Action)
	if !ok {
		return nil, nil, nil, fmt.Errorf("não há a ação %s.%s", c.Tool, c.Action)
	}
	known := map[string]bool{}
	values := map[string]string{}
	lists := map[string][]string{}
	for _, p := range action.Params {
		known[p.Name] = true
		raw, present := c.Args[p.Name]
		if !present || raw == nil || raw == "" {
			if !p.Optional {
				return nil, nil, nil, fmt.Errorf("falta o parâmetro %s (%s)", p.Name, p.Desc)
			}
			values[p.Name] = p.Default
			continue
		}
		if p.Kind == "ins" {
			items, ok := raw.([]any)
			if !ok || len(items) == 0 || len(items) > maxToolFiles {
				return nil, nil, nil, fmt.Errorf("%s é uma lista de 1 a %d arquivos", p.Name, maxToolFiles)
			}
			for _, it := range items {
				s, _ := it.(string)
				f, err := CleanFile(s)
				if err != nil {
					return nil, nil, nil, err
				}
				lists[p.Name] = append(lists[p.Name], f)
				ins = append(ins, f)
			}
			continue
		}
		v, ok := raw.(string)
		if !ok {
			if n, isNum := raw.(float64); isNum {
				v, ok = fmt.Sprint(n), true
			}
		}
		if !ok || !utf8.ValidString(v) {
			return nil, nil, nil, fmt.Errorf("%s deve ser texto", p.Name)
		}
		switch p.Kind {
		case "url":
			v, err = cleanToolURL(v)
		case "in", "out":
			v, err = CleanFile(v)
			if err == nil && p.Kind == "in" {
				ins = append(ins, v)
			} else if err == nil {
				outs = append(outs, v)
			}
		default:
			v = strings.TrimSpace(v)
			switch {
			case len(p.Enum) > 0:
				found := false
				for _, e := range p.Enum {
					found = found || e == v
				}
				if !found {
					err = fmt.Errorf("%s é um destes: %s", p.Name, strings.Join(p.Enum, ", "))
				}
			case p.pattern != nil && !p.pattern.MatchString(v):
				err = fmt.Errorf("%s inválido: %q (%s)", p.Name, v, p.Desc)
			}
		}
		if err != nil {
			return nil, nil, nil, err
		}
		values[p.Name] = v
	}
	extra := []string{}
	for k := range c.Args {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, nil, nil, fmt.Errorf("parâmetros que %s.%s não tem: %s", c.Tool, c.Action, strings.Join(extra, ", "))
	}
	for _, a := range action.Args {
		if name, ok := strings.CutSuffix(strings.TrimPrefix(a, "{"), "...}"); ok && strings.HasPrefix(a, "{") {
			argv = append(argv, lists[name]...)
			continue
		}
		// One pass: a value is never scanned for placeholders again.
		argv = append(argv, placeholder.ReplaceAllStringFunc(a, func(m string) string {
			if v, ok := values[m[1:len(m)-1]]; ok {
				return v
			}
			return m // {$model}: the node fills it
		}))
	}
	c.Clean = values
	return argv, ins, outs, nil
}

// EncodeToolCall is the job prompt of a call.
func EncodeToolCall(c ToolCall) string {
	b, _ := json.Marshal(c)
	return string(b)
}

// DecodeToolCall reads a job prompt back.
func DecodeToolCall(s string) (ToolCall, error) {
	var c ToolCall
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, errors.New("chamada de ferramenta inválida")
	}
	return c, nil
}

func validTools(list []ToolInfo) bool {
	if len(list) > len(NodeToolCatalog) {
		return false
	}
	for _, t := range list {
		if !knownTool(t.ID) || len(t.Version) > 80 || !utf8.ValidString(t.Version) {
			return false
		}
	}
	return true
}

func knownTool(id string) bool {
	for _, t := range NodeToolCatalog {
		if t.ID == id {
			return true
		}
	}
	return false
}
