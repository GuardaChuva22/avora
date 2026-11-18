package connect

import "github.com/GuardaChuva22/avora/internal/mcp"

// MCPCatalog is the apps with an official remote MCP server that lets any
// client sign up and log in by itself (dynamic client registration). Each
// address was checked on 2026-11-11: it answers MCP over HTTP, asks for
// OAuth and publishes a registration endpoint. An app may still refuse a
// client it does not know at the consent step; the card then says so.
// Left out: Figma (refuses unknown clients) and Calendly (no loopback
// redirect). Exa and Hugging Face answer without a login.
func MCPCatalog() []*MCPApp {
	type e struct{ kind, name, url, desc string }
	groups := []struct {
		category string
		apps     []e
	}{
		{"Tarefas e projetos", []e{
			{"clickup", "ClickUp", "https://mcp.clickup.com/mcp", "Tarefas, listas, docs, comentários, chat e controle de tempo do seu workspace."},
			{"notion", "Notion", "https://mcp.notion.com/mcp", "Buscar, ler, criar e editar páginas e bancos de dados."},
			{"asana", "Asana", "https://mcp.asana.com/mcp", "Projetos, tarefas, prazos e comentários."},
			{"monday", "monday.com", "https://mcp.monday.com/mcp", "Quadros, itens, colunas e atualizações."},
			{"linear", "Linear", "https://mcp.linear.app/mcp", "Issues, projetos, ciclos e comentários."},
			{"atlassian", "Jira e Confluence", "https://mcp.atlassian.com/v1/mcp", "Issues do Jira e páginas do Confluence."},
			{"todoist", "Todoist", "https://ai.todoist.net/mcp", "Tarefas, projetos e prazos."},
			{"airtable", "Airtable", "https://mcp.airtable.com/mcp", "Bases, tabelas e registros."},
			{"miro", "Miro", "https://mcp.miro.com/", "Quadros, notas e diagramas."},
		}},
		{"Design e conteúdo", []e{
			{"canva", "Canva", "https://mcp.canva.com/mcp", "Criar, buscar e exportar designs."},
			{"gamma", "Gamma", "https://mcp.gamma.app/mcp", "Gerar apresentações, documentos e sites."},
			{"webflow", "Webflow", "https://mcp.webflow.com/mcp", "Sites, páginas e coleções do CMS."},
			{"wix", "Wix", "https://mcp.wix.com/mcp", "Site, loja, reservas e conteúdo."},
			{"wordpress", "WordPress.com", "https://public-api.wordpress.com/wpcom/v2/mcp/v1", "Posts, páginas e comentários dos seus sites."},
		}},
		{"Reuniões e agenda", []e{
			{"cal", "Cal.com", "https://mcp.cal.com/mcp", "Agendamentos e disponibilidade."},
			{"granola", "Granola", "https://mcp.granola.ai/mcp", "Notas e transcrições das suas reuniões."},
			{"fireflies", "Fireflies", "https://api.fireflies.ai/mcp", "Gravações, transcrições e resumos de reuniões."},
			{"tldv", "tl;dv", "https://mcp.tldv.io/mcp", "Reuniões gravadas, transcrições e destaques."},
		}},
		{"Vendas, marketing e atendimento", []e{
			{"brevo", "Brevo", "https://mcp.brevo.com/v1/brevo/mcp", "Contatos, listas e campanhas de e-mail e SMS."},
			{"semrush", "Semrush", "https://mcp.semrush.com/v1/mcp", "SEO, palavras-chave e concorrentes."},
			{"ahrefs", "Ahrefs", "https://api.ahrefs.com/mcp/mcp", "Backlinks, palavras-chave e tráfego."},
			{"attio", "Attio", "https://mcp.attio.com/mcp", "CRM: pessoas, empresas e negócios."},
			{"intercom", "Intercom", "https://mcp.intercom.com/mcp", "Conversas, contatos e artigos de ajuda."},
			{"jotform", "Jotform", "https://mcp.jotform.com/", "Formulários e respostas."},
		}},
		{"Pagamentos", []e{
			{"mercadopago", "Mercado Pago", "https://mcp.mercadopago.com/mcp", "Pagamentos, links de cobrança e relatórios."},
			{"stripe", "Stripe", "https://mcp.stripe.com", "Clientes, cobranças, assinaturas e links de pagamento."},
			{"paypal", "PayPal", "https://mcp.paypal.com/mcp", "Faturas, pedidos e transações."},
			{"square", "Square", "https://mcp.squareup.com/mcp", "Vendas, catálogo e clientes."},
		}},
		{"Arquivos, notas e pesquisa", []e{
			{"dropbox", "Dropbox", "https://mcp.dropbox.com/mcp", "Buscar, ler e organizar arquivos."},
			{"mem", "Mem", "https://mcp.mem.ai/mcp", "Suas notas e coleções."},
			{"raindrop", "Raindrop.io", "https://api.raindrop.io/rest/v2/ai/mcp", "Links e coleções salvos."},
			{"exa", "Exa", "https://mcp.exa.ai/mcp", "Busca na web feita para IA."},
			{"huggingface", "Hugging Face", "https://huggingface.co/mcp", "Modelos, datasets, Spaces e artigos."},
		}},
		{"Automação e dados", []e{
			{"zapier", "Zapier", "https://mcp.zapier.com/api/mcp/mcp", "As ações dos milhares de apps que você liga no Zapier."},
			{"make", "Make", "https://mcp.make.com", "Rodar e gerenciar seus cenários do Make."},
		}},
		{"Sites e desenvolvimento", []e{
			{"hostinger", "Hostinger", "https://mcp.hostinger.com", "Hospedagem, domínios, DNS e VPS."},
			{"vercel", "Vercel", "https://mcp.vercel.com", "Projetos, deploys e logs."},
			{"netlify", "Netlify", "https://netlify-mcp.netlify.app/mcp", "Sites, deploys e formulários."},
			{"supabase", "Supabase", "https://mcp.supabase.com/mcp", "Bancos, tabelas, funções e logs."},
			{"github", "GitHub", "https://api.githubcopilot.com/mcp/", "Repositórios, código, issues, pull requests e Actions."},
			{"gitlab", "GitLab", "https://gitlab.com/api/v4/mcp", "Repositórios, issues e merge requests."},
			{"sentry", "Sentry", "https://mcp.sentry.dev/mcp", "Erros e desempenho dos seus apps."},
			{"posthog", "PostHog", "https://mcp.posthog.com/mcp", "Analytics, funis e feature flags."},
			{"clickhouse", "ClickHouse Cloud", "https://mcp.clickhouse.cloud/mcp", "Consultas nos seus bancos ClickHouse."},
			{"prisma", "Prisma", "https://mcp.prisma.io/mcp", "Bancos Prisma Postgres."},
		}},
	}
	open := map[string]bool{"exa": true, "huggingface": true}
	// Apps whose server takes a personal token instead of a login.
	tokens := map[string][2]string{
		"github": {"Crie um token em GitHub → Settings → Developer settings → Fine-grained tokens, com acesso aos repositórios que quiser " +
			"(Contents, Issues, Pull requests e Actions; leitura, ou escrita se os agentes forem mexer). Cole o token (github_pat_...).",
			"https://github.com/settings/personal-access-tokens/new"},
	}
	out := []*MCPApp{}
	for _, g := range groups {
		for _, a := range g.apps {
			m := &MCPApp{Kind: a.kind, Name: a.name, URL: a.url, Description: a.desc, Category: g.category}
			if open[a.kind] {
				m.Auth = mcp.AuthNone
			}
			if t, ok := tokens[a.kind]; ok {
				m.Auth, m.Help, m.HelpURL = mcp.AuthToken, t[0], t[1]
			}
			out = append(out, m)
		}
	}
	return out
}
