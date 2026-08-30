package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/october-dev/october-bus/bus"
)

func TestAgentRunHelper(t *testing.T) {
	if os.Getenv("OCTOBER_BUS_TEST_HELPER") != "1" {
		return
	}
	for _, name := range []string{
		"OCTOBER_BUS_ADDRESS",
		"OCTOBER_BUS_MCP_URL",
		"OCTOBER_BUS_AGENT_ID",
		"OCTOBER_BUS_EXECUTION_ID",
		"OCTOBER_BUS_AGENT_TOKEN",
	} {
		if os.Getenv(name) == "" {
			os.Exit(2)
		}
	}
	if os.Getenv("OCTOBER_BUS_SCOPE_TOKEN") != "" {
		os.Exit(3)
	}
}

func TestStopCommandUsesProtectedLocalEndpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTOBER_BUS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("OCTOBER_BUS_RUNTIME_DIR", filepath.Join(root, "run"))
	daemon, err := bus.StartDaemon(context.Background(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() {
		select {
		case <-daemon.Server.ShutdownRequested():
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stopped <- daemon.Stop(ctx)
		case <-time.After(time.Second):
			stopped <- context.DeadlineExceeded
		}
	}()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestRunAgentOwnsHeartbeatAndEnvironment(t *testing.T) {
	ctx := context.Background()
	runtimeValue, err := bus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	server := bus.NewServer(runtimeValue, bus.ServerOptions{})
	address, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	scope, err := runtimeValue.CreateScope(ctx, bus.CreateScopeInput{ID: "agent-run"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCTOBER_BUS_SCOPE_TOKEN", scope.ScopeToken)
	t.Setenv("OCTOBER_BUS_TEST_HELPER", "1")
	if err := runAgent([]string{
		"--id", "worker",
		"--name", "Worker",
		"--address", address,
		"--lease", "30s",
		"--heartbeat", "10ms",
		"--", os.Args[0], "-test.run=TestAgentRunHelper",
	}); err != nil {
		t.Fatal(err)
	}
	agents, err := (bus.Client{Address: address, Token: scope.ScopeToken}).ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		t.Fatalf("unexpected agents: %#v, %v", agents, err)
	}
	if agents[0].Lifecycle != bus.LifecycleOffline || agents[0].Ready || agents[0].Reachable {
		t.Fatalf("agent process did not leave an offline execution: %#v", agents[0])
	}
}

func TestSetEnvironmentReplacesExistingValues(t *testing.T) {
	result := setEnvironment([]string{"PATH=/bin", "OCTOBER_BUS_AGENT_TOKEN=old"},
		"OCTOBER_BUS_AGENT_TOKEN", "new",
		"OCTOBER_BUS_AGENT_ID", "worker",
	)
	if len(result) != 3 || result[0] != "PATH=/bin" || result[1] != "OCTOBER_BUS_AGENT_TOKEN=new" || result[2] != "OCTOBER_BUS_AGENT_ID=worker" {
		t.Fatalf("unexpected environment: %#v", result)
	}
}

func TestRemoveEnvironmentRemovesCredentials(t *testing.T) {
	result := removeEnvironment([]string{"PATH=/bin", "OCTOBER_BUS_SCOPE_TOKEN=secret", "OTHER=value"}, "october_bus_scope_token")
	if len(result) != 2 || result[0] != "PATH=/bin" || result[1] != "OTHER=value" {
		t.Fatalf("unexpected environment: %#v", result)
	}
}
