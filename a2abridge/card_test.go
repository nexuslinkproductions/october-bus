package a2abridge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/october-dev/october-bus/a2abridge"
	"github.com/october-dev/october-bus/bus"
)

func TestAgentCardMapsPublicAgentFields(t *testing.T) {
	card, err := a2abridge.NewAgentCard(bus.Agent{
		ID:          "reviewer",
		DisplayName: "Reviewer",
		Capabilities: []bus.AgentCapability{
			{Name: "code_review", Description: "Reviews code changes."},
		},
		ExecutionID: "execution-secret",
	}, a2abridge.CardOptions{
		InterfaceURL: "https://agents.example.com/reviewer",
		Version:      "1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "Reviewer" || card.Version != "1.2.3" || len(card.Skills) != 1 {
		t.Fatalf("unexpected card: %#v", card)
	}
	if card.Skills[0].ID != "code_review" || card.Skills[0].Description != "Reviews code changes." {
		t.Fatalf("unexpected skills: %#v", card.Skills)
	}
	if len(card.SupportedInterfaces) != 1 || card.SupportedInterfaces[0].ProtocolBinding != a2a.TransportProtocolHTTPJSON || card.SupportedInterfaces[0].ProtocolVersion != a2a.Version {
		t.Fatalf("unexpected interfaces: %#v", card.SupportedInterfaces)
	}
	if _, ok := card.SecuritySchemes["bearer"].(a2a.HTTPAuthSecurityScheme); !ok {
		t.Fatalf("bearer security scheme is missing: %#v", card.SecuritySchemes)
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"execution-secret", "scopeToken", "agentToken"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("card contains private value %q", secret)
		}
	}
}

func TestAgentCardRequiresSecureRemoteURL(t *testing.T) {
	agent := bus.Agent{DisplayName: "Reviewer"}
	for _, value := range []string{
		"http://example.com/a2a",
		"https://user:secret@example.com/a2a",
		"https://example.com/a2a?token=secret",
		"file:///tmp/a2a",
	} {
		if _, err := a2abridge.NewAgentCard(agent, a2abridge.CardOptions{InterfaceURL: value}); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	for _, value := range []string{
		"http://127.0.0.1:8080/a2a",
		"http://[::1]:8080/a2a",
		"http://localhost:8080/a2a",
		"https://agents.example.com/a2a",
	} {
		if _, err := a2abridge.NewAgentCard(agent, a2abridge.CardOptions{InterfaceURL: value}); err != nil {
			t.Fatalf("expected %q to be accepted: %v", value, err)
		}
	}
}

func TestAgentCardHandlerWorksWithOfficialResolver(t *testing.T) {
	card, err := a2abridge.NewAgentCard(bus.Agent{DisplayName: "Reviewer"}, a2abridge.CardOptions{
		InterfaceURL: "https://agents.example.com/reviewer",
		Version:      "1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := a2abridge.NewAgentCardHandler(card)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(a2abridge.AgentCardPath, handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	resolver := agentcard.NewResolver(server.Client())
	resolved, err := resolver.Resolve(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != card.Name || resolved.Version != card.Version || len(resolved.SupportedInterfaces) != 1 {
		t.Fatalf("unexpected resolved card: %#v", resolved)
	}

	response, err := server.Client().Get(server.URL + a2abridge.AgentCardPath)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("Cache-Control") != "public, max-age=60" || response.Header.Get("ETag") == "" || response.Header.Get("Last-Modified") == "" {
		t.Fatalf("missing cache headers: %#v", response.Header)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+a2abridge.AgentCardPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-None-Match", response.Header.Get("ETag"))
	cached, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	cached.Body.Close()
	if cached.StatusCode != http.StatusNotModified {
		t.Fatalf("unexpected cache response: %d", cached.StatusCode)
	}
}

func TestAgentCardHandlerRejectsUnsupportedMethods(t *testing.T) {
	card, err := a2abridge.NewAgentCard(bus.Agent{DisplayName: "Reviewer"}, a2abridge.CardOptions{
		InterfaceURL: "https://agents.example.com/reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := a2abridge.NewAgentCardHandler(card)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, a2abridge.AgentCardPath, nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Fatalf("unexpected response: %d, %q", response.Code, response.Header().Get("Allow"))
	}
}
