package webhook

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

func newClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// post sends body as application/json to url. Returns an error on transport
// failure or non-2xx status. The response body is intentionally discarded
// (never surfaced to the user — avoids a blind-SSRF data oracle).
func post(c *http.Client, url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
