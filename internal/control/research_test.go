package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
)

func TestResearchRunsCodexWithLiveSearch(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	setCalls(e, "Cora", call("pesquisar", map[string]any{"assunto": "casas de 4 quartos para alugar em São Sebastião do Paraíso até R$ 2.300",
		"formato": "tabela"}))
	s := chatServer(t, e)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	c := agentConversation(t, s, "cora")
	answer := sendAsync(t, s, c.ID, "acha casas")
	job := waitClaim(t, s, vps.Token)
	sp := job.Agent
	if sp == nil || sp.Engine != "codex" || sp.Model != "" || sp.Effort != "high" || !sp.Search || sp.Mode != "read" ||
		!strings.Contains(sp.Prompt, "São Sebastião do Paraíso") || !strings.Contains(sp.Prompt, "Entregue assim: tabela") ||
		!strings.HasPrefix(sp.Title, "Pesquisa: ") {
		t.Fatalf("spec %+v", sp)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token, JobResult{Success: true, Output: "3 casas achadas."})
	<-answer
}
