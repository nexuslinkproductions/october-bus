package bus

import "context"

const maxExpiryMS int64 = 30 * 24 * 60 * 60 * 1000

type Runtime struct {
	store *Store
}

func Open(source string) (*Runtime, error) {
	store, err := OpenStore(source)
	if err != nil {
		return nil, err
	}
	return &Runtime{store: store}, nil
}

func (r *Runtime) Close() error { return r.store.Close() }

func (r *Runtime) CreateScope(ctx context.Context, input CreateScopeInput) (CreateScopeResult, error) {
	if err := validateIdentity(input.ID, "id", true); err != nil {
		return CreateScopeResult{}, err
	}
	return r.store.CreateScope(ctx, input.ID)
}

func (r *Runtime) RegisterAgent(ctx context.Context, scopeToken string, input RegisterAgentInput) (RegisterAgentResult, error) {
	scopeID, err := r.store.AuthenticateScope(ctx, scopeToken)
	if err != nil {
		return RegisterAgentResult{}, err
	}
	if err := validateIdentity(input.ID, "id", true); err != nil {
		return RegisterAgentResult{}, err
	}
	if err := validateText(input.DisplayName, "displayName", 256, false); err != nil {
		return RegisterAgentResult{}, err
	}
	if err := validateCapabilities(input.Capabilities); err != nil {
		return RegisterAgentResult{}, err
	}
	if len(input.ConnectTo) > 128 {
		return RegisterAgentResult{}, Errorf(CodeInvalidArgument, "connectTo exceeds 128 items")
	}
	for _, peer := range input.ConnectTo {
		if err := validateIdentity(peer, "connectTo", false); err != nil {
			return RegisterAgentResult{}, err
		}
	}
	input.LeaseMS, err = normalizedLease(input.LeaseMS)
	if err != nil {
		return RegisterAgentResult{}, err
	}
	if input.Capabilities == nil {
		input.Capabilities = []AgentCapability{}
	}
	return r.store.RegisterAgent(ctx, scopeID, input)
}

func (r *Runtime) ListAgents(ctx context.Context, scopeToken string) ([]Agent, error) {
	scopeID, err := r.store.AuthenticateScope(ctx, scopeToken)
	if err != nil {
		return nil, err
	}
	return r.store.ListAgents(ctx, scopeID)
}

func (r *Runtime) LinkAgents(ctx context.Context, scopeToken, left, right string) error {
	scopeID, err := r.store.AuthenticateScope(ctx, scopeToken)
	if err != nil {
		return err
	}
	if err := validateIdentity(left, "left", false); err != nil {
		return err
	}
	if err := validateIdentity(right, "right", false); err != nil {
		return err
	}
	return r.store.LinkAgents(ctx, scopeID, left, right)
}

func (r *Runtime) Principal(ctx context.Context, agentToken string) (Principal, error) {
	return r.store.AuthenticateAgent(ctx, agentToken)
}

func (r *Runtime) Heartbeat(ctx context.Context, agentToken string, input HeartbeatInput) (Agent, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return Agent{}, err
	}
	if err := validateLifecycle(input.Lifecycle); err != nil {
		return Agent{}, err
	}
	if input.Lifecycle == LifecycleOffline && input.Ready {
		return Agent{}, Errorf(CodeInvalidArgument, "offline agents cannot be ready")
	}
	input.LeaseMS, err = normalizedLease(input.LeaseMS)
	if err != nil {
		return Agent{}, err
	}
	return r.store.Heartbeat(ctx, principal, input)
}

func (r *Runtime) ListPeers(ctx context.Context, agentToken string) ([]Agent, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return nil, err
	}
	return r.store.ListPeers(ctx, principal)
}

func (r *Runtime) SendMessage(ctx context.Context, agentToken string, input SendMessageInput) (DeliveryReceipt, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateIdentity(input.To, "to", false); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateText(input.Body, "body", 65536, false); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateMessageMode(input.Mode); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateIdentity(input.ResponseTo, "responseTo", true); err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateText(input.IdempotencyKey, "idempotencyKey", 256, true); err != nil {
		return DeliveryReceipt{}, err
	}
	if input.ExpiresInMS < 0 || input.ExpiresInMS > maxExpiryMS {
		return DeliveryReceipt{}, Errorf(CodeInvalidArgument, "expiresInMs is invalid")
	}
	if err := validateContext(input.Context); err != nil {
		return DeliveryReceipt{}, err
	}
	if input.Context == nil {
		input.Context = []ContextItem{}
	}
	return r.store.SendMessage(ctx, principal, input)
}

func (r *Runtime) Receipt(ctx context.Context, agentToken, messageID string) (DeliveryReceipt, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if err := validateIdentity(messageID, "messageId", false); err != nil {
		return DeliveryReceipt{}, err
	}
	return r.store.Receipt(ctx, principal, messageID)
}

func (r *Runtime) ReserveInbox(ctx context.Context, agentToken string, limit int) (*InboxReservation, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, Errorf(CodeInvalidArgument, "limit must be between 1 and 100")
	}
	return r.store.ReserveInbox(ctx, principal, limit)
}

