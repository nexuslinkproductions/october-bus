package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxBodyBytes = 1024 * 1024

type ServerOptions struct {
	Host       string
	Port       int
	AdminToken string
	StartedAt  string
}

type Server struct {
	runtime      *Runtime
	options      ServerOptions
	httpServer   *http.Server
	listener     net.Listener
	mcpHandler   http.Handler
	address      string
	closeOnce    sync.Once
	serveDone    chan error
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

type mcpTokenKey struct{}

func NewServer(runtime *Runtime, options ServerOptions) *Server {
	if options.Host == "" {
		options.Host = "127.0.0.1"
	}
	if options.StartedAt == "" {
		options.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	server := &Server{
		runtime: runtime, options: options,
		serveDone: make(chan error, 1), shutdown: make(chan struct{}),
	}
	server.mcpHandler = mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		token, _ := request.Context().Value(mcpTokenKey{}).(string)
		if token == "" {
			return nil
		}
		return server.newMCPServer(token)
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxBodyBytes,
		PropagateRequestCancellation: true,
	})
	server.httpServer = &http.Server{
		Handler: server, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second,
	}
	return server
}

func (s *Server) Start() (string, error) {
	if s.listener != nil {
		return s.address, nil
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.options.Host, s.options.Port))
	if err != nil {
		return "", err
	}
	s.listener = listener
	s.address = "http://" + listener.Addr().String()
	go func() {
		err := s.httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		} else if err != nil {
			_ = listener.Close()
		}
		s.serveDone <- err
		close(s.serveDone)
	}()
	return s.address, nil
}

func (s *Server) Address() string { return s.address }

func (s *Server) Done() <-chan error { return s.serveDone }

func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }

func (s *Server) requestShutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdown) })
}

func (s *Server) Stop(ctx context.Context) error {
	var stopErr error
	s.closeOnce.Do(func() {
		if s.listener != nil {
			stopErr = s.httpServer.Shutdown(ctx)
		}
		if err := s.runtime.Close(); stopErr == nil {
			stopErr = err
		}
		s.address = ""
	})
	return stopErr
}

func bearer(request *http.Request) (string, error) {
	scheme, token, found := strings.Cut(request.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", Errorf(CodeUnauthenticated, "Bearer token is required")
	}
	return token, nil
}

func (s *Server) requireAdmin(request *http.Request) error {
	token, err := bearer(request)
	if err != nil || s.options.AdminToken == "" || !secureEqual(token, s.options.AdminToken) {
		return Errorf(CodeUnauthenticated, "Invalid admin token")
	}
	return nil
}

func decodeBody(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return Errorf(CodeInvalidArgument, "Request body exceeds 1 MiB")
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return Errorf(CodeInvalidArgument, "Request body must be valid JSON: "+err.Error())
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Errorf(CodeInvalidArgument, "Request body must contain one JSON value")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeResult(response http.ResponseWriter, status int, result any) {
	writeJSON(response, status, map[string]any{"ok": true, "result": result})
}

func writeFailure(response http.ResponseWriter, err error) {
	failure := AsBusError(err)
	writeJSON(response, ErrorStatus(failure), map[string]any{
		"ok":    false,
		"error": map[string]any{"code": failure.Code, "message": failure.Message},
	})
}

func pathID(value string) (string, error) {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", Errorf(CodeInvalidArgument, "Route identifier is invalid")
	}
	return decoded, nil
}

func pathParts(path, prefix string) []string {
	if !strings.HasPrefix(path, prefix) {
		return nil
	}
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return []string{}
	}
	return strings.Split(rest, "/")
}

