// internal/feishu/client.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client wraps Feishu's IM API. One client per relay process, used by
// feishu.Service to actually POST cards.
type Client struct {
	baseURL string
	httpC   *http.Client
}

func NewClient(baseURL string, httpC *http.Client) *Client {
	if httpC == nil {
		httpC = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpC: httpC}
}

// SendInteractiveToOpenID posts an interactive card to a single open_id.
// cardBody must be a JSON object with at least {"msg_type":"interactive","card":...}.
// The function wraps it with receive_id + msg_type + content per
// Feishu's im/v1/messages contract.
// It returns the Feishu message_id of the sent card, which callers use to
// route a user's reply back to the originating session.
func (c *Client) SendInteractiveToOpenID(ctx context.Context, tenantToken, openID string, cardBody []byte) (string, error) {
	// The Feishu API expects:
	//   { receive_id, msg_type:"interactive", content: <stringified card JSON> }
	// We unmarshal cardBody to extract the card sub-object so it can be
	// re-marshaled into the `content` field as a string.
	var c0 struct {
		Card json.RawMessage `json:"card"`
	}
	if err := json.Unmarshal(cardBody, &c0); err != nil {
		return "", fmt.Errorf("parse card body: %w", err)
	}
	wrapper := map[string]any{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(c0.Card),
	}
	return c.postIM(ctx, tenantToken, wrapper)
}

func (c *Client) SendTextToOpenID(ctx context.Context, tenantToken, openID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	wrapper := map[string]any{
		"receive_id": openID,
		"msg_type":   "text",
		"content":    string(content),
	}
	_, err := c.postIM(ctx, tenantToken, wrapper)
	return err
}

// postIM posts a message wrapper and returns the resulting message_id.
func (c *Client) postIM(ctx context.Context, tenantToken string, wrapper map[string]any) (string, error) {
	body, _ := json.Marshal(wrapper)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/open-apis/im/v1/messages?receive_id_type=open_id",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return "", fmt.Errorf("im POST: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return "", &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return "", fmt.Errorf("feishu im send: code=%d msg=%s", r.Code, r.Msg)
	}
	return r.Data.MessageID, nil
}
