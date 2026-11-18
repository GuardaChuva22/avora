package control

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Skills (D26) are written know-how in the SKILL.md format of Agent
// Skills: a name and a description go into the prompt of every agent that
// can see the skill, and the instructions load with skill_ler when a task
// matches (progressive disclosure: the prompt stays small). A skill of a
// project is seen by that project's agents and the universal ones. Agents
// write skills with skill_criar, which the owner approves; a routine can
// follow a skill, which makes it a workflow.

const (
	maxSkillDescription = 1024
	maxSkillBody        = 30000
	maxSkills           = 200
	// promptSkillChars bounds the skill list in the system prompt.
	promptSkillChars = 5000
)

// SkillRequest creates or edits a skill. Markdown is a whole SKILL.md
// (frontmatter with name and description, then the instructions), for
// importing one; the other fields win when both are sent.
type SkillRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Body        string  `json:"body"`
	ProjectID   *string `json:"project_id"`
	Markdown    string  `json:"markdown"`
}

func (s *Server) registerSkills(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/skills", s.admin(s.listSkills))
	mux.HandleFunc("POST /v1/skills", s.admin(s.createSkill))
	mux.HandleFunc("GET /v1/skills/starters", s.admin(s.listStarterSkills))
	mux.HandleFunc("GET /v1/skills/{id}", s.admin(s.getSkill))
	mux.HandleFunc("PUT /v1/skills/{id}", s.admin(s.updateSkill))
	mux.HandleFunc("DELETE /v1/skills/{id}", s.admin(s.deleteSkill))
}

// parseSkillMarkdown splits a SKILL.md into its frontmatter fields and body.
func parseSkillMarkdown(md string) (name, description, body string) {
	md = strings.TrimPrefix(strings.ReplaceAll(md, "\r\n", "\n"), "\ufeff")
	rest, ok := strings.CutPrefix(strings.TrimLeft(md, "\n"), "---\n")
	if !ok {
		return "", "", strings.TrimSpace(md)
	}
	front, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", "", strings.TrimSpace(md)
	}
	for _, line := range strings.Split(front, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			description = v
		}
	}
	_, after, _ = strings.Cut(after, "\n")
	return name, description, strings.TrimSpace(after)
}

