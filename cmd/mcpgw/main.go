package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

var (
	serveHandler   = serveCommand
	serverHandler  = serverCommand
	apikeyHandler  = apikeyCommand
	policyHandler  = policyCommand
	healthHandler  = healthCommand
	migrateHandler = migrateCommand
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch strings.TrimSpace(args[0]) {
		case "--version", "version":
			printVersion(stdout)
			return nil
		case "--help", "-h", "help":
			printUsage(stdout)
			return nil
		}
	}

	opts, remaining, err := parseGlobalFlags(args)
	if err != nil {
		printUsage(stderr)
		return fmt.Errorf("parse flags: %w", err)
	}

	if err := applyGlobalOptions(opts); err != nil {
		return err
	}

	if len(remaining) == 0 {
		return serveHandler(nil)
	}

	command := strings.TrimSpace(remaining[0])
	commandArgs := remaining[1:]

	switch command {
	case "serve":
		return serveHandler(commandArgs)
	case "server":
		return serverHandler(commandArgs)
	case "apikey":
		return apikeyHandler(commandArgs)
	case "policy":
		return policyHandler(commandArgs)
	case "health":
		return healthHandler(commandArgs)
	case "migrate":
		return migrateHandler(commandArgs)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", command)
	}
}

type globalOptions struct {
	configPath string
	logLevel   string
	logFormat  string
}

func parseGlobalFlags(args []string) (globalOptions, []string, error) {
	fs := flag.NewFlagSet("mcpgw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts globalOptions
	fs.StringVar(&opts.configPath, "config", "", "reserved for future config file support")
	fs.StringVar(&opts.logLevel, "log-level", "", "override log level")
	fs.StringVar(&opts.logFormat, "log-format", "", "override log format")

	if err := fs.Parse(args); err != nil {
		return globalOptions{}, nil, err
	}

	return opts, fs.Args(), nil
}

func applyGlobalOptions(opts globalOptions) error {
	if strings.TrimSpace(opts.logLevel) != "" {
		if err := os.Setenv("MCPGW_LOG_LEVEL", strings.TrimSpace(opts.logLevel)); err != nil {
			return fmt.Errorf("apply global log-level: %w", err)
		}
	}
	if strings.TrimSpace(opts.logFormat) != "" {
		if err := os.Setenv("MCPGW_LOG_FORMAT", strings.TrimSpace(opts.logFormat)); err != nil {
			return fmt.Errorf("apply global log-format: %w", err)
		}
	}
	_ = opts.configPath
	return nil
}

func printVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "version=%s commit=%s build_date=%s\n", version, commit, buildDate)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: mcpgw [global flags] <command> [args]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  serve       Start gateway server")
	_, _ = fmt.Fprintln(w, "  server      Manage registered servers")
	_, _ = fmt.Fprintln(w, "  apikey      Manage API keys")
	_, _ = fmt.Fprintln(w, "  policy      Manage policy profiles")
	_, _ = fmt.Fprintln(w, "  health      Check gateway health")
	_, _ = fmt.Fprintln(w, "  migrate     Run database migrations")
	_, _ = fmt.Fprintln(w, "  version     Print build version information")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Global flags:")
	_, _ = fmt.Fprintln(w, "  --config      Reserved for future config file support")
	_, _ = fmt.Fprintln(w, "  --log-level   Override MCPGW_LOG_LEVEL")
	_, _ = fmt.Fprintln(w, "  --log-format  Override MCPGW_LOG_FORMAT")
}
