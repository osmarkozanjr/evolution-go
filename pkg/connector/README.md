# Conector Chatwoot (embutido)

Liga instâncias do evolution-go a inboxes do Chatwoot. Roda **sempre, dentro do
próprio processo do evolution-go**; não há container, imagem ou rota de proxy separados.

## Arquitetura

```
                         ┌─────────────────────┐
                         │       Chatwoot       │
                         │  (inbox API Channel) │
                         └──────────┬───────────┘
                                    │ webhook (message_created, ...)
                                    ▼
   ┌───────────────────────────────────────────────────────────────┐
   │                     evolution-go (:4000)                       │
   │                                                                │
   │  /manager  ── botão "Chatwoot" (manager/dist/assets/           │
   │               connector-menu.js)                               │
   │                                                                │
   │  /connector ─ pkg/connector                                    │
   │     ┌─────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────┐ │
   │     │ painel  │  │   egress    │  │   ingest    │  │ store  │ │
   │     │ UI/API  │  │ CW → /send/*│  │ fila → CW   │  │ PG/RD  │ │
   │     └─────────┘  └──────┬──────┘  └──────▲──────┘  └────────┘ │
   │                         │ loopback       │                     │
   │  /send/* ◄──────────────┘                │                     │
   │                                          │                     │
   │  produtor AMQP ──────┐                   │                     │
   └──────────────────────┼───────────────────┼─────────────────────┘
                          ▼                   │
                   ┌─────────────────┐        │
                   │    RabbitMQ      │────────┘
                   │ (filas globais)  │
                   └─────────────────┘
```

- **Ingest (WhatsApp → Chatwoot):** consome as filas globais que o próprio
  evolution-go publica (`AMQP_GLOBAL_ENABLED=true`) e cria
  contatos/conversas/mensagens no Chatwoot. O RabbitMQ é mantido como
  transporte de propósito: se o Chatwoot cair ou o container reiniciar, os
  eventos ficam na fila e nada se perde.
- **Egress (Chatwoot → WhatsApp):** recebe o webhook do inbox em
  `/connector/chatwoot/webhook/{instanceId}` e envia pela API `/send/*` do
  evolution-go (via `127.0.0.1`), inclusive revogando no WhatsApp mensagens
  excluídas no Chatwoot.
- **Painel:** `/connector/` — UI e API (`/connector/api/*`) para configurar
  cada instância.
- **Store:** o mesmo banco Postgres do evolution-go (`POSTGRES_USERS_DB` ou
  `POSTGRES_HOST`/...). As tabelas `chatwoot_configs`, `contact_mappings`,
  `conversation_mappings`, `message_mappings` e
  `chatwoot_schema_migrations` são criadas automaticamente no start. Locks em memória (o evolution-go é instância única),
  sem Redis.

Código: `connector.go` (wire-up, rotas e ciclo de vida) e `internal/`
(`chatwoot`, `config`, `egress`, `evolution`, `ingest`, `model`, `panel`,
`store`, `transport`).

## Configuração

O conector não tem variáveis próprias: usa o banco, o `AMQP_URL` e a
`GLOBAL_API_KEY` do evolution-go. O que ele exige do evolution-go:

| Variável | Para quê |
|---|---|
| `AMQP_URL` | broker de onde o conector consome os eventos (o evolution-go não sobe sem ele) |
| `AMQP_GLOBAL_ENABLED` / `AMQP_GLOBAL_EVENTS` | `true` e `MESSAGE,SEND_MESSAGE,CONNECTION,QRCODE,CONTACT,HISTORY_SYNC` — sem isso nenhum evento chega ao conector (há um aviso no log) |

Não há variável de URL pública: o endereço do webhook registrado no Chatwoot
(`https://<domínio>/connector/chatwoot/webhook/{instanceId}`) é montado a
partir da própria requisição ao salvar a instância no painel (host e
protocolo, respeitando `X-Forwarded-Host`/`X-Forwarded-Proto` do Traefik). Por
isso, **abra o painel pelo domínio público** do evolution-go, não por IP ou
endereço interno.

