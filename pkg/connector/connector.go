// Package connector embute o conector Chatwoot no evolution-go.
//
// Fluxos:
//   - Ingest:  RabbitMQ (filas globais publicadas pelo próprio evolution-go) -> API do Chatwoot
//   - Egress:  webhook do inbox Chatwoot -> POST /send/* do evolution-go (loopback)
//   - Painel:  UI + API de configuração por instância, servidos em /connector
//
// Está sempre ativo (iniciado pelo main do evolution-go). O RabbitMQ é o
// transporte para que nenhum evento se perca se o Chatwoot ficar fora do ar ou
// o processo reiniciar.
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/chatwoot"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/config"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/egress"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/evolution"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/ingest"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/panel"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/store"
	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/transport"
)

// BasePath é o prefixo HTTP onde o painel, a API e o webhook são servidos.
const BasePath = "/connector"

// Options são os valores herdados da configuração do evolution-go.
type Options struct {
	ServerPort   string
	GlobalAPIKey string
	AMQPURL      string
	PostgresDSN  string // banco do evolution-go; o conector cria suas tabelas nele
}

// Connector é uma instância em execução do conector.
type Connector struct {
	log      *slog.Logger
	st       store.Store
	consumer transport.Consumer
	cancel   context.CancelFunc
	done     chan struct{}
}

// Start valida a configuração, conecta no Postgres do conector, registra as
// rotas em BasePath no router informado e inicia o consumo do RabbitMQ em
// background. Deve ser chamado antes de o servidor HTTP começar a atender.
func Start(ctx context.Context, router gin.IRouter, opts Options) (*Connector, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "chatwoot-connector")

	cfg, err := config.Load(config.Defaults{
		ServerPort:   opts.ServerPort,
		GlobalAPIKey: opts.GlobalAPIKey,
		AMQPURL:      opts.AMQPURL,
		PostgresDSN:  opts.PostgresDSN,
	})
	if err != nil {
		return nil, fmt.Errorf("chatwoot connector: %w", err)
	}

	st, err := store.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("chatwoot connector: postgres: %w", err)
	}
	if err := st.Migrate(ctx); err != nil {
		st.Close()
		return nil, fmt.Errorf("chatwoot connector: migrations: %w", err)
	}

	locker := store.NewLocker()

	cw := chatwoot.NewHTTPClient()
	evo := evolution.NewHTTPClient(cfg.EvolutionBaseURL)

	// Ingest: eventos do evolution-go via RabbitMQ (filas globais).
	ingestSvc := ingest.New(st, cw, evo, locker, logger)
	consumer := transport.NewRabbitMQConsumer(cfg.AMQPURL,
		transport.WithLogger(logger),
		// historysync leva minutos por evento; com prefetch alto as entregas
		// paradas no buffer estouram o consumer_timeout do broker.
		transport.WithQueuePrefetch("historysync", 1))
	ingestSvc.Register(consumer)

	// Egress: webhook do Chatwoot → /send/* do evolution-go. O token da
	// instância é resolvido via GET /instance/info/{id} (admin) com cache.
	tokens := newTokenResolver(cfg)
	egressSvc := egress.NewService(st, cw, evo, tokens.resolve, logger)

	registerRoutes(router, cfg, st, cw, egressSvc)

	runCtx, cancel := context.WithCancel(ctx)
	c := &Connector{log: logger, st: st, consumer: consumer, cancel: cancel, done: make(chan struct{})}

	go func() {
		defer close(c.done)
		logger.Info("consumer rabbitmq iniciando", "queues", ingest.Queues())
		// Start só retorna com ctx cancelado/Close (reconecta sozinho) ou em
		// erro de configuração (nenhuma fila registrada).
		if err := consumer.Start(runCtx); err != nil && runCtx.Err() == nil {
			logger.Error("consumer rabbitmq encerrou com erro", "error", err)
		}
	}()

	logger.Info("conector chatwoot iniciado", "path", BasePath)
	return c, nil
}

func registerRoutes(router gin.IRouter, cfg *config.Config, st store.Store, cw chatwoot.Client, egressSvc *egress.Service) {
	// /connector (sem barra) -> /connector/, para os assets relativos do painel resolverem certo.
	router.GET(BasePath, func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, BasePath+"/")
	})

	g := router.Group(BasePath)

	// GET e HEAD: healthcheckers como `wget --spider` usam HEAD.
	health := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }
	g.GET("/health", health)
	g.HEAD("/health", health)

	// O webhook de cada inbox é registrado como https://<domínio do painel>/connector/chatwoot/webhook/{instanceId}.
	g.POST("/chatwoot/webhook/:instanceId", egress.Handler(egressSvc))

	panel.RegisterUI(g)
	panel.NewHandler(st, cw, cfg).Register(g)
}

// Close para o consumer e fecha o Postgres do conector.
func (c *Connector) Close() {
	if c == nil {
		return
	}
	c.cancel()
	_ = c.consumer.Close()
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		c.log.Warn("timeout aguardando o consumer encerrar")
	}
	c.st.Close()
}

// tokenResolver busca o token de uma instância no evolution-go
// (GET /instance/info/{id}, header apikey = GLOBAL_API_KEY; resposta
// {"message":"success","data":{... "token": "..."}}), com cache em memória.
type tokenResolver struct {
	cfg   *config.Config
	http  *http.Client
	mu    sync.Mutex
	cache map[string]tokenEntry
}

type tokenEntry struct {
	token   string
	expires time.Time
}

func newTokenResolver(cfg *config.Config) *tokenResolver {
	return &tokenResolver{
		cfg:   cfg,
		http:  &http.Client{Timeout: 15 * time.Second},
		cache: map[string]tokenEntry{},
	}
}

func (t *tokenResolver) resolve(ctx context.Context, instanceID string) (string, error) {
	t.mu.Lock()
	if e, ok := t.cache[instanceID]; ok && time.Now().Before(e.expires) {
		t.mu.Unlock()
		return e.token, nil
	}
	t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		t.cfg.EvolutionBaseURL+"/instance/info/"+instanceID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", t.cfg.EvolutionAPIKey)

	res, err := t.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token resolver: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token resolver: /instance/info/%s => HTTP %d", instanceID, res.StatusCode)
	}

	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("token resolver: resposta inválida: %w", err)
	}
	if body.Data.Token == "" {
		return "", fmt.Errorf("token resolver: instância %s sem token na resposta", instanceID)
	}

	t.mu.Lock()
	t.cache[instanceID] = tokenEntry{token: body.Data.Token, expires: time.Now().Add(10 * time.Minute)}
	t.mu.Unlock()
	return body.Data.Token, nil
}
