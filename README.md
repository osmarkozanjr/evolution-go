<p align="center">
  <a href="https://evolutionfoundation.com.br">
    <img src="./public/hover-evolution.png" alt="Evolution Foundation" />
  </a>
</p>

<h1 align="center">Evolution Go</h1>

<p align="center">
  High-performance WhatsApp API built in Go — part of the Evolution Foundation ecosystem.
</p>

<p align="center">
  <a href="https://github.com/evolution-foundation/evolution-go/releases/latest"><img src="https://img.shields.io/github/v/release/evolution-foundation/evolution-go?include_prereleases&label=version&color=00ffa7" alt="Latest version" /></a>
  <a href="https://opensource.org/licenses/Apache-2.0"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache 2.0" /></a>
  <a href="https://docs.evolutionfoundation.com.br"><img src="https://img.shields.io/badge/Docs-evolutionfoundation.com.br-00ffa7" alt="Documentation" /></a>
  <a href="https://evolutionfoundation.com.br/community"><img src="https://img.shields.io/badge/Community-Join%20us-white" alt="Community" /></a>
  <a href="https://hub.docker.com/r/evoapicloud/evolution-go"><img src="https://img.shields.io/badge/Docker-evoapicloud-blue" alt="Docker image" /></a>
</p>

<p align="center">
  <a href="https://evolutionfoundation.com.br">Website</a> &middot;
  <a href="https://docs.evolutionfoundation.com.br">Documentation</a> &middot;
  <a href="https://evolutionfoundation.com.br/community">Community</a> &middot;
  <a href="mailto:suporte@evofoundation.com.br">Support</a>
</p>


---

## Por que este fork existe

