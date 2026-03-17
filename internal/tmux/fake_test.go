package tmux

import "testing"

func TestFakeTmux_SessionLifecycle(t *testing.T) {
	fake := &FakeTmux{}

	// No sessions initially
	sessions, err := fake.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}

	// Create a session
	if err := fake.NewSession("test-session", "/tmp"); err != nil {
		t.Fatal(err)
	}
	ok, err := fake.HasSession("test-session")
	if err != nil || !ok {
		t.Fatal("session should exist after NewSession")
	}

	// Rename it
	if err := fake.RenameSession("test-session", "renamed"); err != nil {
		t.Fatal(err)
	}
	ok, _ = fake.HasSession("test-session")
	if ok {
		t.Fatal("old name should not exist after rename")
	}
	ok, _ = fake.HasSession("renamed")
	if !ok {
		t.Fatal("new name should exist after rename")
	}

	// Kill it
	if err := fake.KillSession("renamed"); err != nil {
		t.Fatal(err)
	}
	ok, _ = fake.HasSession("renamed")
	if ok {
		t.Fatal("session should not exist after kill")
	}
}

func TestFakeTmux_CallRecording(t *testing.T) {
	fake := &FakeTmux{}
	fake.NewSession("s1", "/tmp")
	fake.SendKeys("s1", "echo hello")
	fake.HasSession("s1")

	if len(fake.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(fake.Calls))
	}
	if fake.Calls[0].Method != "NewSession" {
		t.Errorf("call 0 = %s, want NewSession", fake.Calls[0].Method)
	}
	if fake.Calls[1].Method != "SendKeys" {
		t.Errorf("call 1 = %s, want SendKeys", fake.Calls[1].Method)
	}
	if fake.Calls[2].Method != "HasSession" {
		t.Errorf("call 2 = %s, want HasSession", fake.Calls[2].Method)
	}
}

func TestFakeTmux_CapturePane(t *testing.T) {
	fake := &FakeTmux{
		CaptureOutput: map[string]string{
			"worker": "$ echo hello\nhello\n$",
		},
	}

	out, err := fake.CapturePane("worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if out != "$ echo hello\nhello\n$" {
		t.Errorf("unexpected capture output: %q", out)
	}
}

func TestFakeTmux_Environment(t *testing.T) {
	fake := &FakeTmux{}
	fake.SetEnvironment("sess", "KEY", "value")

	got, err := fake.GetEnvironment("sess", "KEY")
	if err != nil {
		t.Fatal(err)
	}
	if got != "value" {
		t.Errorf("GetEnvironment = %q, want %q", got, "value")
	}
}

func TestFakeTmux_ErrorInjection(t *testing.T) {
	fake := &FakeTmux{Err: ErrNoServer}

	_, err := fake.HasSession("any")
	if err != ErrNoServer {
		t.Errorf("expected ErrNoServer, got %v", err)
	}

	err = fake.NewSession("any", "/tmp")
	if err != ErrNoServer {
		t.Errorf("expected ErrNoServer, got %v", err)
	}
}

func TestFakeTmux_RenameNonexistent(t *testing.T) {
	fake := &FakeTmux{}
	err := fake.RenameSession("ghost", "new")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}
