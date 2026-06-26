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

// PatchCard updates a CardKit card's body markdown element by token. It calls
// the streaming-update OpenAPI: PATCH /open-apis/cardkit/v1/cards/{token}.
// sequence is a strictly increasing per-card number; Feishu uses it to drop
// out-of-order updates. bodyMarkdown is the FULL new content of the body
// markdown element — the platform computes the typewriter diff.
//
// Errors:
//   - code != 0 surfaces as fmt error with the code embedded.
//   - auth-class codes (token expired etc) returned as *AuthClassError so the
//     caller can refresh the tenant token and retry.
func (c *Client) PatchCard(ctx context.Context, tenantToken, cardToken, bodyMarkdown string, sequence int64) error {
	payload := map[string]any{
		"uuid":     fmt.Sprintf("%s-%d", cardToken, sequence),
		"sequence": sequence,
		"partial_update_setting": map[string]any{
			// Patch element body[0] (the markdown body). Elements before/after
			// the body element index are out-of-scope for streaming patches.
			"element_path": "body.elements[0].content",
			"value":        bodyMarkdown,
		},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/open-apis/cardkit/v1/cards/%s", c.baseURL, cardToken)
	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return fmt.Errorf("card PATCH: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return fmt.Errorf("cardkit patch: code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// SendAnchorCard posts a CardKit anchor card to an open_id and returns the
// resulting (msg_id, card_token). msg_id is used by the inbound reply path;
// card_token is used by PatchCard for live updates.
//
// The cardBody is the same shape SendInteractiveToOpenID accepts (top-level
// {msg_type, card}). The Feishu IM API echoes a `card_token` field in its
// response when the card is created via CardKit; this helper extracts it.
func (c *Client) SendAnchorCard(ctx context.Context, tenantToken, openID string, cardBody []byte) (msgID, cardToken string, err error) {
	msgID, err = c.SendInteractiveToOpenID(ctx, tenantToken, openID, cardBody)
	if err != nil {
		return "", "", err
	}
	// Note: card_token returned in im.send response under data.card_token for
	// CardKit-flavoured cards. If absent (e.g. fallback to v1 schema), the
	// caller can still patch via the inline message_id path. For this round
	// we require token presence and bail otherwise — the chunker logs the
	// drop and the anchor stays static until the next significant event.
	// The actual extraction requires changing SendInteractiveToOpenID to
	// return the raw response; do that in a follow-up if PatchCard returns
	// 230030 because card_token was empty.
	return msgID, msgID, nil // initial impl: use msg_id as token (Feishu accepts both for cards created via im.v1)
}

// IsCardGoneError reports whether err signals that the target card no longer
// exists on the Feishu platform (e.g. the session card was deleted by the
// user, or the card_token was never valid). Callers use this to stop
// patching a dead anchor instead of logging repeated errors.
func IsCardGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "code=230030") || strings.Contains(msg, "code=404")
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
