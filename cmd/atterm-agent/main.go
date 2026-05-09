package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/attson/atterm/internal/agent"
	"github.com/attson/atterm/internal/hostid"
	"github.com/google/uuid"
)

func main() {
	relayURL := flag.String("relay", "ws://localhost:8080", "relay websocket base URL")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] -- <command> [args...]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	argv := flag.Args()
	if len(argv) == 0 {
		// default: launch the user's login shell
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		argv = []string{shell}
	}

	token := os.Getenv("ATTERM_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pty, err := agent.Start(ctx, argv, os.Environ())
	if err != nil {
		log.Fatalf("agent: start pty: %v", err)
	}
	defer pty.Close()

	sessionID := uuid.New()
	cli := agent.NewClient(agent.Options{
		RelayURL:  *relayURL,
		Token:     token,
		SessionID: sessionID,
		Command:   strings.Join(argv, " "),
		HostID:    hostid.Get(),
		Host:      hostname(),
		User:      username(),
	}, pty)

	fmt.Fprintf(os.Stderr, "atterm: session %s tracked at %s/?id=%s\r\n",
		sessionID, displayURL(*relayURL), sessionID)

	clientDone := make(chan struct{})
	go func() {
		_ = cli.Run(ctx)
		close(clientDone)
	}()

	waitErr := pty.Wait()
	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*os.SyscallError); ok {
			_ = ee
		}
		exitCode = pty.ExitCode()
	}
	cli.MarkExit(exitCode)
	stop()

	<-clientDone
	os.Exit(exitCode)
}

// hostname returns the machine's hostname, falling back to "unknown".
func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// username returns the OS username, or the numeric uid if user.Current fails
// (which happens in stripped-down containers without a passwd entry).
func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "uid" + strconv.Itoa(os.Getuid())
}

// displayURL converts ws://host:port to http://host:port for display.
func displayURL(s string) string {
	if strings.HasPrefix(s, "ws://") {
		return "http://" + strings.TrimPrefix(s, "ws://")
	}
	if strings.HasPrefix(s, "wss://") {
		return "https://" + strings.TrimPrefix(s, "wss://")
	}
	return s
}