func apiRouteMethods(path string) []string {
	switch path {
	case "/v1/admin/shutdown", "/v1/scopes", "/v1/links", "/v1/messages", "/v1/messages/ack", "/v1/inbox/reserve", "/v1/tasks", "/v1/escalations":
		if path == "/v1/tasks" {
			return []string{http.MethodGet, http.MethodPost}
		}
		return []string{http.MethodPost}
	case "/v1/agents":
		return []string{http.MethodGet, http.MethodPost}
	case "/v1/me/heartbeat":
		return []string{http.MethodPatch}
	case "/v1/peers", "/v1/scope/escalations":
		return []string{http.MethodGet}
	}
	if len(pathParts(path, "/v1/messages/")) == 1 {
		return []string{http.MethodGet}
	}
	inbox := pathParts(path, "/v1/inbox/")
	if len(inbox) == 2 && (inbox[1] == "commit" || inbox[1] == "release") {
		return []string{http.MethodPost}
	}
	tasks := pathParts(path, "/v1/tasks/")
	if len(tasks) == 2 && (tasks[1] == "claim" || tasks[1] == "release" || tasks[1] == "complete") {
		return []string{http.MethodPost}
	}
	if len(pathParts(path, "/v1/escalations/")) == 1 {
		return []string{http.MethodGet}
	}
	escalations := pathParts(path, "/v1/scope/escalations/")
	if len(escalations) == 2 && escalations[1] == "resolve" {
		return []string{http.MethodPost}
	}
	return nil
}

