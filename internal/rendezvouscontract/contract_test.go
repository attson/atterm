package rendezvouscontract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/rendezvous"
)

func TestStrictContractDecodingRejectsTrailingJSON(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","protocol_version":1} {}`))
	}))
	defer health.Close()
	if _, err := checkHealth(context.Background(), health.URL); err == nil {
		t.Fatal("health accepted a trailing JSON value")
	}
	if _, err := decodeEvent([]byte(`{"v":1,"kind":"registered"} {}`)); err == nil {
		t.Fatal("event accepted a trailing JSON value")
	}
}

func TestRunAgainstStandaloneService(t *testing.T) {
	handler, err := rendezvous.New(rendezvous.Config{AllowedOrigins: []string{"https://app.example"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	defer handler.Close()

	report, err := Run(context.Background(), Config{
		URL: server.URL, Origin: "https://app.example", AllowInsecureLoopback: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ProtocolVersion != rendezvous.Version || !report.LiveDelivery ||
		!report.MailboxDelivery || !report.RetryDeduplicated || !report.OversizeRejected || !report.MetricsPrivate {
		t.Fatalf("report=%+v", report)
	}
}

func TestExternalServiceContract(t *testing.T) {
	serverURL := strings.TrimSpace(os.Getenv("ATTERM_RENDEZVOUS_TEST_URL"))
	if serverURL == "" {
		t.Skip("ATTERM_RENDEZVOUS_TEST_URL is not set")
	}
	if _, err := Run(context.Background(), Config{
		URL:    serverURL,
		Origin: strings.TrimSpace(os.Getenv("ATTERM_RENDEZVOUS_TEST_ORIGIN")),
		AllowInsecureLoopback: strings.HasPrefix(serverURL, "http://localhost") ||
			strings.HasPrefix(serverURL, "http://127.0.0.1") || strings.HasPrefix(serverURL, "http://[::1]"),
	}); err != nil {
		t.Fatal(err)
	}
}
