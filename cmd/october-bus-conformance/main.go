package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/october-dev/october-bus/bus"
	"github.com/october-dev/october-bus/conformance"
)

func run() error {
	flags := flag.NewFlagSet("october-bus-conformance", flag.ContinueOnError)
	address := flags.String("address", os.Getenv("OCTOBER_BUS_ADDRESS"), "October Bus address")
	adminTokenEnv := flags.String("admin-token-env", "OCTOBER_BUS_ADMIN_TOKEN", "environment variable containing the admin token")
	timeout := flags.Duration("timeout", 2*time.Minute, "conformance timeout")
	format := flags.String("format", "json", "output format: json or text")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *format != "json" && *format != "text" {
		return errors.New("format must be json or text")
	}
	adminToken := os.Getenv(*adminTokenEnv)
	if *address == "" || adminToken == "" {
		paths, err := bus.DefaultDaemonPaths()
		if err != nil {
			return err
		}
		runFile, err := bus.ReadRunFile(paths.RunFile)
		if err != nil {
			return err
		}
		if *address == "" {
			*address = runFile.Address
		}
		if adminToken == "" && *address == runFile.Address {
			adminToken = runFile.AdminToken
		}
	}
	if *address == "" || adminToken == "" {
		return errors.New("address and admin token are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, runErr := conformance.Run(ctx, conformance.Options{Address: *address, AdminToken: adminToken})
	if *format == "text" {
		fmt.Printf("October Bus %s conformance\n", result.Profile)
		for _, name := range result.Passed {
			fmt.Printf("PASS %s\n", name)
		}
		for _, failure := range result.Failed {
			fmt.Printf("FAIL %s: %s\n", failure.Check, failure.Error)
		}
		fmt.Printf("Runtime %s, protocol %s\n", result.RuntimeVersion, result.ProtocolVersion)
	} else {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	}
	return runErr
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
