package chatwoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evolution-foundation/evolution-go/pkg/connector/internal/model"
)

func TestAddTokenOwnerToInbox(t *testing.T) {
	var gotBody map[string]any
	var calls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("api_access_token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/profile":
			_, _ = w.Write([]byte(`{"id": 17, "name": "Admin"}`))
		case "POST /api/v1/accounts/3/inbox_members":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"payload": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &model.ChatwootConfig{URL: srv.URL + "/", AccountID: "3", Token: "tok"}
	if err := NewHTTPClient().AddTokenOwnerToInbox(context.Background(), cfg, 42); err != nil {
		t.Fatalf("AddTokenOwnerToInbox: %v", err)
	}

	if len(calls) != 2 || calls[0] != "GET /api/v1/profile" || calls[1] != "POST /api/v1/accounts/3/inbox_members" {
		t.Fatalf("chamadas inesperadas: %v", calls)
	}
	if gotBody["inbox_id"] != float64(42) {
		t.Fatalf("inbox_id = %v, esperado 42", gotBody["inbox_id"])
	}
	ids, ok := gotBody["user_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != float64(17) {
		t.Fatalf("user_ids = %v, esperado [17]", gotBody["user_ids"])
	}
}

func TestAddTokenOwnerToInbox_PerfilSemID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := &model.ChatwootConfig{URL: srv.URL, AccountID: "3", Token: "tok"}
	if err := NewHTTPClient().AddTokenOwnerToInbox(context.Background(), cfg, 42); err == nil {
		t.Fatal("esperava erro quando o perfil não traz id")
	}
}
