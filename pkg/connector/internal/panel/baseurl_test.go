package panel

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		mount   string
		host    string
		tls     bool
		headers map[string]string
		want    string
	}{
		{"atrás do traefik", "/connector", "evolution_go:4000", false,
			map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evolutiongo.exemplo.com"},
			"https://evolutiongo.exemplo.com/connector"},
		{"proxy com múltiplos valores", "/connector", "interno", false,
			map[string]string{"X-Forwarded-Proto": "https, http", "X-Forwarded-Host": "a.exemplo.com, b"},
			"https://a.exemplo.com/connector"},
		{"acesso direto http", "/connector", "10.0.0.5:4000", false, nil,
			"http://10.0.0.5:4000/connector"},
		{"acesso direto tls", "/connector", "evolutiongo.exemplo.com", true, nil,
			"https://evolutiongo.exemplo.com/connector"},
		{"montado na raiz", "", "localhost:4500", false, nil,
			"http://localhost:4500"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			r := gin.New()
			g := r.Group(tc.mount)
			g.GET("/api/chatwoot/:instanceId", func(c *gin.Context) { got = publicBaseURL(c) })

			req := httptest.NewRequest(http.MethodGet, tc.mount+"/api/chatwoot/abc", nil)
			req.Host = tc.host
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			r.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.want {
				t.Fatalf("publicBaseURL = %q, esperado %q", got, tc.want)
			}
			if wh := WebhookURL(got, "abc"); wh != tc.want+"/chatwoot/webhook/abc" {
				t.Fatalf("WebhookURL = %q", wh)
			}
		})
	}
}
