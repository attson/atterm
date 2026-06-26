// desktop/feishu/test_send_test.go
package feishu

import (
	"context"
	"strings"
	"testing"
)

func TestRenderTestCard_AllScenarios(t *testing.T) {
	cases := []struct {
		scenario TestCardScenario
		// substrings that must appear in the rendered JSON
		wants []string
	}{
		{TestCardCommandSuccess, []string{`"命令完成"`, `"green"`, "退出码", "测试命令"}},
		{TestCardCommandFailure, []string{`"命令完成"`, `"red"`, "退出码", "重试", "连续第 2 次", "FAIL"}},
		{TestCardCommandSealed, []string{"仅本机可见", `"grey"`}},
		{TestCardWaitingInput, []string{"等待输入", `"orange"`, "测试通知"}},
		{TestCardAskQuestion, []string{"Agent 提问", `"blue"`, "继续执行", "停止"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.scenario), func(t *testing.T) {
			body, err := renderTestCard(tc.scenario)
			if err != nil {
				t.Fatalf("renderTestCard(%s): %v", tc.scenario, err)
			}
			s := string(body)
			for _, want := range tc.wants {
				if !strings.Contains(s, want) {
					t.Errorf("scenario %s: rendered card missing %q\nbody: %s", tc.scenario, want, s)
				}
			}
			// Every card must carry the action row deep-link to the fixed test session.
			if !strings.Contains(s, testSessionID.String()) {
				t.Errorf("scenario %s: missing test session deep-link", tc.scenario)
			}
		})
	}
}

func TestRenderTestCard_Unknown(t *testing.T) {
	if _, err := renderTestCard(TestCardScenario("nope")); err == nil {
		t.Fatal("expected error for unknown scenario")
	}
}

func TestSendTestCard_HappyPath(t *testing.T) {
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "ou_test", hash: "h"}
	svc := &Service{tokenSrc: ts, imClient: im}

	if err := svc.SendTestCard(context.Background(), TestCardWaitingInput); err != nil {
		t.Fatalf("SendTestCard: %v", err)
	}
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
	if im.openIDs[0] != "ou_test" || im.tokens[0] != "tt" {
		t.Fatalf("sent with wrong creds: openID=%s tok=%s", im.openIDs[0], im.tokens[0])
	}
	if !strings.Contains(im.bodies[0], "等待输入") {
		t.Fatalf("wrong card body: %s", im.bodies[0])
	}
}

func TestSendTestCard_NoOpenID(t *testing.T) {
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "", hash: "h"} // bound creds but no open_id
	svc := &Service{tokenSrc: ts, imClient: im}

	err := svc.SendTestCard(context.Background(), TestCardCommandSuccess)
	if err == nil || !strings.Contains(err.Error(), "no bound open_id") {
		t.Fatalf("expected no-open_id error, got %v", err)
	}
	if len(im.bodies) != 0 {
		t.Fatalf("must not send when open_id missing")
	}
}

func TestSendTestCard_TokenError(t *testing.T) {
	im := &capturingIM{}
	ts := &stubTokenSource{err: ErrTokenNotConfigured}
	svc := &Service{tokenSrc: ts, imClient: im}

	err := svc.SendTestCard(context.Background(), TestCardCommandSuccess)
	if err == nil || !strings.Contains(err.Error(), "get token") {
		t.Fatalf("expected get-token error, got %v", err)
	}
}

func TestSendTestCard_UnknownScenario(t *testing.T) {
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "ou", hash: "h"}
	svc := &Service{tokenSrc: ts, imClient: im}

	if err := svc.SendTestCard(context.Background(), TestCardScenario("bogus")); err == nil {
		t.Fatal("expected error for unknown scenario")
	}
	if len(im.bodies) != 0 {
		t.Fatal("must not send for unknown scenario")
	}
}
