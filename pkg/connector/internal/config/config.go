// Package config monta a configuração do conector Chatwoot embutido.
//
// O conector é parte do evolution-go: autenticação (GLOBAL_API_KEY), broker
// (AMQP_URL), banco (o mesmo Postgres do evolution-go) e porta vêm da
// configuração do próprio evolution-go. Não há variáveis próprias.
package config

import "fmt"

type Config struct {
	// APIKey protege a API do painel; é a GLOBAL_API_KEY do evolution-go (a
	// mesma do login do manager, que o painel reaproveita).
	APIKey string

	PostgresDSN string // banco do evolution-go (POSTGRES_USERS_DB ou POSTGRES_HOST/...)
	AMQPURL     string // AMQP_URL do evolution-go

	// evolution-go (o próprio processo, via loopback)
	EvolutionBaseURL string // http://127.0.0.1:SERVER_PORT
	EvolutionAPIKey  string // GLOBAL_API_KEY do evolution-go (operações administrativas)
}

// Defaults são os valores herdados da configuração do evolution-go.
type Defaults struct {
	ServerPort   string
	GlobalAPIKey string
	AMQPURL      string
	PostgresDSN  string
}

// Load monta a configuração e valida os campos obrigatórios.
func Load(d Defaults) (*Config, error) {
	port := d.ServerPort
	if port == "" {
		port = "4000"
	}
	c := &Config{
		APIKey:           d.GlobalAPIKey,
		PostgresDSN:      d.PostgresDSN,
		AMQPURL:          d.AMQPURL,
		EvolutionBaseURL: "http://127.0.0.1:" + port,
		EvolutionAPIKey:  d.GlobalAPIKey,
	}
	var missing []string
	for _, kv := range []struct{ k, v string }{
		{"POSTGRES_USERS_DB", c.PostgresDSN},
		{"AMQP_URL", c.AMQPURL},
		{"GLOBAL_API_KEY", c.APIKey},
	} {
		if kv.v == "" {
			missing = append(missing, kv.k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("variáveis de ambiente obrigatórias ausentes: %v", missing)
	}
	return c, nil
}
