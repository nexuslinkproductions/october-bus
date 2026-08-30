package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/october-dev/october-bus/bus"
)

const usage = `October Bus

Usage:
  october-bus start [--port <port>]
  october-bus status
  october-bus scope create [scope-id]
  october-bus demo
  october-bus version
`

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
