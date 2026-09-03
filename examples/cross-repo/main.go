package main

import (
	"context"
	"fmt"
	"os"

	"github.com/october-dev/october-bus/bus"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "cross-repo example: %v\n", err)
		os.Exit(1)
	}
}

// run executes the full cross-repo flow and prints progress.
func run(ctx context.Context) error {
	_, err := runFlow(ctx, true)
	return err
}

// flowResult holds counts the test needs to assert.
type flowResult struct {
	BackendAckCount  int64
	FrontendAckCount int64
}

// runFlow executes the full cross-repo flow. Set verbose to true for
// the interactive example output; set to false for test silence.
func runFlow(ctx context.Context, verbose bool) (flowResult, error) {
	zero := flowResult{}

	// --- Spin up a local Bus daemon ---
	runtimeValue, err := bus.Open(":memory:")
	if err != nil {
		return zero, fmt.Errorf("open runtime: %w", err)
	}
	defer runtimeValue.Close()

	adminToken := "cross-repo-admin"
	server := bus.NewServer(runtimeValue, bus.ServerOptions{AdminToken: adminToken})
	address, err := server.Start()
	if err != nil {
		return zero, fmt.Errorf("start server: %w", err)
	}
	defer server.Stop(context.Background())

	// --- Create a shared scope (the "organization") ---
	admin := bus.Client{Address: address, Token: adminToken}
	scope, err := admin.CreateScope(ctx, bus.CreateScopeInput{ID: "cross-repo-org"})
	if err != nil {
		return zero, fmt.Errorf("create scope: %w", err)
	}
	scopeClient := bus.Client{Address: address, Token: scope.ScopeToken}

	// --- Register two agents in separate working directories ---
	// Agent "frontend" lives in a dir with only frontend context.
	// Agent "backend" lives in a separate dir with only backend context.
	// Neither shares its full workspace — they exchange only contracts.

	frontendReg, err := scopeClient.RegisterAgent(ctx, bus.RegisterAgentInput{
		ID:          "frontend",
		DisplayName: "Frontend Agent",
		Capabilities: []bus.AgentCapability{{Name: "ui_integration"}},
	})
	if err != nil {
		return zero, fmt.Errorf("register frontend: %w", err)
	}

	backendReg, err := scopeClient.RegisterAgent(ctx, bus.RegisterAgentInput{
		ID:           "backend",
		DisplayName:  "Backend Agent",
		Capabilities: []bus.AgentCapability{{Name: "api_development"}},
	})
	if err != nil {
		return zero, fmt.Errorf("register backend: %w", err)
	}

	// --- Link agents so they discover each other ---
	if err := scopeClient.LinkAgents(ctx, "frontend", "backend"); err != nil {
		return zero, fmt.Errorf("link agents: %w", err)
	}

	frontend := bus.Client{Address: address, Token: frontendReg.AgentToken}
	backend := bus.Client{Address: address, Token: backendReg.AgentToken}

	// --- Heartbeat both agents to ready ---
	if _, err := frontend.Heartbeat(ctx, bus.HeartbeatInput{Lifecycle: bus.LifecycleReady, Ready: true}); err != nil {
		return zero, fmt.Errorf("frontend heartbeat: %w", err)
	}
	if _, err := backend.Heartbeat(ctx, bus.HeartbeatInput{Lifecycle: bus.LifecycleReady, Ready: true}); err != nil {
		return zero, fmt.Errorf("backend heartbeat: %w", err)
	}

	// --- Discovery: each agent lists its peers ---
	if verbose {
		fmt.Println("=== Discovery ===")
	}
	fePeers, err := frontend.ListPeers(ctx)
	if err != nil {
		return zero, fmt.Errorf("frontend list peers: %w", err)
	}
	if verbose {
		fmt.Printf("frontend discovered: %s\n", fePeers[0].DisplayName)
	}

	bePeers, err := backend.ListPeers(ctx)
	if err != nil {
		return zero, fmt.Errorf("backend list peers: %w", err)
	}
	if verbose {
		fmt.Printf("backend discovered: %s\n", bePeers[0].DisplayName)
	}

	// --- Bounded context: frontend requests a contract change from backend ---
	// The frontend does NOT share its entire workspace. It sends only the
	// contract spec (bounded context) that the backend needs.
	if verbose {
		fmt.Println("\n=== Delegation (bounded context) ===")
	}
	contractSpec := bus.ContextItem{
		Kind:      "text",
		Title:     "Add GET /api/v2/users?include_posts",
		Text:      "Add an optional 'include_posts' query parameter to the users endpoint.",
		MediaType: "text/markdown",
	}

	receipt, err := frontend.SendMessage(ctx, bus.SendMessageInput{
		To:      "backend",
		Mode:    bus.MessageRequest,
		Body:    "Please add the include_posts query parameter to GET /api/v2/users.",
		Context: []bus.ContextItem{contractSpec},
	})
	if err != nil {
		return zero, fmt.Errorf("send request: %w", err)
	}
	if verbose {
		fmt.Printf("request sent: %s (state=%s)\n", receipt.MessageID, receipt.State)
	}

	// --- Create dependent tasks ---
	backendTask, err := frontend.AddTask(ctx, bus.AddTaskInput{
		Title:       "Add include_posts query parameter to GET /api/v2/users",
		Description: "Backend change required before frontend integration.",
	})
	if err != nil {
		return zero, fmt.Errorf("add backend task: %w", err)
	}
	if verbose {
		fmt.Printf("task created: %s (%s)\n", backendTask.ID, backendTask.Title)
	}

	frontendTask, err := frontend.AddTask(ctx, bus.AddTaskInput{
		Title:        "Integrate include_posts in the user dashboard",
		Description:  "Consume the new query parameter once the backend ships it.",
		Dependencies: []string{backendTask.ID},
	})
	if err != nil {
		return zero, fmt.Errorf("add frontend task: %w", err)
	}
	if verbose {
		fmt.Printf("task created: %s (%s, depends on: %s)\n", frontendTask.ID, frontendTask.Title, backendTask.ID)
	}

	// --- Backend claims its task ---
	if _, err := backend.ClaimTask(ctx, backendTask.ID); err != nil {
		return zero, fmt.Errorf("backend claim task: %w", err)
	}

	// --- Backend receives the message ---
	if verbose {
		fmt.Println("\n=== Reply + Ack ===")
	}
	reservation, err := backend.ReserveInbox(ctx, 10, 0)
	if err != nil {
		return zero, fmt.Errorf("backend reserve inbox: %w", err)
	}
	if reservation == nil || len(reservation.Messages) == 0 {
		return zero, fmt.Errorf("backend expected at least one message, got none")
	}
	committed, err := backend.CommitInbox(ctx, reservation.ID)
	if err != nil {
		return zero, fmt.Errorf("backend commit inbox: %w", err)
	}
	msg := committed[0]
	if verbose {
		fmt.Printf("backend received: %q (state=%s)\n", msg.Body, msg.State)
	}

	// --- Backend replies ---
	replyReceipt, err := backend.SendMessage(ctx, bus.SendMessageInput{
		To:         "frontend",
		Mode:       bus.MessageResponse,
		ResponseTo: msg.ID,
		Body:       "Done. include_posts is live on staging. Check GET /api/v2/users?include_posts=1.",
	})
	if err != nil {
		return zero, fmt.Errorf("backend send reply: %w", err)
	}
	if verbose {
		fmt.Printf("reply sent: %s (state=%s)\n", replyReceipt.MessageID, replyReceipt.State)
	}

	// --- Backend acknowledges the original request ---
	backendAckCount, err := backend.AcknowledgeMessages(ctx, []string{msg.ID})
	if err != nil {
		return zero, fmt.Errorf("backend ack: %w", err)
	}
	if verbose {
		fmt.Printf("backend acknowledged: %d message(s)\n", backendAckCount)
	}

	// --- Backend completes its task ---
	if _, err := backend.CompleteTask(ctx, backendTask.ID, "shipped"); err != nil {
		return zero, fmt.Errorf("backend complete task: %w", err)
	}

	// --- Frontend receives the reply ---
	frontendReservation, err := frontend.ReserveInbox(ctx, 10, 0)
	if err != nil {
		return zero, fmt.Errorf("frontend reserve inbox: %w", err)
	}
	if frontendReservation == nil || len(frontendReservation.Messages) == 0 {
		return zero, fmt.Errorf("frontend expected at least one message, got none")
	}
	frontendCommitted, err := frontend.CommitInbox(ctx, frontendReservation.ID)
	if err != nil {
		return zero, fmt.Errorf("frontend commit inbox: %w", err)
	}
	reply := frontendCommitted[0]
	if verbose {
		fmt.Printf("frontend received reply: %q (state=%s)\n", reply.Body, reply.State)
	}

	// --- Frontend acknowledges the reply ---
	frontendAckCount, err := frontend.AcknowledgeMessages(ctx, []string{reply.ID})
	if err != nil {
		return zero, fmt.Errorf("frontend ack: %w", err)
	}
	if verbose {
		fmt.Printf("frontend acknowledged: %d message(s)\n", frontendAckCount)
	}

	// --- Final delivery receipts ---
	if verbose {
		fmt.Println("\n=== Final Receipts ===")
	}
	requestReceipt, err := frontend.Receipt(ctx, receipt.MessageID)
	if err != nil {
		return zero, fmt.Errorf("request receipt: %w", err)
	}
	if verbose {
		fmt.Printf("request receipt: id=%s state=%s acknowledged=%s\n",
			requestReceipt.MessageID, requestReceipt.State, requestReceipt.AcknowledgedAt)
	}

	replyReceiptFinal, err := backend.Receipt(ctx, replyReceipt.MessageID)
	if err != nil {
		return zero, fmt.Errorf("reply receipt: %w", err)
	}
	if verbose {
		fmt.Printf("reply receipt:   id=%s state=%s acknowledged=%s\n",
			replyReceiptFinal.MessageID, replyReceiptFinal.State, replyReceiptFinal.AcknowledgedAt)
	}

	// --- Final task states ---
	if verbose {
		fmt.Println("\n=== Task States ===")
	}
	tasks, err := frontend.ListTasks(ctx, false)
	if err != nil {
		return zero, fmt.Errorf("list tasks: %w", err)
	}
	for _, t := range tasks {
		if verbose {
			fmt.Printf("task %s: %q status=%s ready=%v\n", t.ID, t.Title, t.Status, t.Ready)
		}
	}

	return flowResult{
		BackendAckCount:  backendAckCount,
		FrontendAckCount: frontendAckCount,
	}, nil
}