O conector fica atrás do mesmo gate de licença do resto da API: sem licença
ativada, `/connector/*` responde `503 LICENSE_REQUIRED` e o botão no manager
não aparece.

## Configurando uma instância no painel

1. No manager do evolution-go, abra as **configurações da instância**
   (ícone de engrenagem). Aparece o item **"Chatwoot"** na sidebar e um card no final da página.
2. O painel abre já autenticado: ele usa o login do manager (mesma
   `GLOBAL_API_KEY`), sem pedir chave. Sem login, redireciona para
   `/manager/login`.
3. O **instance ID já vem preenchido**. Informe:
   - a URL do Chatwoot e o **Token de Acesso** — o `api_access_token` de um
     usuário administrador (Configurações de Perfil → Access Token), não o
     token do inbox;
   - o `accountId` (o número em `/app/accounts/<id>` na URL do Chatwoot).
4. Inbox **nova**: marque **"Auto Create Inbox"** e o inbox é criado com o
   webhook já registrado. Inbox **existente**: copie a **Webhook URL** do
   final da página e cole em Chatwoot → Caixas de Mensagens → sua inbox →
   Configuração → Webhook URL.
5. **Salvar.** O conector cria/reaproveita o inbox, registra o webhook e passa
   a sincronizar a instância.

## Migrando da stack com o conector separado

Antes o conector era um service à parte (`evolution_connector`, imagem
`evolution-chatwoot-connector`) e o botão do manager era injetado por
`inject-menu.sh` via `docker config`. Para migrar:

1. Builde a imagem deste fork e use-a no service `evolution_go`.
2. No `evolution_go`: remova `entrypoint:` e `configs:`. Nenhuma variável
   do conector antigo (`CONNECTOR_*`, `EVOLUTION_BASE_URL`,
   `EVOLUTION_GLOBAL_API_KEY`) existe mais.
3. Remova o service `evolution_connector` (e suas labels Traefik) e a seção
   `configs:` da stack. Depois do deploy, os configs `inject_menu_sh` e
   `connector_menu_js` podem ser apagados com `docker config rm`.

4. Os dados do conector agora ficam no banco do evolution-go; o banco antigo
   `evogo_chatwoot` **não é migrado**. Depois do deploy, configure de novo
   cada instância no painel (`/connector/`) e salve — o conector registra o
   webhook no inbox. Com o mesmo nome de inbox ele reaproveita o inbox
   existente no Chatwoot em vez de criar outro. O banco `evogo_chatwoot` pode
   ser apagado quando não for mais necessário.

Veja a stack pronta em [`para_docker/`](../../para_docker/).

## Troubleshooting

**Mensagens do WhatsApp não chegam ao Chatwoot / fila acumulando**
- Procure no log `conector chatwoot iniciado` e `consumer rabbitmq iniciando`.
- Confira `AMQP_GLOBAL_ENABLED=true` e `AMQP_GLOBAL_EVENTS`; com o global
  desligado o evolution-go loga um aviso no start.
- Na management UI do RabbitMQ, as filas `message`, `sendmessage`,
  `connected`, `qrcode`, `contact`, `historysync`... devem ter 1 consumidor.

**401 / apikey inválida**
- Painel do conector redirecionando para o login: a sessão do manager
  expirou ou foi feita com uma chave que não é a `GLOBAL_API_KEY`. Entre de
  novo no manager com a `GLOBAL_API_KEY`.
- Chatwoot: confira o `api_access_token` cadastrado na instância (token de
  usuário/agente, não do inbox).

**Webhook do Chatwoot não chega no conector**
- Confira a Webhook URL mostrada no painel da instância: ela precisa ser o
  domínio público (`curl -i https://SEU-DOMINIO/connector/health`). Se
  aparecer um IP/host interno, o painel foi aberto por esse endereço; abra
  pelo domínio público e salve a instância de novo.
- Se o domínio mudou depois do cadastro, resalve a instância no painel para o
  conector registrar o webhook de novo.
