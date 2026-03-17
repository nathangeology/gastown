package mail

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

// Crash simulation tests for the two-phase delivery protocol (pending → acked).

func TestCrashBetweenPhase1AndPhase2(t *testing.T) {
	labels := []string{DeliveryLabelPending}
	state, by, at := ParseDeliveryLabels(labels)
	if state != DeliveryStatePending {
		t.Fatalf("state = %q, want %q", state, DeliveryStatePending)
	}
	if by != "" || at != nil {
		t.Fatalf("no ack metadata expected, got by=%q at=%v", by, at)
	}
}

func TestCrashAfterAckedByBeforeAckedAt(t *testing.T) {
	labels := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
	}
	state, by, at := ParseDeliveryLabels(labels)
	if state != DeliveryStatePending {
		t.Fatalf("state = %q, want %q", state, DeliveryStatePending)
	}
	if by != "" || at != nil {
		t.Fatalf("partial ack should not expose metadata, got by=%q at=%v", by, at)
	}
}

func TestCrashAfterAckedAtBeforeAckedLabel(t *testing.T) {
	labels := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
	}
	state, by, at := ParseDeliveryLabels(labels)
	if state != DeliveryStatePending {
		t.Fatalf("state = %q, want %q", state, DeliveryStatePending)
	}
	if by != "" || at != nil {
		t.Fatalf("without acked label, metadata should not be exposed, got by=%q at=%v", by, at)
	}
}

func TestRetryAfterCrashReusesTimestamp(t *testing.T) {
	existing := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
	}
	retryTime := time.Date(2026, 2, 17, 14, 0, 0, 0, time.UTC)
	got := DeliveryAckLabelSequenceIdempotent("gastown/worker", retryTime, existing)
	want := []string{
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
		DeliveryLabelAcked,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retry should reuse timestamp:\ngot  %v\nwant %v", got, want)
	}
}

func TestRetryAfterFullAckIsIdempotent(t *testing.T) {
	existing := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
		DeliveryLabelAcked,
	}
	retryTime := time.Date(2026, 2, 17, 15, 0, 0, 0, time.UTC)
	got := DeliveryAckLabelSequenceIdempotent("gastown/worker", retryTime, existing)
	want := []string{
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
		DeliveryLabelAcked,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retry after full ack should be idempotent:\ngot  %v\nwant %v", got, want)
	}
}

func TestCrashRecoveryWithDifferentRecipient(t *testing.T) {
	existing := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/workerA",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
	}
	workerBTime := time.Date(2026, 2, 17, 14, 0, 0, 0, time.UTC)
	got := DeliveryAckLabelSequenceIdempotent("gastown/workerB", workerBTime, existing)
	if got[1] != DeliveryLabelAckedAtPrefix+"2026-02-17T14:00:00Z" {
		t.Fatalf("workerB should get fresh timestamp, got %v", got)
	}
}

func TestCrashRecoveryMixedAckers(t *testing.T) {
	existing := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/workerA",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
		DeliveryLabelAcked,
		DeliveryLabelAckedByPrefix + "gastown/workerB",
	}
	workerBRetry := time.Date(2026, 2, 17, 14, 0, 0, 0, time.UTC)
	got := DeliveryAckLabelSequenceIdempotent("gastown/workerB", workerBRetry, existing)
	if got[1] != DeliveryLabelAckedAtPrefix+"2026-02-17T14:00:00Z" {
		t.Fatalf("mixed ackers should produce fresh timestamp, got %v", got)
	}
}

func TestConcurrentDeliveryAckLabelGeneration(t *testing.T) {
	existing := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			at := time.Date(2026, 2, 17, 14, 0, 0, 0, time.UTC)
			got := DeliveryAckLabelSequenceIdempotent("gastown/worker", at, existing)
			if len(got) != 3 {
				t.Errorf("expected 3 labels, got %d", len(got))
			}
		}()
	}
	wg.Wait()
}

func TestDeliverySendLabelsContainsPending(t *testing.T) {
	labels := DeliverySendLabels()
	if len(labels) != 1 || labels[0] != DeliveryLabelPending {
		t.Fatalf("DeliverySendLabels() = %v, want [%q]", labels, DeliveryLabelPending)
	}
}

func TestParseDeliveryLabels_EmptyLabels(t *testing.T) {
	state, by, at := ParseDeliveryLabels(nil)
	if state != "" || by != "" || at != nil {
		t.Fatalf("empty labels should return zero values, got state=%q by=%q at=%v", state, by, at)
	}
}

func TestParseDeliveryLabels_UnrelatedLabels(t *testing.T) {
	labels := []string{"from:mayor/", "thread:t-001", "msg-type:task"}
	state, by, at := ParseDeliveryLabels(labels)
	if state != "" || by != "" || at != nil {
		t.Fatalf("unrelated labels should return zero values, got state=%q by=%q at=%v", state, by, at)
	}
}

func TestParseDeliveryLabels_MalformedTimestamp(t *testing.T) {
	labels := []string{
		DeliveryLabelPending,
		DeliveryLabelAckedByPrefix + "gastown/worker",
		DeliveryLabelAckedAtPrefix + "not-a-timestamp",
		DeliveryLabelAcked,
	}
	state, by, at := ParseDeliveryLabels(labels)
	if state != DeliveryStateAcked {
		t.Fatalf("state = %q, want %q", state, DeliveryStateAcked)
	}
	if by != "gastown/worker" {
		t.Fatalf("by = %q, want %q", by, "gastown/worker")
	}
	if at != nil {
		t.Fatalf("malformed timestamp should yield nil ackedAt, got %v", at)
	}
}

func TestBeadsMessageDeliveryStateRoundtrip(t *testing.T) {
	bm := BeadsMessage{
		ID:       "hq-test",
		Title:    "Test",
		Assignee: "gastown/worker",
		Labels: []string{
			"from:mayor/",
			DeliveryLabelPending,
			DeliveryLabelAckedByPrefix + "gastown/worker",
			DeliveryLabelAckedAtPrefix + "2026-02-17T12:00:00Z",
			DeliveryLabelAcked,
		},
	}
	msg := bm.ToMessage()
	if msg.DeliveryState != DeliveryStateAcked {
		t.Fatalf("DeliveryState = %q, want %q", msg.DeliveryState, DeliveryStateAcked)
	}
	if msg.DeliveryAckedBy != "gastown/worker" {
		t.Fatalf("DeliveryAckedBy = %q, want %q", msg.DeliveryAckedBy, "gastown/worker")
	}
	if msg.DeliveryAckedAt == nil {
		t.Fatal("DeliveryAckedAt should not be nil")
	}
}