func methodAllowed(method string, allowed []string) bool {
	for _, value := range allowed {
		if method == value {
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/health" {
		if request.Method == http.MethodGet {
			writeJSON(response, http.StatusOK, Health{Name: "october-bus", ProtocolVersion: ProtocolVersion, RuntimeVersion: Version, Status: "ready", StartedAt: s.options.StartedAt})
		} else {
			response.Header().Set("Allow", http.MethodGet)
			writeFailure(response, Errorf(CodeMethodNotAllowed, "Method not allowed"))
		}
		return
	}
	if request.URL.Path == "/mcp" {
		token, err := bearer(request)
		if err == nil {
			_, err = s.runtime.Principal(request.Context(), token)
		}
		if err != nil {
			writeFailure(response, err)
			return
		}
		request = request.WithContext(context.WithValue(request.Context(), mcpTokenKey{}, token))
		s.mcpHandler.ServeHTTP(response, request)
		return
	}
	if err := s.serveAPI(response, request); err != nil {
		writeFailure(response, err)
	}
}

func (s *Server) serveAPI(response http.ResponseWriter, request *http.Request) error {
	ctx := request.Context()
	method, path := request.Method, request.URL.Path
	allowed := apiRouteMethods(path)
	if len(allowed) == 0 {
		return Errorf(CodeNotFound, "Route not found")
	}
	if !methodAllowed(method, allowed) {
		response.Header().Set("Allow", strings.Join(allowed, ", "))
		return Errorf(CodeMethodNotAllowed, "Method not allowed")
	}
	if method == http.MethodPost && path == "/v1/admin/shutdown" {
		if err := s.requireAdmin(request); err != nil {
			return err
		}
		var input emptyInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		writeResult(response, http.StatusAccepted, map[string]bool{"stopping": true})
		s.requestShutdown()
		return nil
	}
	if method == http.MethodPost && path == "/v1/scopes" {
		if err := s.requireAdmin(request); err != nil {
			return err
		}
		var input CreateScopeInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.CreateScope(ctx, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusCreated, result)
		return nil
	}
	if method == http.MethodPost && path == "/v1/agents" {
		token, err := bearer(request)
		if err != nil {
			return err
		}
		var input RegisterAgentInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.RegisterAgent(ctx, token, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusCreated, result)
		return nil
	}
	if method == http.MethodGet && path == "/v1/agents" {
		token, err := bearer(request)
		if err != nil {
			return err
		}
		result, err := s.runtime.ListAgents(ctx, token)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	if method == http.MethodPost && path == "/v1/links" {
		token, err := bearer(request)
		if err != nil {
			return err
		}
		var input struct {
			Left  string `json:"left"`
			Right string `json:"right"`
		}
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		if err := s.runtime.LinkAgents(ctx, token, input.Left, input.Right); err != nil {
			return err
		}
		writeResult(response, http.StatusOK, map[string]bool{"linked": true})
		return nil
	}
	token, err := bearer(request)
	if err != nil {
		return err
	}
	if method == http.MethodPatch && path == "/v1/me/heartbeat" {
		var input HeartbeatInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.Heartbeat(ctx, token, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	if method == http.MethodGet && path == "/v1/peers" {
		result, err := s.runtime.ListPeers(ctx, token)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	if method == http.MethodPost && path == "/v1/messages" {
		var input SendMessageInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.SendMessage(ctx, token, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusAccepted, result)
		return nil
	}
	if method == http.MethodPost && path == "/v1/messages/ack" {
		var input struct {
			MessageIDs []string `json:"messageIds"`
		}
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		count, err := s.runtime.AcknowledgeMessages(ctx, token, input.MessageIDs)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, map[string]int64{"acknowledged": count})
		return nil
	}
	if method == http.MethodGet {
		parts := pathParts(path, "/v1/messages/")
		if len(parts) == 1 {
			id, err := pathID(parts[0])
			if err != nil {
				return err
			}
			result, err := s.runtime.Receipt(ctx, token, id)
			if err != nil {
				return err
			}
			writeResult(response, http.StatusOK, result)
			return nil
		}
	}
	if method == http.MethodPost && path == "/v1/inbox/reserve" {
		var input struct {
			Limit int `json:"limit,omitempty"`
		}
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.ReserveInbox(ctx, token, input.Limit)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	if method == http.MethodPost {
		parts := pathParts(path, "/v1/inbox/")
		if len(parts) == 2 && (parts[1] == "commit" || parts[1] == "release") {
			id, err := pathID(parts[0])
			if err != nil {
				return err
			}
			if parts[1] == "commit" {
				result, err := s.runtime.CommitInbox(ctx, token, id)
				if err != nil {
					return err
				}
				writeResult(response, http.StatusOK, result)
			} else {
				if err := s.runtime.ReleaseInbox(ctx, token, id); err != nil {
					return err
				}
				writeResult(response, http.StatusOK, map[string]bool{"released": true})
			}
			return nil
		}
	}
	if method == http.MethodPost && path == "/v1/tasks" {
		var input AddTaskInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.AddTask(ctx, token, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusCreated, result)
		return nil
	}
	if method == http.MethodGet && path == "/v1/tasks" {
		result, err := s.runtime.ListTasks(ctx, token)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	if method == http.MethodPost {
		parts := pathParts(path, "/v1/tasks/")
		if len(parts) == 2 && (parts[1] == "claim" || parts[1] == "release" || parts[1] == "complete") {
			id, err := pathID(parts[0])
			if err != nil {
				return err
			}
			if parts[1] == "claim" {
				result, err := s.runtime.ClaimTask(ctx, token, id)
				if err != nil {
					return err
				}
				writeResult(response, http.StatusOK, result)
			} else if parts[1] == "release" {
				result, err := s.runtime.ReleaseTask(ctx, token, id)
				if err != nil {
					return err
				}
				writeResult(response, http.StatusOK, result)
			} else {
				var input struct {
					Note string `json:"note,omitempty"`
				}
				if err := decodeBody(response, request, &input); err != nil {
					return err
				}
				result, err := s.runtime.CompleteTask(ctx, token, id, input.Note)
				if err != nil {
					return err
				}
				writeResult(response, http.StatusOK, result)
			}
			return nil
		}
	}
	if method == http.MethodPost && path == "/v1/escalations" {
		var input AskHumanInput
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.AskHuman(ctx, token, input)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusCreated, result)
		return nil
	}
	if method == http.MethodGet && path == "/v1/scope/escalations" {
		result, err := s.runtime.ListEscalations(ctx, token)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	parts := pathParts(path, "/v1/escalations/")
	if method == http.MethodGet && len(parts) == 1 {
		id, err := pathID(parts[0])
		if err != nil {
			return err
		}
		result, err := s.runtime.Escalation(ctx, token, id)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	scopeParts := pathParts(path, "/v1/scope/escalations/")
	if method == http.MethodPost && len(scopeParts) == 2 && scopeParts[1] == "resolve" {
		id, err := pathID(scopeParts[0])
		if err != nil {
			return err
		}
		var input struct {
			Answer string `json:"answer"`
		}
		if err := decodeBody(response, request, &input); err != nil {
			return err
		}
		result, err := s.runtime.ResolveEscalation(ctx, token, id, input.Answer)
		if err != nil {
			return err
		}
		writeResult(response, http.StatusOK, result)
		return nil
	}
	return Errorf(CodeNotFound, "Route not found")
}

type emptyInput struct{}

func (s *Server) newMCPServer(token string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "october-bus", Version: Version}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_peers", Description: "List linked agents and their capabilities."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
			peers, err := s.runtime.ListPeers(ctx, token)
			return nil, map[string]any{"peers": peers}, err
		})
	type messagePeerInput struct {
		Peer           string        `json:"peer" jsonschema:"exact agent id preferred, or unique exact display name"`
		Message        string        `json:"message" jsonschema:"message body"`
		Mode           MessageMode   `json:"mode,omitempty" jsonschema:"notify, request, or response; use response when responseTo is set"`
		ResponseTo     string        `json:"responseTo,omitempty" jsonschema:"original request message id; requires mode response"`
		IdempotencyKey string        `json:"idempotencyKey,omitempty"`
		ExpiresInMS    int64         `json:"expiresInMs,omitempty"`
		Context        []ContextItem `json:"context,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "message_peer", Description: "Send a durable notification, request, or response to a linked peer."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input messagePeerInput) (*mcp.CallToolResult, any, error) {
			peer, err := s.resolvePeer(ctx, token, input.Peer)
			if err != nil {
				return nil, nil, err
			}
			result, err := s.runtime.SendMessage(ctx, token, SendMessageInput{
				To: peer.ID, Body: input.Message, Mode: input.Mode, ResponseTo: input.ResponseTo,
				IdempotencyKey: input.IdempotencyKey, ExpiresInMS: input.ExpiresInMS, Context: input.Context,
			})
			return nil, result, err
		})
	type inboxInput struct {
		Limit int `json:"limit,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "check_inbox", Description: "Receive durable messages waiting for this agent."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inboxInput) (*mcp.CallToolResult, any, error) {
			reservation, err := s.runtime.ReserveInbox(ctx, token, input.Limit)
			if err != nil || reservation == nil {
				if reservation == nil && err == nil {
					return nil, map[string]any{"messages": []Message{}}, nil
				}
				return nil, nil, err
			}
			messages, err := s.runtime.CommitInbox(ctx, token, reservation.ID)
			return nil, map[string]any{"messages": messages, "acknowledgement": "Call acknowledge_messages after processing."}, err
		})
	type acknowledgeInput struct {
		MessageIDs []string `json:"messageIds"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "acknowledge_messages", Description: "Acknowledge delivered messages after processing them."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input acknowledgeInput) (*mcp.CallToolResult, any, error) {
			count, err := s.runtime.AcknowledgeMessages(ctx, token, input.MessageIDs)
			return nil, map[string]int64{"acknowledged": count}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "add_task", Description: "Add a shared task with optional dependencies."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input AddTaskInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.AddTask(ctx, token, input)
			return nil, result, err
		})
	type taskIDInput struct {
		TaskID string `json:"taskId"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "claim_task", Description: "Claim a ready shared task."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input taskIDInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.ClaimTask(ctx, token, input.TaskID)
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "release_task", Description: "Release a task claimed by this execution."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input taskIDInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.ReleaseTask(ctx, token, input.TaskID)
			return nil, result, err
		})
	type completeTaskInput struct {
		TaskID string `json:"taskId"`
		Note   string `json:"note,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "complete_task", Description: "Complete a task claimed by this agent."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input completeTaskInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.CompleteTask(ctx, token, input.TaskID, input.Note)
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_tasks", Description: "List shared tasks and dependency state."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.ListTasks(ctx, token)
			return nil, map[string]any{"tasks": result}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "ask_user", Description: "Request human input or permission."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input AskHumanInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.AskHuman(ctx, token, input)
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_node_status", Description: "Return this agent's identity, lease, and lifecycle."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
			result, err := s.runtime.NodeStatus(ctx, token)
			return nil, result, err
		})
	return server
}

func (s *Server) resolvePeer(ctx context.Context, token, value string) (Agent, error) {
	if err := validateText(value, "peer", 256, false); err != nil {
		return Agent{}, err
	}
	peers, err := s.runtime.ListPeers(ctx, token)
	if err != nil {
		return Agent{}, err
	}
	for _, peer := range peers {
		if peer.ID == value {
			return peer, nil
		}
	}
	matches := []Agent{}
	for _, peer := range peers {
		if strings.EqualFold(peer.DisplayName, value) {
			matches = append(matches, peer)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Agent{}, Errorf(CodeNotFound, "Linked peer "+value+" was not found")
	}
	return Agent{}, Errorf(CodeConflict, "Peer "+value+" is ambiguous")
}