// toSkill validates a request onto base (the current skill on edit).
func (s *Server) toSkill(ctx context.Context, req SkillRequest, base store.Skill) (store.Skill, error) {
	sk := base
	if md := strings.TrimSpace(req.Markdown); md != "" {
		n, d, b := parseSkillMarkdown(md)
		req.Name, req.Description, req.Body = orDefault(strings.TrimSpace(req.Name), n),
			orDefault(strings.TrimSpace(req.Description), d), orDefault(strings.TrimSpace(req.Body), b)
	}
	if base.ID == "" {
		sk.ID = slug(req.Name)
		sk.Name = sk.ID
	}
	sk.Description = strings.Join(strings.Fields(req.Description), " ")
	sk.Body = strings.TrimSpace(req.Body)
	if req.ProjectID != nil {
		sk.ProjectID = strings.TrimSpace(*req.ProjectID)
	}
	switch {
	case sk.ID == "":
		return sk, errors.New("dê um nome à skill (letras e números)")
	case sk.Description == "" || utf8.RuneCountInString(sk.Description) > maxSkillDescription:
		return sk, fmt.Errorf("a descrição diz quando usar a skill, em até %d caracteres", maxSkillDescription)
	case sk.Body == "" || len(sk.Body) > maxSkillBody || !utf8.ValidString(sk.Body):
		return sk, fmt.Errorf("as instruções têm de 1 a %d caracteres", maxSkillBody)
	}
	if sk.ProjectID != "" {
		if _, err := s.store.GetProject(ctx, sk.ProjectID); err != nil {
			return sk, errors.New("esse projeto não existe")
		}
	}
	return sk, nil
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSkills(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.store.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var req SkillRequest
	if !readJSONMax(w, r, &req, 2*maxSkillBody+8192) {
		return
	}
	sk, err := s.toSkill(r.Context(), req, store.Skill{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	list, err := s.store.ListSkills(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	for _, o := range list {
		if o.ID == sk.ID {
			http.Error(w, "já existe uma skill com esse nome", http.StatusConflict)
			return
		}
	}
	if len(list) >= maxSkills {
		http.Error(w, fmt.Sprintf("até %d skills", maxSkills), http.StatusBadRequest)
		return
	}
	sk.Author = store.ActorAdmin
	saved, err := s.store.SaveSkill(r.Context(), sk, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	cur, err := s.store.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	var req SkillRequest
	if !readJSONMax(w, r, &req, 2*maxSkillBody+8192) {
		return
	}
	sk, err := s.toSkill(r.Context(), req, cur)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.SaveSkill(r.Context(), sk, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSkill(r.Context(), r.PathValue("id"), store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listStarterSkills(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, starterSkills)
}

// skillSeen says whether an agent may use a skill: universal skills are for
// everyone, a project's skills for its agents and the universal ones.
func skillSeen(a store.Agent, sk store.Skill) bool {
	return sk.ProjectID == "" || a.ProjectID == "" || a.ProjectID == sk.ProjectID
}

func (s *Server) visibleSkills(ctx context.Context, a store.Agent) []store.Skill {
	list, err := s.store.ListSkills(ctx)
	if err != nil {
		return nil
	}
	out := []store.Skill{}
	for _, sk := range list {
		if skillSeen(a, sk) {
			out = append(out, sk)
		}
	}
	return out
}

// skillsBlock lists the skills an agent can use, name and description only.
func (s *Server) skillsBlock(ctx context.Context, a store.Agent) string {
	list := s.visibleSkills(ctx, a)
	var b strings.Builder
	b.WriteString("\n\nSkills: saber escrito pelo dono e pelo time. Quando um pedido casar com uma, leia com skill_ler antes de começar e siga os passos dela. " +
		"Quando o dono ensinar um jeito de fazer algo, ou depois de uma tarefa difícil que vai se repetir, ofereça guardar como skill (skill_criar).")
	if len(list) == 0 {
		b.WriteString("\nAinda não há skills.")
		return b.String()
	}
	projects, _ := s.store.ListProjects(ctx)
	sort.SliceStable(list, func(i, j int) bool { return list[i].ProjectID != "" && list[j].ProjectID == "" })
	for _, sk := range list {
		line := "\n- " + sk.Name + ": " + truncate(sk.Description, 300)
		if p, ok := projectByID(projects, sk.ProjectID); ok && a.ProjectID == "" {
			line += " (projeto " + p.Name + ")"
		}
		if b.Len()+len(line) > promptSkillChars {
			b.WriteString("\n- … (há mais skills; peça a lista com skill_ler sem nome)")
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// toolReadSkill loads a skill's instructions; without a name, the list.
func (s *Server) toolReadSkill(ctx context.Context, t *turn, a toolArgs) (string, error) {
	list := s.visibleSkills(ctx, t.agent)
	name := slug(a.Nome)
	if name == "" {
		if len(list) == 0 {
			return "Não há skills ainda.", nil
		}
		var b strings.Builder
		for _, sk := range list {
			b.WriteString("- " + sk.Name + ": " + sk.Description + "\n")
		}
		return strings.TrimSpace(b.String()), nil
	}
	for _, sk := range list {
		if sk.ID == name {
			s.step(t, AgentStep{Tool: "skill_ler", Summary: sk.Name})
			return "Skill " + sk.Name + " — " + sk.Description + "\n\n" + sk.Body, nil
		}
	}
	names := []string{}
	for _, sk := range list {
		names = append(names, sk.Name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("não há skill %q (ainda não há skills)", a.Nome)
	}
	return "", fmt.Errorf("não há skill %q (as skills: %s)", a.Nome, strings.Join(names, ", "))
}

// skillDraft is the skill a skill_criar call would save, and whether it
// replaces one.
func (s *Server) skillDraft(ctx context.Context, t *turn, a toolArgs) (store.Skill, bool, error) {
	project := t.agent.ProjectID
	if p := strings.TrimSpace(a.Projeto); p != "" {
		found, _, err := s.projectFolder(ctx, t.agent, p, "")
		if err != nil {
			return store.Skill{}, false, err
		}
		if found != nil {
			project = found.ID
		}
	}
	req := SkillRequest{Name: a.Nome, Description: a.Descricao, Body: a.Instrucoes, ProjectID: &project}
	cur, err := s.store.GetSkill(ctx, slug(a.Nome))
	replaces := err == nil
	if replaces && !skillSeen(t.agent, cur) {
		return store.Skill{}, false, errors.New("já existe uma skill com esse nome em outro projeto; escolha outro nome")
	}
	if !replaces {
		cur = store.Skill{}
	}
	sk, err := s.toSkill(ctx, req, cur)
	if err != nil {
		return store.Skill{}, false, err
	}
	if !replaces {
		if list, err := s.store.ListSkills(ctx); err == nil && len(list) >= maxSkills {
			return store.Skill{}, false, fmt.Errorf("já há %d skills, o máximo", maxSkills)
		}
	}
	return sk, replaces, nil
}

func (s *Server) toolCreateSkill(ctx context.Context, t *turn, a toolArgs) (string, error) {
	sk, replaces, err := s.skillDraft(ctx, t, a)
	if err != nil {
		return "", err
	}
	sk.Author = "agent:" + t.agent.ID
	saved, err := s.store.SaveSkill(ctx, sk, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	verb := "criada"
	if replaces {
		verb = "atualizada"
	}
	s.step(t, AgentStep{Tool: "skill_criar", Summary: saved.Name + " · " + truncate(saved.Description, 120)})
	return "Skill " + saved.Name + " " + verb + ". Ela já aparece para o time na lista de skills.", nil
}

// routineSkill checks the skill a routine follows: it must exist and be
// one the routine's agent can see.
func (s *Server) routineSkill(ctx context.Context, agent store.Agent, name string) (string, error) {
	id := slug(name)
	if id == "" {
		return "", nil
	}
	sk, err := s.store.GetSkill(ctx, id)
	if err != nil || !skillSeen(agent, sk) {
		return "", fmt.Errorf("não há skill %q para %s", name, agent.Name)
	}
	return sk.ID, nil
}

// routinePrompt is what a routine's agent receives: its instruction and,
// for a workflow, the steps of its skill.
func (s *Server) routinePrompt(ctx context.Context, rt store.Routine) string {
	if rt.Skill == "" {
		return rt.Prompt
	}
	sk, err := s.store.GetSkill(ctx, rt.Skill)
	if err != nil {
		return rt.Prompt + "\n\n(A skill " + rt.Skill + " desta rotina foi apagada; faça o pedido acima do seu jeito e avise o dono.)"
	}
	return rt.Prompt + "\n\nSiga a skill " + sk.Name + " (" + sk.Description + "), passo a passo:\n\n" + sk.Body
}

// starterSkills come with Avora; the owner adds the ones he wants.
var starterSkills = []store.Skill{
	{ID: "resumir-video", Name: "resumir-video", Description: "Baixar um vídeo (YouTube, Instagram, X, TikTok…) ou só as legendas e resumir o conteúdo.",
		Body: `1. Com ferramenta yt-dlp.info, veja título, duração e se há legendas.
2. Se houver legendas: yt-dlp.legendas (idioma pt ou en) e leia o arquivo .vtt com ler_arquivo.
3. Sem legendas: yt-dlp.baixar_audio e transcreva com whisper.transcrever (se existir em algum nó); senão, diga ao dono que falta uma ferramenta de transcrição.
4. Entregue: título, link, duração, um resumo em 5 a 10 tópicos e os trechos que valem a pena, com o minuto.
5. Pergunte se o dono quer o vídeo inteiro salvo (yt-dlp.baixar_video).`},
	{ID: "cortar-e-legendar", Name: "cortar-e-legendar", Description: "Cortar um trecho de um vídeo e queimar legendas nele, para Reels ou Shorts.",
		Body: `1. Confirme o arquivo (ou baixe com yt-dlp.baixar_video) e o trecho: início e fim (HH:MM:SS).
2. ffmpeg.cortar com inicio e fim para um arquivo novo, ex.: corte.mp4.
3. Para legendar: gere a legenda .srt do trecho (whisper.transcrever com formato srt, ou escreva com escrever_arquivo se o dono ditar o texto).
4. ffmpeg.legendar com o vídeo e a legenda, para um arquivo final, ex.: corte-legendado.mp4.
5. ffprobe.info no arquivo final para conferir duração e resolução, e diga onde ele ficou.`},
	{ID: "transcrever-audio", Name: "transcrever-audio", Description: "Transcrever um áudio ou vídeo (reunião, aula, áudio de WhatsApp) e resumir.",
		Body: `1. Se for vídeo, ffmpeg.extrair_audio para .wav.
2. whisper.transcrever no áudio, idioma pt (ou o do áudio).
3. Leia o .txt gerado com ler_arquivo.
4. Entregue um resumo, as decisões e as tarefas com responsável, e diga onde está a transcrição completa.`},
	{ID: "converter-documento", Name: "converter-documento", Description: "Converter documentos entre formatos (Markdown, Word, HTML, PDF, ODT) e juntar ou separar PDFs.",
		Body: `1. Descubra o arquivo de entrada e o formato pedido.
2. Documento de texto: pandoc.converter de entrada para saida (a extensão da saída escolhe o formato: .docx, .html, .md, .odt).
3. PDFs: qpdf.juntar para unir vários, qpdf.paginas para extrair páginas (ex.: 1-3,7).
4. Imagens: magick.converter para mudar o formato, magick.redimensionar para mudar o tamanho.
5. Diga onde o arquivo novo ficou e o tamanho dele.`},
	{ID: "ler-print", Name: "ler-print", Description: "Ler o texto de um print, foto de documento ou imagem escaneada (OCR).",
		Body: `1. Confirme a imagem (png, jpg) na pasta.
2. tesseract.ocr com idioma por (ou eng; os dois: por+eng).
3. Corrija erros óbvios do OCR sem inventar nada e entregue o texto organizado.
4. Se o texto tiver dados (valores, datas), ofereça montar uma tabela.`},
}
