package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/attson/atterm/internal/rendezvouscontract"
)

const contractLogTag = "rendezvous-contract"

type options struct {
	url                   string
	origin                string
	allowInsecureLoopback bool
	timeout               time.Duration
}

func main() {
	logging.SetSink(os.Stderr)
	opts, err := parseOptions(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logging.Error(contractLogTag, "%v", err)
		os.Exit(2)
	}
	report, err := rendezvouscontract.Run(context.Background(), rendezvouscontract.Config{
		URL: opts.url, Origin: opts.origin,
		AllowInsecureLoopback: opts.allowInsecureLoopback, Timeout: opts.timeout,
	})
	if err != nil {
		logging.Error(contractLogTag, "%v", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		logging.Error(contractLogTag, "encode report: %v", err)
		os.Exit(1)
	}
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	opts := options{timeout: 15 * time.Second}
	flags := flag.NewFlagSet("atterm-rendezvous-contract", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.url, "url", strings.TrimSpace(getenv("ATTERM_RENDEZVOUS_TEST_URL")), "Rendezvous service origin")
	flags.StringVar(&opts.origin, "origin", strings.TrimSpace(getenv("ATTERM_RENDEZVOUS_TEST_ORIGIN")), "browser Origin sent during the contract")
	flags.BoolVar(&opts.allowInsecureLoopback, "allow-insecure-loopback", false, "allow HTTP/WS for an explicit loopback test service")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "whole contract timeout")
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("rendezvous contract: parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("rendezvous contract: unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	opts.url = strings.TrimSpace(opts.url)
	opts.origin = strings.TrimSpace(opts.origin)
	if opts.url == "" {
		return options{}, errors.New("rendezvous contract: --url is required")
	}
	if _, err := rendezvousclient.ParseEndpoint(opts.url, opts.allowInsecureLoopback); err != nil {
		return options{}, err
	}
	if opts.origin != "" {
		parsed, err := url.Parse(opts.origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return options{}, errors.New("rendezvous contract: --origin must contain only scheme and host")
		}
	}
	if opts.timeout < time.Second || opts.timeout > 2*time.Minute {
		return options{}, errors.New("rendezvous contract: --timeout must be between 1s and 2m")
	}
	return opts, nil
}
