// internal/feishu/client.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// PatchCard updates a CardKit card's markdown element identified by elementID.
// It calls the V2 element-level streaming-update OpenAPI:
// PATCH /open-apis/cardkit/v1/cards/{token}/elements/{element_id}.
// sequence is a strictly increasing per-card number; Feishu uses it to drop
// out-of-order updates. bodyMarkdown is the FULL new content of the element —
// the platform computes the typewriter diff.
//
// V2 schema requires the targeted element to carry an explicit `element_id`
// at card-create time, and the PATCH must address it by id (the legacy
// `body.elements[0].content` JSON-path returns code=0 but silently no-ops).
//
// Errors:
//   - code != 0 surfaces as fmt error with the code embedded.
//   - auth-class codes (token expired etc) returned as *AuthClassError so the
//     caller can refresh the tenant token and retry.
func (c *Client) PatchCard(ctx context.Context, tenantToken, cardToken, elementID, bodyMarkdown string, sequence int64) error {
	// `partial_element` carries the element's new partial config as a JSON
	// STRING (not a nested object) — that's the wire shape the Patch element
	// endpoint expects per the Feishu Go SDK (cardkit.v1 PatchCardElement).
	// Anything else fails server-side with code=99992402 "field validation
	// failed".
	partialElement, _ := json.Marshal(map[string]any{"content": bodyMarkdown})
	payload := map[string]any{
		"uuid":            fmt.Sprintf("%s-%d", cardToken, sequence),
		"sequence":        sequence,
		"partial_element": string(partialElement),
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/open-apis/cardkit/v1/cards/%s/elements/%s", c.baseURL, cardToken, elementID)
	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return fmt.Errorf("card PATCH: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		// Verbose detail (url + request payload + raw response body) so a
		// "field validation failed" surfaces with enough info to diagnose
		// without re-running with instrumentation.
		return fmt.Errorf("cardkit patch: code=%d msg=%s url=%s req=%s resp=%s",
			r.Code, r.Msg, url, string(body), string(respBody))
	}
	return nil
}

// SendAnchorCard creates a CardKit card entity, sends an IM message that
// references it, and returns (msg_id, card_id). msg_id is used by the
// inbound reply path; card_id is what PatchCard targets for live updates.
//
// Two-step send is mandatory for streaming PATCH to work:
//  1. POST /open-apis/cardkit/v1/cards — creates a CardKit entity, returns card_id.
//  2. POST /open-apis/im/v1/messages with content {"type":"card","data":{"card_id":...}}
//     — delivers the card to openID.
//
// The earlier "send the card inline via im.v1 and reuse msg_id as card_token"
// shortcut returned an IM message_id, which the cardkit element PATCH endpoint
// can't resolve, surfacing as code=99992402 "field validation failed" on
// every flush. Only a real card_id from the entity-create call works.
//
// cardBody must be the wrapper {msg_type:"interactive", card:{...}}; the card
// sub-object is what becomes the CardKit entity's data.
func (c *Client) SendAnchorCard(ctx context.Context, tenantToken, openID string, cardBody []byte) (msgID, cardID string, err error) {
	var c0 struct {
		Card json.RawMessage `json:"card"`
	}
	if err := json.Unmarshal(cardBody, &c0); err != nil {
		return "", "", fmt.Errorf("parse anchor card body: %w", err)
	}

	cardID, err = c.createCardEntity(ctx, tenantToken, string(c0.Card))
	if err != nil {
		return "", "", err
	}

	imContent, _ := json.Marshal(map[string]any{
		"type": "card",
		"data": map[string]any{"card_id": cardID},
	})
	wrapper := map[string]any{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(imContent),
	}
	msgID, err = c.postIM(ctx, tenantToken, wrapper)
	if err != nil {
		return "", "", err
	}
	return msgID, cardID, nil
}

// createCardEntity registers cardJSON as a CardKit entity and returns its
// card_id. The body shape is {"type":"card_json","data":"<stringified card>"}.
func (c *Client) createCardEntity(ctx context.Context, tenantToken, cardJSON string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"type": "card_json",
		"data": cardJSON,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/open-apis/cardkit/v1/cards",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return "", fmt.Errorf("cardkit create: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			CardID string `json:"card_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(respBody, &r)
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return "", &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return "", fmt.Errorf("cardkit create: code=%d msg=%s resp=%s", r.Code, r.Msg, string(respBody))
	}
	if r.Data.CardID == "" {
		return "", fmt.Errorf("cardkit create: empty card_id (resp=%s)", string(respBody))
	}
	return r.Data.CardID, nil
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
