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

func TestRunHelp(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	buf := &bytes.Buffer{}
	stdout = buf
	stderr = &bytes.Buffer{}

	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run --help: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("expected Usage in help output, got %q", got)
	}
	if !strings.Contains(got, "Commands:") {
		t.Fatalf("expected Commands in help output, got %q", got)
	}
}

func TestRunHelpShort(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	buf := &bytes.Buffer{}
	stdout = buf
	stderr = &bytes.Buffer{}

	if err := run([]string{"-h"}); err != nil {
		t.Fatalf("run -h: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Fatalf("expected Usage in -h output, got %q", buf.String())
	}
}

func TestRunHelpCommand(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	buf := &bytes.Buffer{}
	stdout = buf
	stderr = &bytes.Buffer{}

	if err := run([]string{"help"}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Fatalf("expected Usage in help output, got %q", buf.String())
	}
}

func TestRunVersionSubcommand(t *testing.T) {
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		stdout = origStdout
		stderr = origStderr
	})

	buf := &bytes.Buffer{}
	stdout = buf
	stderr = &bytes.Buffer{}

	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run version: %v", err)
	}
	if !strings.Contains(buf.String(), "version=") {
		t.Fatalf("expected version= in output, got %q", buf.String())
	}
}

func TestRunRoutesAuthAndProxy(t *testing.T) {
	origAuth := authHandler
	origProxy := proxyHandler
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		authHandler = origAuth
		proxyHandler = origProxy
		stdout = origStdout
		stderr = origStderr
	})

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	var called string
	authHandler = func(args []string) error {
		called = "auth"
		return nil
	}
	proxyHandler = func(args []string) error {
		called = "mcp-proxy"
		return nil
	}

	called = ""
	if err := run([]string{"auth", "status"}); err != nil {
		t.Fatalf("run auth: %v", err)
	}
	if called != "auth" {
		t.Fatalf("expected auth handler, got %q", called)
	}

	called = ""
	if err := run([]string{"mcp-proxy", "--gateway-url", "http://example.com"}); err != nil {
		t.Fatalf("run mcp-proxy: %v", err)
	}
	if called != "mcp-proxy" {
		t.Fatalf("expected mcp-proxy handler, got %q", called)
	}
}

func TestRunWithGlobalLogFlags(t *testing.T) {
	origServe := serveHandler
	origStdout := stdout
	origStderr := stderr
	t.Cleanup(func() {
		serveHandler = origServe
		stdout = origStdout
		stderr = origStderr
	})

	serveHandler = func(args []string) error { return nil }
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	if err := run([]string{"--log-level", "debug", "--log-format", "text", "serve"}); err != nil {
		t.Fatalf("run with global flags: %v", err)
	}
}