func (r *Runtime) CommitInbox(ctx context.Context, agentToken, reservationID string) ([]Message, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return nil, err
	}
	if err := validateIdentity(reservationID, "reservationId", false); err != nil {
		return nil, err
	}
	return r.store.CommitInbox(ctx, principal, reservationID)
}

func (r *Runtime) ReleaseInbox(ctx context.Context, agentToken, reservationID string) error {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return err
	}
	if err := validateIdentity(reservationID, "reservationId", false); err != nil {
		return err
	}
	return r.store.ReleaseInbox(ctx, principal, reservationID)
}

func (r *Runtime) AcknowledgeMessages(ctx context.Context, agentToken string, messageIDs []string) (int64, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return 0, err
	}
	if len(messageIDs) > 100 {
		return 0, Errorf(CodeInvalidArgument, "messageIds exceeds 100 items")
	}
	for _, messageID := range messageIDs {
		if err := validateIdentity(messageID, "messageIds", false); err != nil {
			return 0, err
		}
	}
	return r.store.AcknowledgeMessages(ctx, principal, messageIDs)
}

func (r *Runtime) AddTask(ctx context.Context, agentToken string, input AddTaskInput) (Task, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return Task{}, err
	}
	if err := validateText(input.Description, "description", 16384, false); err != nil {
		return Task{}, err
	}
	if len(input.Dependencies) > 128 {
		return Task{}, Errorf(CodeInvalidArgument, "dependencies exceeds 128 items")
	}
	for _, dependency := range input.Dependencies {
		if err := validateIdentity(dependency, "dependencies", false); err != nil {
			return Task{}, err
		}
	}
	return r.store.AddTask(ctx, principal, input)
}

func (r *Runtime) ClaimTask(ctx context.Context, agentToken, taskID string) (Task, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return Task{}, err
	}
	if err := validateIdentity(taskID, "taskId", false); err != nil {
		return Task{}, err
	}
	return r.store.ClaimTask(ctx, principal, taskID)
}

func (r *Runtime) ReleaseTask(ctx context.Context, agentToken, taskID string) (Task, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return Task{}, err
	}
	if err := validateIdentity(taskID, "taskId", false); err != nil {
		return Task{}, err
	}
	return r.store.ReleaseTask(ctx, principal, taskID)
}

func (r *Runtime) CompleteTask(ctx context.Context, agentToken, taskID, note string) (Task, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return Task{}, err
	}
	if err := validateIdentity(taskID, "taskId", false); err != nil {
		return Task{}, err
	}
	if err := validateText(note, "note", 16384, true); err != nil {
		return Task{}, err
	}
	return r.store.CompleteTask(ctx, principal, taskID, note)
}

func (r *Runtime) ListTasks(ctx context.Context, agentToken string) ([]Task, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return nil, err
	}
	return r.store.ListTasks(ctx, principal.ScopeID)
}

func (r *Runtime) AskHuman(ctx context.Context, agentToken string, input AskHumanInput) (HumanEscalation, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return HumanEscalation{}, err
	}
	if err := validateText(input.Question, "question", 4000, false); err != nil {
		return HumanEscalation{}, err
	}
	if len(input.Options) == 1 || len(input.Options) > 4 {
		return HumanEscalation{}, Errorf(CodeInvalidArgument, "options must be empty or contain between 2 and 4 values")
	}
	for _, option := range input.Options {
		if err := validateText(option, "options", 256, false); err != nil {
			return HumanEscalation{}, err
		}
	}
	return r.store.AskHuman(ctx, principal, input)
}

func (r *Runtime) Escalation(ctx context.Context, agentToken, escalationID string) (HumanEscalation, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return HumanEscalation{}, err
	}
	if err := validateIdentity(escalationID, "escalationId", false); err != nil {
		return HumanEscalation{}, err
	}
	return r.store.Escalation(ctx, principal.ScopeID, escalationID)
}

func (r *Runtime) ListEscalations(ctx context.Context, scopeToken string) ([]HumanEscalation, error) {
	scopeID, err := r.store.AuthenticateScope(ctx, scopeToken)
	if err != nil {
		return nil, err
	}
	return r.store.ListEscalations(ctx, scopeID)
}

func (r *Runtime) ResolveEscalation(ctx context.Context, scopeToken, escalationID, answer string) (HumanEscalation, error) {
	scopeID, err := r.store.AuthenticateScope(ctx, scopeToken)
	if err != nil {
		return HumanEscalation{}, err
	}
	if err := validateIdentity(escalationID, "escalationId", false); err != nil {
		return HumanEscalation{}, err
	}
	if err := validateText(answer, "answer", 16384, false); err != nil {
		return HumanEscalation{}, err
	}
	return r.store.ResolveEscalation(ctx, scopeID, escalationID, answer)
}

func (r *Runtime) NodeStatus(ctx context.Context, agentToken string) (NodeStatus, error) {
	principal, err := r.Principal(ctx, agentToken)
	if err != nil {
		return NodeStatus{}, err
	}
	agent, err := r.store.Agent(ctx, principal.ScopeID, principal.AgentID)
	if err != nil {
		return NodeStatus{}, err
	}
	return NodeStatus{
		Identity: NodeIdentity{AgentIdentity: principal.AgentIdentity, LeaseExpiresAt: instant(principal.LeaseExpiresAt)},
		Agent:    agent,
	}, nil
}
