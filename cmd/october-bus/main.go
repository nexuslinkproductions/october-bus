package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/october-dev/october-bus/bus"
)

const usage = `October Bus

Usage:
  october-bus start [--port <port>]
  october-bus status
  october-bus scope create [scope-id]
  october-bus agent run --id <id> --name <name> [--connect-to <peer>] -- <command> [args...]
  october-bus demo
  october-bus version
`

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func start(args []string) error {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	port := flags.Int("port", 0, "loopback port")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *port < 0 || *port > 65535 {
		return errors.New("port must be between 0 and 65535")
	}
	daemon, err := bus.StartDaemon(context.Background(), *port, nil)
	if err != nil {
		return err
	}
	fmt.Printf("October Bus listening on %s\n", daemon.RunFile.Address)
	fmt.Printf("Run file: %s\n", daemon.Paths.RunFile)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return daemon.Stop(shutdown)
}

func status() error {
	paths, err := bus.DefaultDaemonPaths()
	if err != nil {
		return err
	}
	run, err := bus.ReadRunFile(paths.RunFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	health, err := (bus.Client{Address: run.Address}).Health(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("October Bus is %s at %s\n", health.Status, run.Address)
	fmt.Printf("Protocol %s, pid %d\n", health.ProtocolVersion, run.PID)
	return nil
}

func createScope(id string) error {
	paths, err := bus.DefaultDaemonPaths()
	if err != nil {
		return err
	}
	run, err := bus.ReadRunFile(paths.RunFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := (bus.Client{Address: run.Address, Token: run.AdminToken}).CreateScope(ctx, bus.CreateScopeInput{ID: id})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func setEnvironment(base []string, values ...string) []string {
	replacements := map[string]bool{}
	for i := 0; i < len(values); i += 2 {
		replacements[strings.ToUpper(values[i])] = true
	}
	result := make([]string, 0, len(base)+len(values)/2)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if !replacements[strings.ToUpper(key)] {
			result = append(result, entry)
		}
	}
	for i := 0; i < len(values); i += 2 {
		result = append(result, values[i]+"="+values[i+1])
	}
	return result
}

func removeEnvironment(base []string, names ...string) []string {
	removed := map[string]bool{}
	for _, name := range names {
		removed[strings.ToUpper(name)] = true
	}
	result := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if !removed[strings.ToUpper(key)] {
			result = append(result, entry)
		}
	}
	return result
}

func runAgent(args []string) error {
	flags := flag.NewFlagSet("agent run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	id := flags.String("id", "", "stable agent id")
	name := flags.String("name", "", "agent display name")
	address := flags.String("address", "", "October Bus address")
	scopeTokenEnv := flags.String("scope-token-env", "OCTOBER_BUS_SCOPE_TOKEN", "environment variable containing the scope token")
	lease := flags.Duration("lease", 5*time.Minute, "execution lease")
	heartbeat := flags.Duration("heartbeat", 0, "heartbeat interval")
	var connectTo stringList
	var capabilities stringList
	flags.Var(&connectTo, "connect-to", "agent id to link, repeatable")
	flags.Var(&capabilities, "capability", "capability name, repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	commandArgs := flags.Args()
	if *id == "" || *name == "" || len(commandArgs) == 0 {
		return errors.New("agent run requires --id, --name, and a command after --")
	}
	if *scopeTokenEnv == "" {
		return errors.New("scope token environment variable name is required")
	}
	scopeToken := os.Getenv(*scopeTokenEnv)
	if scopeToken == "" {
		return fmt.Errorf("%s is required", *scopeTokenEnv)
	}
	resolvedAddress := *address
	if resolvedAddress == "" {
		resolvedAddress = os.Getenv("OCTOBER_BUS_ADDRESS")
	}
	if resolvedAddress == "" {
		paths, err := bus.DefaultDaemonPaths()
		if err != nil {
			return err
		}
		run, err := bus.ReadRunFile(paths.RunFile)
		if err != nil {
			return err
		}
		resolvedAddress = run.Address
	}
	declaredCapabilities := make([]bus.AgentCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		declaredCapabilities = append(declaredCapabilities, bus.AgentCapability{Name: capability})
	}
	session, err := bus.StartAgentSession(context.Background(), bus.AgentSessionOptions{
		Address: resolvedAddress, ScopeToken: scopeToken, HeartbeatInterval: *heartbeat,
		Registration: bus.RegisterAgentInput{
			ID: *id, DisplayName: *name, ConnectTo: connectTo,
			Capabilities: declaredCapabilities, LeaseMS: lease.Milliseconds(),
		},
	})
	if err != nil {
		return err
	}
	closeSession := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return session.Close(ctx)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	childCtx, stopChild := context.WithCancel(context.Background())
	defer stopChild()
	command := exec.CommandContext(childCtx, commandArgs[0], commandArgs[1:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = setEnvironment(removeEnvironment(os.Environ(), *scopeTokenEnv),
		"OCTOBER_BUS_ADDRESS", resolvedAddress,
		"OCTOBER_BUS_MCP_URL", resolvedAddress+"/mcp",
		"OCTOBER_BUS_AGENT_ID", session.Registration.AgentID,
		"OCTOBER_BUS_EXECUTION_ID", session.Registration.ExecutionID,
		"OCTOBER_BUS_AGENT_TOKEN", session.Registration.AgentToken,
	)
	if err := command.Start(); err != nil {
		_ = closeSession()
		return err
	}
	fmt.Fprintf(os.Stderr, "Agent %s connected to %s\n", session.Registration.AgentID, resolvedAddress)
	childDone := make(chan error, 1)
	go func() { childDone <- command.Wait() }()

	select {
	case childErr := <-childDone:
		if closeErr := closeSession(); childErr == nil {
			return closeErr
		}
		return childErr
	case <-session.Done():
		stopChild()
		<-childDone
		if sessionErr := session.Err(); sessionErr != nil {
			_ = closeSession()
			return fmt.Errorf("agent session ended: %w", sessionErr)
		}
		return closeSession()
	case <-signalCtx.Done():
		stopChild()
		<-childDone
		return closeSession()
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "start":
		return start(args[1:])
	case "status":
		return status()
	case "scope":
		if len(args) >= 2 && args[1] == "create" {
			id := ""
			if len(args) >= 3 {
				id = args[2]
			}
			return createScope(id)
		}
	case "agent":
		if len(args) >= 2 && args[1] == "run" {
			return runAgent(args[2:])
		}
	case "demo":
		return bus.RunDemo(context.Background())
	case "version":
		fmt.Printf("october-bus %s (protocol %s)\n", bus.Version, bus.ProtocolVersion)
		return nil
	}
	return fmt.Errorf("unknown command: %v\n\n%s", args, usage)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
