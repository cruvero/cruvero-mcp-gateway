package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunRoutesCommands(t *testing.T) {
	origServe := serveHandler
	origServer := serverHandler
	origAPIKey := apikeyHandler
	origPolicy := policyHandler
	origHealth := healthHandler
	origMigrate := migrateHandler
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		serveHandler = origServe
		serverHandler = origServer
		apikeyHandler = origAPIKey
		policyHandler = origPolicy
		healthHandler = origHealth
		migrateHandler = origMigrate
		stdout = origStdout
		stderr = origStderr
	})

	var called string
	serveHandler = func(args []string) error {
		called = "serve"
		return nil
	}
	serverHandler = func(args []string) error {
		called = "server"
		return nil
	}
	apikeyHandler = func(args []string) error {
		called = "apikey"
		return nil
	}
	policyHandler = func(args []string) error {
		called = "policy"
		return nil
	}
	healthHandler = func(args []string) error {
		called = "health"
		return nil
	}
	migrateHandler = func(args []string) error {
		called = "migrate"
		return nil
	}

	cases := []struct {
		name    string
		args    []string
		expects string
	}{
		{name: "default serve", args: nil, expects: "serve"},
		{name: "serve command", args: []string{"serve"}, expects: "serve"},
		{name: "server command", args: []string{"server", "list"}, expects: "server"},
		{name: "apikey command", args: []string{"apikey", "list"}, expects: "apikey"},
		{name: "policy command", args: []string{"policy", "list"}, expects: "policy"},
		{name: "health command", args: []string{"health"}, expects: "health"},
		{name: "migrate command", args: []string{"migrate", "up"}, expects: "migrate"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			called = ""
			if err := run(tc.args); err != nil {
				t.Fatalf("run returned error: %v", err)
			}
			if called != tc.expects {
				t.Fatalf("expected %q handler, got %q", tc.expects, called)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	buf := &bytes.Buffer{}
	stdout = buf
	stderr = &bytes.Buffer{}

	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("run --version: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "version=") || !strings.Contains(got, "commit=") || !strings.Contains(got, "build_date=") {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	stderrBuf := &bytes.Buffer{}
	stdout = &bytes.Buffer{}
	stderr = stderrBuf

	err := run([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderrBuf.String(), "Usage:") {
		t.Fatalf("expected usage output, got %q", stderrBuf.String())
	}
}

func TestRunPropagatesSubcommandError(t *testing.T) {
	origServe := serveHandler
	serveHandler = func(args []string) error {
		_ = args
		return errors.New("boom")
	}
	t.Cleanup(func() {
		serveHandler = origServe
	})

	err := run([]string{"serve"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
