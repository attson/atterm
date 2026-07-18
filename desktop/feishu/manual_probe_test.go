//go:build manual_probe

// Run with: go test -tags manual_probe -v -run TestManualProbe_SendAndPatch ./desktop/feishu/
//
// This test hits the REAL Feishu Open Platform using the dev-build's local
// keychain blob (file fallback at desktop/.atterm-dev/keyring-fallback.json).
// It is excluded from the default build by the manual_probe tag so CI never
// runs it. Intent: validate SendAnchorCard → PatchCard round-trip against the
// actual API without re-running wails dev.
package feishu

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/appdir"
	internalfeishu "github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/safekeyring"
)

func TestManualProbe_SendAndPatch(t *testing.T) {
	// Match the running wails dev build's namespace ("atterm-dev" + .dev
	// keychain suffix) so we read the binding it wrote, not a fresh prod one.
	appdir.UseDev()
	// Point at the dev keyring file written by the running wails dev build.
	safekeyring.SetFileDirForTest("/Users/attson/code/github.com.attson/atterm/desktop/.atterm-dev")
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})

	ctx := context.Background()
	store := NewLocalKeychainBindingStore()
	view, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("read binding: %v", err)
	}
	if view.AppID == "" || view.AppSecret == "" {
		t.Fatalf("incomplete binding: AppID=%q has-secret=%v", view.AppID, view.AppSecret != "")
	}
	if view.OpenID == "" {
		t.Fatalf("not bound to an open_id yet — finish the bind flow first")
	}
	t.Logf("binding: appID=%s openID=%s", view.AppID, view.OpenID)

	httpC := &http.Client{Timeout: 10 * time.Second}
	tokenSrc := NewLocalTenantTokenSource(store, "https://open.feishu.cn", httpC, time.Now)
	tok, _, _, err := tokenSrc.Get(ctx)
	if err != nil {
		t.Fatalf("tenant token: %v", err)
	}
	t.Logf("tenant token: %s…%s (len=%d)", tok[:8], tok[len(tok)-4:], len(tok))

	client := internalfeishu.NewClient("https://open.feishu.cn", httpC)

	// Build a minimal anchor card.
	cardJSON, err := internalfeishu.RenderAnchorCreate(internalfeishu.AnchorState{
		SessionID:    "manual-probe-sid",
		SessionLabel: "probe",
		StatusText:   "manual probe",
		BodyMarkdown: "",
		Template:     "blue",
	})
	if err != nil {
		t.Fatalf("render card: %v", err)
	}

	t.Log("=== step 1: SendAnchorCard (creates cardkit entity + sends IM) ===")
	msgID, cardID, err := client.SendAnchorCard(ctx, tok, view.OpenID, cardJSON)
	if err != nil {
		t.Fatalf("SendAnchorCard FAILED: %v", err)
	}
	t.Logf("OK: msgID=%s cardID=%s", msgID, cardID)
	if !strings.HasPrefix(cardID, "om_") {
		t.Logf("(cardID does NOT look like an IM message_id — good, that's the real cardkit id)")
	} else {
		t.Errorf("cardID looks like an IM message_id (%s); SendAnchorCard regressed", cardID)
	}

	t.Log("=== step 2: PatchCard with sequence=1 ===")
	body1 := "👤 manual probe — first patch\n\n"
	err = client.PatchCard(ctx, tok, cardID, internalfeishu.AnchorBodyElementID, body1, 1)
	if err != nil {
		t.Fatalf("PatchCard #1 FAILED: %v", err)
	}
	t.Logf("OK: patch #1 applied")

	time.Sleep(500 * time.Millisecond)

	t.Log("=== step 3: PatchCard with sequence=2 ===")
	body2 := "👤 manual probe — first patch\n\n🤖 second patch reached the card\n"
	err = client.PatchCard(ctx, tok, cardID, internalfeishu.AnchorBodyElementID, body2, 2)
	if err != nil {
		t.Fatalf("PatchCard #2 FAILED: %v", err)
	}
	t.Logf("OK: patch #2 applied")

	time.Sleep(500 * time.Millisecond)

	t.Log("=== step 4b: DELETE + POST re-create input (visual reset) ===")
	err = client.DeleteCardElement(ctx, tok, cardID, internalfeishu.AnchorInputElementID, 3)
	if err != nil {
		t.Fatalf("DeleteCardElement FAILED: %v", err)
	}
	fresh := internalfeishu.NewInputElement("manual-probe-sid", "anchor_input_reset")
	err = client.CreateCardElement(ctx, tok, cardID,
		internalfeishu.AnchorBodyElementID, "insert_after",
		[]map[string]any{fresh}, 4)
	if err != nil {
		t.Fatalf("CreateCardElement FAILED: %v", err)
	}
	t.Logf("OK: input DELETE+POST re-create applied (check Feishu: input should be visually empty)")

	time.Sleep(500 * time.Millisecond)

	t.Log("=== step 5: PATCH column_set with new columns (option buttons) ===")
	askOpts := internalfeishu.AskOptionsColumnSet("manual-probe-sid", []string{"写代码", "代码审查", "研究"})
	delete(askOpts, "tag")     // partial_element shouldn't restate tag (immutable)
	err = client.PatchCardElement(ctx, tok, cardID, internalfeishu.AnchorButtonsElementID,
		askOpts, 5)
	if err != nil {
		t.Fatalf("PatchCardElement columns swap FAILED: %v", err)
	}
	t.Logf("OK: column_set columns swapped to option buttons — check Feishu: bottom row should show 3 primary buttons")

	// (Restore step intentionally skipped for visual verification — leaves
	// the final card showing the option buttons so the user can confirm
	// step 5 took effect by just looking at the latest probe card in
	// Feishu. Re-enable in a later probe iteration to also verify restore.)
	_ = internalfeishu.DefaultButtonsColumnSet // silence unused

	time.Sleep(500 * time.Millisecond)

	// step 5b — remove the standalone Type-here input before mounting the
	// AskUserQuestion form. Real flow does this so the per-question inputs
	// aren't competing for attention with the anchor's own input.
	t.Log("=== step 5b: DELETE standalone input (Type-here) before form mount ===")
	if err := client.DeleteCardElement(ctx, tok, cardID, "anchor_input_reset", 6); err != nil {
		t.Fatalf("DeleteCardElement (Type-here) FAILED: %v", err)
	}
	t.Logf("OK: standalone input removed — check Feishu: 'Type here…' gone")

	time.Sleep(500 * time.Millisecond)

	// step 5c — also remove the buttons row before the form goes up. Real
	// flow does this to keep the form as the only interactive surface.
	t.Log("=== step 5c: DELETE anchor buttons row before form mount ===")
	if err := client.DeleteCardElement(ctx, tok, cardID, internalfeishu.AnchorButtonsElementID, 7); err != nil {
		t.Fatalf("DeleteCardElement (buttons) FAILED: %v", err)
	}
	t.Logf("OK: buttons row removed — check Feishu: bottom row gone")

	time.Sleep(500 * time.Millisecond)

	t.Log("=== step 6: CREATE form container (per-question row: select + input) ===")
	mkRow := func(idx int, question string, opts []string) map[string]any {
		optElems := make([]any, 0, len(opts))
		for _, o := range opts {
			optElems = append(optElems, map[string]any{
				"text":  map[string]any{"tag": "plain_text", "content": o},
				"value": o,
			})
		}
		sel := map[string]any{
			"tag":         "select_static",
			"element_id":  fmt.Sprintf("probe_q%d_sel", idx),
			"name":        fmt.Sprintf("q_%d_sel", idx),
			"required":    false,
			"placeholder": map[string]any{"tag": "plain_text", "content": "❓ " + question},
			"options":     optElems,
		}
		txt := map[string]any{
			"tag":         "input",
			"element_id":  fmt.Sprintf("probe_q%d_txt", idx),
			"name":        fmt.Sprintf("q_%d_txt", idx),
			"required":    false,
			"placeholder": map[string]any{"tag": "plain_text", "content": "自定义答案"},
		}
		return map[string]any{
			"tag":                "column_set",
			"horizontal_spacing": "small",
			"columns": []any{
				map[string]any{"tag": "column", "width": "weighted", "weight": 1, "elements": []any{sel}},
				map[string]any{"tag": "column", "width": "weighted", "weight": 1, "elements": []any{txt}},
			},
		}
	}
	form := map[string]any{
		"tag":              "form",
		"element_id":       "probe_askform",
		"name":             "probe_askform",
		"vertical_spacing": "8px",
		"padding":          "8px 0px 8px 0px",
		"elements": []any{
			mkRow(0, "用什么语言?", []string{"Python", "Go", "Rust"}),
			mkRow(1, "包含哪些功能?", []string{"数据库", "用户认证", "CI/CD"}),
			// Submit + Cancel row
			map[string]any{
				"tag":                "column_set",
				"horizontal_spacing": "small",
				"columns": []any{
					map[string]any{
						"tag": "column", "width": "auto",
						"elements": []any{
							map[string]any{
								"tag":              "button",
								"element_id":       "probe_submit",
								"type":             "primary",
								"text":             map[string]any{"tag": "plain_text", "content": "提交"},
								"form_action_type": "submit",
								"name":             "submit_btn",
								"behaviors": []any{
									map[string]any{"type": "callback", "value": map[string]any{
										"kind":       "form",
										"session_id": "manual-probe-sid",
									}},
								},
							},
						},
					},
					map[string]any{
						"tag": "column", "width": "auto",
						"elements": []any{
							map[string]any{
								"tag":        "button",
								"element_id": "probe_reset",
								"type":       "default",
								"text":       map[string]any{"tag": "plain_text", "content": "重置"},
								"form_action_type": "reset",
								"name":       "reset_btn",
							},
						},
					},
				},
			},
		},
	}
	err = client.CreateCardElement(ctx, tok, cardID,
		internalfeishu.AnchorBodyElementID, "insert_after",
		[]map[string]any{form}, 8)
	if err != nil {
		t.Fatalf("CreateCardElement form FAILED: %v", err)
	}
	t.Logf("OK: form container inserted — check Feishu: form only, no input, no buttons row")

	fmt.Printf("\n\n=== VERDICT ===\n")
	fmt.Printf("CardKit card_id: %s\n", cardID)
	fmt.Printf("IM message_id:   %s\n", msgID)
	fmt.Printf("Look at the Feishu DM for the bound user — the card body should\n")
	fmt.Printf("show '🤖 second patch reached the card' if streaming PATCH works.\n")
	fmt.Printf("The input textbox should be empty (we just sent a clear PATCH).\n")
}
