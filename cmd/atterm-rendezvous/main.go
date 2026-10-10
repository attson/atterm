package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/rendezvous"
)

const rendezvousLogTag = "rendezvous"

type options struct {
	addr                      string
	origins                   []string
	tlsCert                   string
	tlsKey                    string
	useTLS                    bool
	behindTLSProxy            bool
	allowNonLoopbackProxy     bool
	devInsecure               bool
	logLevel                  string
	maxConnections            int
	maxConnectionsPerIP       int
	maxConnectionsPerTopic    int
	maxMailboxPerTopic        int
	maxMailboxGlobal          int
	maxRecentMessages         int
	maxMessagesPerMinutePerIP int
}

func main() {
	logging.SetSink(os.Stderr)
	logging.SetLevel(logging.LevelInfo)
	opts, err := parseOptions(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logging.Error(rendezvousLogTag, "%v", err)
		os.Exit(2)
	}
	logging.SetLevel(logging.ParseLevelOr(opts.logLevel, logging.LevelInfo))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, opts); err != nil {
		logging.Error(rendezvousLogTag, "%v", err)
		os.Exit(1)
	}
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	var opts options
	envLimitKeys := []string{
		"ATTERM_RENDEZVOUS_MAX_CONNECTIONS",
		"ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_IP",
		"ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_TOPIC",
		"ATTERM_RENDEZVOUS_MAX_MAILBOX_PER_TOPIC",
		"ATTERM_RENDEZVOUS_MAX_MAILBOX_GLOBAL",
		"ATTERM_RENDEZVOUS_MAX_RECENT_MESSAGES",
		"ATTERM_RENDEZVOUS_MAX_MESSAGES_PER_MINUTE_PER_IP",
	}
	envLimits := make([]int, len(envLimitKeys))
	for index, key := range envLimitKeys {
		value, err := envInt(getenv, key)
		if err != nil {
			return options{}, err
		}
		envLimits[index] = value
	}
	flags := flag.NewFlagSet("atterm-rendezvous", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	origins := flags.String("origins", getenv("ATTERM_RENDEZVOUS_ORIGINS"), "comma-separated browser Origin allow-list")
	flags.StringVar(&opts.addr, "addr", envOr(getenv, "ATTERM_RENDEZVOUS_ADDR", ":8443"), "listen address")
	flags.StringVar(&opts.tlsCert, "tls-cert", getenv("ATTERM_RENDEZVOUS_TLS_CERT"), "TLS certificate PEM")
	flags.StringVar(&opts.tlsKey, "tls-key", getenv("ATTERM_RENDEZVOUS_TLS_KEY"), "TLS private key PEM")
	flags.BoolVar(&opts.behindTLSProxy, "behind-tls-proxy", false, "serve plaintext behind a TLS-terminating reverse proxy")
	flags.BoolVar(&opts.allowNonLoopbackProxy, "allow-non-loopback-proxy", false, "allow a containerized proxy backend to listen beyond loopback")
	flags.BoolVar(&opts.devInsecure, "dev-insecure", false, "allow plaintext development service")
	flags.StringVar(&opts.logLevel, "log-level", envOr(getenv, "ATTERM_RENDEZVOUS_LOG_LEVEL", "INFO"), "DEBUG, INFO, WARN, or ERROR")
	flags.IntVar(&opts.maxConnections, "max-connections", envLimits[0], "global concurrent connection limit")
	flags.IntVar(&opts.maxConnectionsPerIP, "max-connections-per-ip", envLimits[1], "per-IP concurrent connection limit")
	flags.IntVar(&opts.maxConnectionsPerTopic, "max-connections-per-topic", envLimits[2], "per-topic concurrent presence limit")
	flags.IntVar(&opts.maxMailboxPerTopic, "max-mailbox-per-topic", envLimits[3], "per-topic ephemeral mailbox limit")
	flags.IntVar(&opts.maxMailboxGlobal, "max-mailbox-global", envLimits[4], "global ephemeral mailbox limit")
	flags.IntVar(&opts.maxRecentMessages, "max-recent-messages", envLimits[5], "global publish retry-dedupe limit")
	flags.IntVar(&opts.maxMessagesPerMinutePerIP, "max-messages-per-minute-per-ip", envLimits[6], "per-IP publish rate limit")
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("rendezvous: parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("rendezvous: unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	opts.addr = strings.TrimSpace(opts.addr)
	opts.tlsCert = strings.TrimSpace(opts.tlsCert)
	opts.tlsKey = strings.TrimSpace(opts.tlsKey)
	opts.origins = splitNonEmpty(*origins)
	if opts.addr == "" {
		return options{}, errors.New("rendezvous: --addr must not be empty")
	}
	if _, port, err := net.SplitHostPort(opts.addr); err != nil || port == "" {
		return options{}, fmt.Errorf("rendezvous: invalid listen address %q", opts.addr)
	}
	if _, ok := logging.ParseLevel(opts.logLevel); !ok {
		return options{}, fmt.Errorf("rendezvous: invalid log level %q", opts.logLevel)
	}
	if opts.devInsecure && opts.behindTLSProxy {
		return options{}, errors.New("rendezvous: choose only one of --dev-insecure or --behind-tls-proxy")
	}
	if opts.allowNonLoopbackProxy && !opts.behindTLSProxy {
		return options{}, errors.New("rendezvous: --allow-non-loopback-proxy requires --behind-tls-proxy")
	}
	if !opts.devInsecure && len(opts.origins) == 0 {
		return options{}, errors.New("rendezvous: production mode requires --origins")
	}
	if opts.behindTLSProxy {
		if !isLoopbackAddress(opts.addr) && !opts.allowNonLoopbackProxy {
			return options{}, errors.New("rendezvous: --behind-tls-proxy requires a loopback listen address")
		}
		if opts.tlsCert != "" || opts.tlsKey != "" {
			return options{}, errors.New("rendezvous: TLS files cannot be combined with --behind-tls-proxy")
		}
	} else if !opts.devInsecure {
		if opts.tlsCert == "" || opts.tlsKey == "" {
			return options{}, errors.New("rendezvous: production mode requires TLS certificate and key")
		}
		opts.useTLS = true
	}
	if _, err := rendezvous.New(opts.serverConfig()); err != nil {
		return options{}, err
	}
	return opts, nil
}

func serve(ctx context.Context, opts options) error {
	handler, err := rendezvous.New(opts.serverConfig())
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              opts.addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if opts.useTLS {
		httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	}
	errCh := make(chan error, 1)
	go func() {
		logging.Info(rendezvousLogTag, "listening addr=%q tls=%t", opts.addr, opts.useTLS)
		if opts.useTLS {
			errCh <- httpServer.ListenAndServeTLS(opts.tlsCert, opts.tlsKey)
			return
		}
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("rendezvous: serve: %w", err)
	case <-ctx.Done():
		handler.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("rendezvous: shutdown: %w", err)
		}
		return nil
	}
}

func (o options) serverConfig() rendezvous.Config {
	return rendezvous.Config{
		AllowedOrigins:            o.origins,
		MaxConnections:            o.maxConnections,
		MaxConnectionsPerIP:       o.maxConnectionsPerIP,
		MaxConnectionsPerTopic:    o.maxConnectionsPerTopic,
		MaxMailboxPerTopic:        o.maxMailboxPerTopic,
		MaxMailboxGlobal:          o.maxMailboxGlobal,
		MaxRecentMessages:         o.maxRecentMessages,
		MaxMessagesPerMinutePerIP: o.maxMessagesPerMinutePerIP,
	}
}

func splitNonEmpty(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(getenv func(string) string, key string) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("rendezvous: %s must be an integer", key)
	}
	return value, nil
}
