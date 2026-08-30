package bus

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// AgentSessionOptions describes one managed agent execution.
type AgentSessionOptions struct {
	Address           string
	ScopeToken        string
	Registration      RegisterAgentInput
	HeartbeatInterval time.Duration
	InitialLifecycle  AgentLifecycle
	InitialReady      bool
	HTTP              *http.Client
}

// AgentSession keeps an agent execution registered and reachable while its
// harness process is running.
type AgentSession struct {
	Address      string
	Registration RegisterAgentResult
	Client       Client

	leaseMS   int64
	cancel    context.CancelFunc
	done      chan struct{}
	beatMu    sync.Mutex
	stateMu   sync.Mutex
	lifecycle AgentLifecycle
	ready     bool
	errMu     sync.Mutex
	err       error
	close     sync.Once
	closeErr  error
}

// StartAgentSession registers an execution and starts its heartbeat loop.
func StartAgentSession(ctx context.Context, options AgentSessionOptions) (*AgentSession, error) {
	leaseMS, err := normalizedLease(options.Registration.LeaseMS)
	if err != nil {
		return nil, err
	}
	interval := options.HeartbeatInterval
	if interval == 0 {
		interval = time.Duration(leaseMS) * time.Millisecond / 3
	}
	if interval <= 0 || interval >= time.Duration(leaseMS)*time.Millisecond {
		return nil, Errorf(CodeInvalidArgument, "heartbeat interval must be shorter than the execution lease")
	}
	lifecycle := options.InitialLifecycle
	if lifecycle == "" {
		lifecycle = LifecycleStarting
	}
	if err := validateLifecycle(lifecycle); err != nil {
		return nil, err
	}
	registrationInput := options.Registration
	registrationInput.LeaseMS = leaseMS
	scopeClient := Client{Address: options.Address, Token: options.ScopeToken, HTTP: options.HTTP}
	registration, err := scopeClient.RegisterAgent(ctx, registrationInput)
	if err != nil {
		return nil, err
	}
	agentClient := Client{Address: options.Address, Token: registration.AgentToken, HTTP: options.HTTP}
	if _, err := agentClient.Heartbeat(ctx, HeartbeatInput{Lifecycle: lifecycle, Ready: options.InitialReady, LeaseMS: leaseMS}); err != nil {
		return nil, err
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	session := &AgentSession{
		Address: options.Address, Registration: registration, Client: agentClient,
		leaseMS: leaseMS, cancel: cancel, done: make(chan struct{}),
		lifecycle: lifecycle, ready: options.InitialReady,
	}
	go session.heartbeat(heartbeatCtx, interval)
	return session, nil
}

func (s *AgentSession) heartbeat(ctx context.Context, interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.sendHeartbeat(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.errMu.Lock()
				s.err = err
				s.errMu.Unlock()
				return
			}
		}
	}
}

func (s *AgentSession) sendHeartbeat(ctx context.Context) (Agent, error) {
	s.beatMu.Lock()
	defer s.beatMu.Unlock()
	s.stateMu.Lock()
	lifecycle, ready := s.lifecycle, s.ready
	s.stateMu.Unlock()
	return s.Client.Heartbeat(ctx, HeartbeatInput{Lifecycle: lifecycle, Ready: ready, LeaseMS: s.leaseMS})
}

// SetState updates the lifecycle and readiness reported by this execution.
func (s *AgentSession) SetState(ctx context.Context, lifecycle AgentLifecycle, ready bool) (Agent, error) {
	if err := validateLifecycle(lifecycle); err != nil {
		return Agent{}, err
	}
	s.beatMu.Lock()
	defer s.beatMu.Unlock()
	s.stateMu.Lock()
	s.lifecycle, s.ready = lifecycle, ready
	s.stateMu.Unlock()
	return s.Client.Heartbeat(ctx, HeartbeatInput{Lifecycle: lifecycle, Ready: ready, LeaseMS: s.leaseMS})
}

// Done closes if the session context ends or heartbeat authority is lost.
func (s *AgentSession) Done() <-chan struct{} { return s.done }

// Err returns the heartbeat failure that ended the session, if any.
func (s *AgentSession) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

// Close stops heartbeats and marks the current execution offline.
func (s *AgentSession) Close(ctx context.Context) error {
	s.close.Do(func() {
		s.cancel()
		<-s.done
		s.beatMu.Lock()
		defer s.beatMu.Unlock()
		_, offlineErr := s.Client.Heartbeat(ctx, HeartbeatInput{Lifecycle: LifecycleOffline, Ready: false, LeaseMS: s.leaseMS})
		if heartbeatErr := s.Err(); heartbeatErr != nil {
			s.closeErr = heartbeatErr
		} else {
			s.closeErr = offlineErr
		}
	})
	return s.closeErr
}
