package a2abridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const AgentCardPath = "/.well-known/agent-card.json"

func NewAgentCardHandler(card *a2a.AgentCard) (http.Handler, error) {
	if card == nil {
		return nil, errors.New("agent card is required")
	}
	body, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	lastModified := time.Now().UTC().Truncate(time.Second).Format(http.TimeFormat)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Allow", "GET, HEAD, OPTIONS")
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		response.Header().Set("Cache-Control", "public, max-age=60")
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("ETag", etag)
		response.Header().Set("Last-Modified", lastModified)

		switch request.Method {
		case http.MethodOptions:
			response.WriteHeader(http.StatusNoContent)
		case http.MethodGet, http.MethodHead:
			if request.Header.Get("If-None-Match") == etag {
				response.WriteHeader(http.StatusNotModified)
				return
			}
			response.WriteHeader(http.StatusOK)
			if request.Method == http.MethodGet {
				_, _ = response.Write(body)
			}
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), nil
}
