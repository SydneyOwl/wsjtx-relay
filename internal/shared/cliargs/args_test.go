package cliargs

import "testing"

func TestRejectSingleDashLongFlagsAllowsShortFlags(t *testing.T) {
	if err := RejectSingleDashLongFlags([]string{"-h"}); err != nil {
		t.Fatalf("expected short flag to be allowed, got %v", err)
	}
}

func TestRejectSingleDashLongFlagsRejectsLongFlags(t *testing.T) {
	err := RejectSingleDashLongFlags([]string{"-shared-secret=bad"})
	if err == nil {
		t.Fatal("expected single-dash long flag to be rejected")
	}
}