Este é um fork do [evolution-foundation/evolution-go](https://github.com/evolution-foundation/evolution-go)
que resolve dois problemas do projeto original:

1. **Atualização da versão do WhatsApp Web** — instâncias caindo com `Client outdated (405)`.
2. **Integração com o Chatwoot** — o evolution-go não tem conector nativo; aqui ele vem embutido no próprio binário ([`pkg/connector/`](pkg/connector/)).

### 1. Erro 405 "Client outdated" e atualização automática da versão do WhatsApp Web

**Problema.** O WhatsApp recusa conexões de clientes cuja versão considera
desatualizada. Em 2026-11-02 a versão embutida no whatsmeow usado pela v0.7.2
(`2.3000.1042386815`) passou a ser rejeitada e **todas as instâncias deixaram de
reconectar** com `Client outdated (405) connect failure`.

O evolution-go já tentava evitar isso: no `StartClient` ele busca a versão atual
em `https://web.whatsapp.com/sw.js` (`fetchWhatsAppWebVersion`, cache de 1h).
Mas a versão buscada só era gravada em `store.DeviceProps.Version` — e o
handshake do whatsmeow usa a versão global definida por `store.SetWAVersion`.
Ou seja, **a busca automática acontecia, mas era ignorada**, e o cliente sempre
se apresentava com a versão fixa do whatsmeow.

**Correção** (combinação das PRs upstream
[#226](https://github.com/evolution-foundation/evolution-go/pull/226) e
[#227](https://github.com/evolution-foundation/evolution-go/pull/227)):

- **#226** — atualiza o whatsmeow para `v0.0.0-20261102152715-54038813c47f`
  (versão base `2.3000.1052556849`), Go 1.26 no `go.mod` e no `Dockerfile`, e
  adapta `pkg/user/service` às mudanças de API (`UpdateBlocklist`,
  `GetBlocklist`, `SetStatusMessage`). É a rede de segurança: se a busca no
  WhatsApp Web falhar, o cliente cai numa versão base recente.
- **#227** — aplica a versão obtida do WhatsApp Web com `store.SetWAVersion`
  em `pkg/whatsmeow/service/whatsmeow.go`. Assim, quando o WhatsApp sobe a
  versão mínima, as instâncias passam a usar a versão nova **sem precisar de
  rebuild nem de atualizar o whatsmeow**.

**Como se comporta:**

- Se `WHATSAPP_VERSION_MAJOR`/`MINOR`/`PATCH` estiverem definidas, elas têm
  prioridade e a versão fica fixa. **Deixe-as vazias para usar a atualização
  automática.**
- A versão é buscada/aplicada a cada `StartClient` (conectar/reconectar uma
  instância pela API ou no startup), respeitando o cache de 1h. Como
  `SetWAVersion` é global no processo, a última instância iniciada define a
  versão usada por todas.
- No log aparece `Setting whatsapp version from web to 2.3000.X` seguido de
  `Applied whatsmeow store.SetWAVersion to 2.3000.X`.

### 2. Conector Chatwoot embutido (`pkg/connector/`)

O evolution-go não tem integração nativa com o Chatwoot. Neste fork o
**evolution-chatwoot-connector** faz parte do próprio evolution-go: mesmo
binário, mesma imagem, mesma porta, sempre ativo.

- **WhatsApp → Chatwoot:** consome as filas globais do RabbitMQ que o próprio
  evolution-go publica (`AMQP_GLOBAL_ENABLED=true`) e cria
  contatos/conversas/mensagens no Chatwoot. A fila garante que nenhuma
  mensagem se perde se o Chatwoot cair ou o container reiniciar.
- **Chatwoot → WhatsApp:** recebe o webhook do inbox em
  `/connector/chatwoot/webhook/{instanceId}` e envia pela API `/send/*`
  (inclusive revogando no WhatsApp a mensagem excluída no Chatwoot).
- **Painel:** em `/connector/`, com um item **"Chatwoot"** nativo no manager
  (`manager/dist/assets/connector-menu.js`).

Não há variáveis novas: o painel usa o login do próprio manager e o conector
usa o mesmo banco Postgres, `AMQP_URL` e `GLOBAL_API_KEY` do evolution-go
(suas tabelas são criadas no banco do evolution-go no start). O `AMQP_URL`
passa a ser obrigatório, com `AMQP_GLOBAL_ENABLED=true`.

Documentação completa, configuração do painel e migração da stack antiga
(conector como service separado): [`pkg/connector/README.md`](pkg/connector/README.md).

### Build e deploy (`para_docker/`)

Uma imagem só tem a correção do 405 e o conector:

```bash
docker build --build-arg VERSION=0.7.4 -t osmarkozanjr/evolution_go:0.7.4 .
docker push osmarkozanjr/evolution_go:0.7.4   # opcional, ver abaixo
```

O `--build-arg VERSION` define a versão que o evolution-go reporta (log de
start e heartbeat de licença); sem ele a imagem sai como `dev`. O `docker
push` só é necessário se a imagem for rodar em outra máquina ou num Swarm
com vários nós — buildando no próprio nó manager, a imagem local basta.

A pasta [`para_docker/`](para_docker/) tem a stack Docker Swarm (Traefik) com
**evolution-go + RabbitMQ**:
`evolution-go-with-connector-example.yaml`. Copie para
`evolution-go-with-connector.yaml` (ignorado pelo git) e preencha imagem,
domínio, senhas e tokens.

Os bancos (`evogo_auth`, `evogo_users`) e as tabelas do conector são criados
automaticamente no start; só os volumes externos precisam existir:

```bash
# 1. volumes
docker volume create evolution_go_data
docker volume create evolution_go_logs
docker volume create rabbitmq_data

# 2. deploy
docker stack deploy -c para_docker/evolution-go-with-connector.yaml evolution
```

> **Proxy e QR code:** com `PROXY_HOST`/`PORT`/`USERNAME`/`PASSWORD`
> preenchidos (ex.: plano gratuito do webshare.io), o QR code pode não ser
> gerado. Deixe as variáveis `PROXY_*` vazias para parear as instâncias. Quando
> preenchidas, elas viram o proxy padrão de toda instância **nova** e ficam
> salvas na instância — esvaziar as variáveis depois não remove o proxy das
> instâncias já criadas; para isso use `DELETE /instance/proxy/{instanceId}`.

**Acesso:** a raiz do domínio redireciona para o manager (`/manager`); o
painel do conector fica em `/connector/`.

### Atualizando

A cada atualização do fork, gere a imagem com uma **tag nova** (a versão em
[`VERSION`](VERSION)) e aponte a stack para ela:

```bash
# na pasta do fork no servidor
git pull
cat VERSION                                   # ex.: 0.7.4
docker build --build-arg VERSION=0.7.4 -t osmarkozanjr/evolution_go:0.7.4 .

# no seu evolution-go-with-connector.yaml, troque a tag:
#   image: osmarkozanjr/evolution_go:0.7.4
docker stack deploy -c para_docker/evolution-go-with-connector.yaml evolution
```

- **Sempre tag nova:** se reaproveitar a mesma tag, o Swarm pode não perceber
  que a imagem mudou e manter o container antigo.
- **Confira:** o log de start mostra `Starting Evolution GO version 0.7.4`.
- **Voltar atrás:** a imagem anterior continua no servidor; recoloque a tag
  antiga no yml e rode o `docker stack deploy` de novo.
- **Limpeza:** imagens antigas podem ser removidas com
  `docker image rm osmarkozanjr/evolution_go:<tag-antiga>` quando a nova
  estiver estável.

---

## About

**Evolution Go** is a high-performance WhatsApp API built in Go. Part of the Evolution Foundation ecosystem, it provides a robust, lightweight solution for WhatsApp integration using the [whatsmeow](https://github.com/tulir/whatsmeow) library.

## Part of the Evolution Foundation ecosystem

Evolution Go is one of the messaging engines maintained by Evolution Foundation. It is used as a WhatsApp provider by the [Evo CRM Community](https://github.com/evolution-foundation/evo-crm-community) and other projects in the ecosystem.

---

## Features

- **High performance** — built with Go for minimal resource usage
- **RESTful API** — clean, well-documented REST endpoints with Swagger
- **Real-time events** — WebSocket, Webhook, AMQP/RabbitMQ and NATS support
- **Media support** — images, videos, audio, documents with MinIO/S3 storage
- **Message storage** — optional PostgreSQL persistence
- **QR code pairing** — built-in QR code generation for device linking
- **License management** — built-in licensing with registration, activation, and heartbeat
- **Docker ready** — production-ready Docker configuration

---

## Quick Start

### Docker (recommended)

```bash
git clone https://github.com/evolution-foundation/evolution-go.git
cd evolution-go
make docker-build
make docker-run
```

### Local development

```bash
git clone https://github.com/evolution-foundation/evolution-go.git
cd evolution-go

# Setup, configure and run
make setup
cp .env.example .env
make dev
```

> Run `make help` to see all available commands. See [COMMANDS.md](./COMMANDS.md) for detailed workflows.

---

## Configuration

Create a `.env` file:

```env
# Server
SERVER_PORT=8080
CLIENT_NAME=evolution

# Security
GLOBAL_API_KEY=your-secure-api-key-here

# Database
POSTGRES_AUTH_DB=postgresql://postgres:password@localhost:5432/evogo_auth?sslmode=disable
POSTGRES_USERS_DB=postgresql://postgres:password@localhost:5432/evogo_users?sslmode=disable
DATABASE_SAVE_MESSAGES=false

# Logging
WADEBUG=DEBUG
LOGTYPE=console

# Optional
# AMQP_URL=amqp://guest:guest@localhost:5672/
# NATS_URL=nats://localhost:4222
# WEBHOOK_URL=https://your-webhook-url.com/webhook
# MINIO_ENABLED=true
# MINIO_ENDPOINT=localhost:9000
# MINIO_ACCESS_KEY=minioadmin
# MINIO_SECRET_KEY=minioadmin
```

| Variable | Description | Default |
|---|---|---|
| `SERVER_PORT` | Server port | `8080` |
| `CLIENT_NAME` | Client identifier | `evolution` |
| `GLOBAL_API_KEY` | API authentication key | **Required** |
| `DATABASE_SAVE_MESSAGES` | Enable message storage | `false` |
| `WADEBUG` | WhatsApp debug level | `INFO` |

---

## License Activation

Evolution Go requires a license to operate. On first run:

1. Start the server — API endpoints return `503` until activated
2. Open the **Manager** at `http://localhost:8080/manager/login`
3. Enter your API URL and `GLOBAL_API_KEY`
4. Complete the license registration flow
5. Once activated, the API is fully operational

The license status persists in the database (`runtime_configs` table). Heartbeats are sent periodically to maintain activation.

---

## API Documentation

Swagger UI available at:

```
http://localhost:8080/swagger/index.html
```

### Key Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/instance/create` | Create WhatsApp instance |
| `GET` | `/instance/{name}/qrcode` | Get QR code for pairing |
| `POST` | `/message/sendText` | Send text message |
| `POST` | `/message/sendMedia` | Send media message |
| `GET` | `/instance/{name}/status` | Get instance status |
| `DELETE` | `/instance/{name}` | Delete instance |

---

## Project Structure

```
evolution-go/
├── cmd/evolution-go/     # Application entry point
├── pkg/
│   ├── core/            # License management & middleware
│   ├── instance/        # Instance management
│   ├── message/         # Message handling
│   ├── sendMessage/     # Message sending
│   ├── routes/          # HTTP routes
│   ├── middleware/      # Auth & validation middleware
│   ├── config/          # Configuration
│   ├── events/          # Event producers (AMQP, NATS, Webhook, WS)
│   └── storage/         # Media storage (MinIO)
├── docs/                # Swagger documentation
├── Dockerfile
├── Makefile
└── VERSION
```

---

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go 1.24+ |
| HTTP framework | Gin |
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) |
| Database | PostgreSQL |
| ORM | GORM |
| Message queue | RabbitMQ, NATS |
| Object storage | MinIO/S3 |
| Documentation | Swagger/OpenAPI |
| Container | Docker |

---

## Documentation

| Resource | Link |
|---|---|
| Website | [evolutionfoundation.com.br](https://evolutionfoundation.com.br) |
| Documentation | [docs.evolutionfoundation.com.br](https://docs.evolutionfoundation.com.br) |
| Community | [evolutionfoundation.com.br/community](https://evolutionfoundation.com.br/community) |
| Docker Hub | [evoapicloud/evolution-go](https://hub.docker.com/r/evoapicloud/evolution-go) |
| Changelog | [CHANGELOG.md](./CHANGELOG.md) |
| Contributing | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| Security | [SECURITY.md](./SECURITY.md) |

---

## Hosting

Deploy Evolution Go with optimized infrastructure through our HostGator partnership:

[**Evolution Go VPS — HostGator**](https://evolution-api.com/vps-evolution-go)

---

## Telemetry

Evolution Go collects anonymous telemetry data (routes used, API version) to help improve the service. **No sensitive or personal data is collected.**

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines on how to submit issues, propose features, and open pull requests.

Join our [community](https://evolutionfoundation.com.br/community) to discuss ideas and collaborate.

---

## Security

For security issues, **do not open a public issue**. Email **suporte@evofoundation.com.br** or use GitHub's private vulnerability reporting. See [SECURITY.md](./SECURITY.md) for details.

---

## Acknowledgments

- [whatsmeow](https://github.com/tulir/whatsmeow) by [Tulir Asokan](https://github.com/tulir) — WhatsApp protocol library
- [Evolution API](https://github.com/evolution-foundation/evolution-api) — Node.js sister project

---

## License

Evolution Go is licensed under the Apache License 2.0, with additional brand-protection conditions (LOGO/copyright preservation and Usage Notification requirement). See [LICENSE](./LICENSE) for full details.

For licensing inquiries, contact **suporte@evofoundation.com.br**.

## Trademarks

"Evolution Foundation", "Evolution" and "Evolution Go" are trademarks of Evolution Foundation. See [TRADEMARKS.md](./TRADEMARKS.md) for the brand assets policy.

Third-party attributions are documented in [NOTICE](./NOTICE).

---

<p align="center">
  Made by <a href="https://evolutionfoundation.com.br">Evolution Foundation</a> · © 2026
</p>
