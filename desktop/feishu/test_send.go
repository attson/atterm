// desktop/feishu/test_send.go
//
// Test-send wiring for the Settings → Feishu integration panel. Lets the user
// fire each notification card on demand to verify the full borrow-token → IM
// send path end-to-end, independent of whether a real hook/heuristic trigger
// has fired. This is the path that silently broke when the relay was missing
// its /v1/feishu/relay-token/me route: a test send surfaces that failure
// directly instead of leaving the user guessing why no card arrived.
package feishu

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

// TestCardScenario identifies which card a test send renders. The string
// values are the stable contract with the frontend buttons.
type TestCardScenario string

const (
	// TestCardCommandSuccess renders the green exit-code-0 command-finished card.
	TestCardCommandSuccess TestCardScenario = "command_success"
	// TestCardCommandFailure renders the red non-zero-exit command-finished card.
	TestCardCommandFailure TestCardScenario = "command_failure"
	// TestCardCommandSealed renders the E2EE "仅本机可见" command-finished card.
	TestCardCommandSealed TestCardScenario = "command_sealed"
	// TestCardWaitingInput renders the orange waiting-for-input card, with a
	// representative question body so the truncation/codeblock path is exercised.
	TestCardWaitingInput TestCardScenario = "waiting_input"
)

// testSessionID is a fixed, obviously-fake UUID used as the deep-link target
// for test cards. Tapping "跳回打开 session" on a test card is expected to be a
// no-op (no such session) — the button is present only to exercise the layout.
var testSessionID = uuid.MustParse("00000000-0000-0000-0000-0000feed0001")

// renderTestCard builds the card body for a scenario. Exported indirectly via
// SendTestCard; kept separate so tests can assert the rendered JSON without a
// live IM client or token source.
func renderTestCard(scenario TestCardScenario) ([]byte, error) {
	var card internalfeishu.Card
	switch scenario {
	case TestCardCommandSuccess:
		card = internalfeishu.RenderCommandFinishedCard(internalfeishu.CommandFinishedInput{
			SessionID: testSessionID,
			ExitCode:  0,
			ElapsedMS: 2_500,
			Label:     "测试命令",
		})
	case TestCardCommandFailure:
		card = internalfeishu.RenderCommandFinishedCard(internalfeishu.CommandFinishedInput{
			SessionID: testSessionID,
			ExitCode:  1,
			ElapsedMS: 73_000,
			Label:     "测试命令",
		})
	case TestCardCommandSealed:
		card = internalfeishu.RenderCommandFinishedCard(internalfeishu.CommandFinishedInput{
			SessionID:  testSessionID,
			SealedBody: []byte("sealed-test"),
		})
	case TestCardWaitingInput:
		card = internalfeishu.RenderWaitingInputCard(internalfeishu.WaitingInputInput{
			SessionID:      testSessionID,
			IdleForSeconds: 42,
			QuestionText:   "这是一条测试通知。\nAgent 想确认是否继续执行某个操作?",
		})
	default:
		return nil, fmt.Errorf("feishu: unknown test card scenario %q", scenario)
	}
	return json.Marshal(card)
}

// SendTestCard renders the card for scenario and sends it to the bound OpenID
// through the live token source + IM client — the exact path a real
// notification takes. Returns a descriptive error if Feishu is not configured,
// the binding is disabled, no OpenID is bound, or the send itself fails, so the
// UI can show the user precisely what went wrong.
func (s *Service) SendTestCard(ctx context.Context, scenario TestCardScenario) error {
	body, err := renderTestCard(scenario)
	if err != nil {
		return err
	}

	tok, openID, _, err := s.tokenSrc.Get(ctx)
	if err != nil {
		return fmt.Errorf("feishu test send: get token: %w", err)
	}
	if openID == "" {
		return fmt.Errorf("feishu test send: no bound open_id")
	}

	if err := s.imClient.SendInteractiveToOpenID(ctx, tok, openID, body); err != nil {
		return fmt.Errorf("feishu test send: %w", err)
	}
	return nil
}
