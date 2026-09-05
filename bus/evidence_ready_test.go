package bus

import (
	"context"
	"testing"
	"time"
)

// TestEnqueueWakesNotReadyBlockedReserve documents why the runtime needs no
// ready-edge inbox notification: a host blocked in a waitMs reserve while NOT
// ready is already woken by the message enqueue notify (SendMessage). Queued
// deliveries therefore reach the host's own reservation loop without any
// server-side wake on the false->true ready transition. The host's obligation
// (per spec) is only to resume its reservation loop after reporting ready.
func TestEnqueueWakesNotReadyBlockedReserve(t *testing.T) {
	agents := setupAgents(t, ":memory:")
	defer agents.runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// reviewer is NOT ready (setupAgents registers with ready=0) and blocks in
	// a waitMs reserve on an empty inbox.
	result := make(chan *InboxReservation, 1)
	failure := make(chan error, 1)
	go func() {
		reservation, err := agents.runtime.ReserveInbox(ctx, agents.reviewerToken, 10, 2000)
		if err != nil {
			failure <- err
			return
		}
		result <- reservation
	}()

	time.Sleep(50 * time.Millisecond)
	receipt, err := agents.runtime.SendMessage(ctx, agents.plannerToken, SendMessageInput{To: "reviewer", Body: "queued while not-ready"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failure:
		t.Fatal(err)
	case reservation := <-result:
		if reservation == nil || len(reservation.Messages) != 1 || reservation.Messages[0].ID != receipt.MessageID {
			t.Fatalf("enqueue did not wake not-ready blocked reserve: %#v", reservation)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
