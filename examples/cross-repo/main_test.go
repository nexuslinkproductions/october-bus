package main

import (
	"context"
	"testing"
)

func TestCrossRepoRoundTrip(t *testing.T) {
	ctx := context.Background()
	result, err := runFlow(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.BackendAckCount != 1 {
		t.Fatalf("expected backend ack count 1, got %d", result.BackendAckCount)
	}
	if result.FrontendAckCount != 1 {
		t.Fatalf("expected frontend ack count 1, got %d", result.FrontendAckCount)
	}
}
