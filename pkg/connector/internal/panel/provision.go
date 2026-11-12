package panel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/chatwoot"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/model"
)

// provision garante que a config habilitada tenha um inbox Chatwoot
// correspondente, com o webhook apontando para este conector.
//
// Semântica (corrigida após o incidente de duplicação de inbox):
//
//  1. Se a config já está vinculada a um inbox (cfg.InboxID != 0, caso de
//     re-save/remanejo): REUTILIZA esse inbox direto — nunca re-resolve pelo
//     nome. Só garante o webhook. Isso torna impossível recriar/duplicar um
//     inbox quando o operador o renomeia no Chatwoot. Se o inbox vinculado
//     não existe mais (404), o vínculo é zerado e a resolução segue abaixo.
//
//  2. Sem vínculo: procura um inbox existente pelo nome (cfg.NameInbox).
//
//  3. Não achou pelo nome: SÓ cria se cfg.AutoCreate estiver ligado. Com
//     auto-create desligado NUNCA cria — retorna erro claro para o operador.
//
// Quando cfg.Enabled é false, nenhuma chamada ao Chatwoot é feita.
func (h *Handler) provision(ctx context.Context, cfg *model.ChatwootConfig, webhookURL string) error {
	if !cfg.Enabled {
		return nil
	}
	if h.Chatwoot == nil {
		return fmt.Errorf("cliente chatwoot não configurado")
	}

	// 1) Já vinculado: reutiliza o inbox e só atualiza o webhook.
	if cfg.InboxID != 0 {
		err := h.Chatwoot.UpdateInboxWebhook(ctx, cfg, cfg.InboxID, webhookURL)
		if err == nil {
			return nil
		}
		// 404 = o inbox vinculado foi removido no Chatwoot; zera o vínculo e
		// re-resolve abaixo. Qualquer outro erro é propagado.
		var apiErr *chatwoot.APIError
		if !(errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound) {
			return fmt.Errorf("update inbox webhook (inbox %d): %w", cfg.InboxID, err)
		}
		cfg.InboxID = 0
	}

	// 2) Procura um inbox existente pelo nome.
	inboxID, err := h.Chatwoot.FindInboxByName(ctx, cfg, cfg.NameInbox)
	if err != nil {
		return fmt.Errorf("find inbox: %w", err)
	}

	// 3) Não achou: só cria com auto-create ligado; senão, nunca cria.
	if inboxID == 0 {
		if !cfg.AutoCreate {
			return fmt.Errorf("inbox %q não existe no Chatwoot e a criação automática (auto create) está desligada — crie o inbox no Chatwoot ou ligue o auto create", cfg.NameInbox)
		}
		inboxID, err = h.Chatwoot.CreateInbox(ctx, cfg, cfg.NameInbox, webhookURL)
		if err != nil {
			return fmt.Errorf("create inbox: %w", err)
		}
		// Inbox recém-criado não tem membros e não aparece na lateral
		// "Caixas de entrada": adiciona o dono do token. Falha aqui não
		// desfaz o provisionamento — dá para adicionar pela tela de
		// Colaboradores do inbox no Chatwoot.
		if err := h.Chatwoot.AddTokenOwnerToInbox(ctx, cfg, inboxID); err != nil {
			slog.Default().Warn("chatwoot connector: não foi possível adicionar o dono do token como membro do inbox",
				"instance", cfg.InstanceID, "inbox", inboxID, "error", err)
		}
	}

	cfg.InboxID = inboxID
	if err := h.Chatwoot.UpdateInboxWebhook(ctx, cfg, inboxID, webhookURL); err != nil {
		return fmt.Errorf("update inbox webhook: %w", err)
	}
	return nil
}
