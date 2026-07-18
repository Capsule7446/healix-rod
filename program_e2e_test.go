package rod_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	coreengine "github.com/Capsule7446/healix-core/application/engine"
	"github.com/Capsule7446/healix-core/domain/fingerprint"
	"github.com/Capsule7446/healix-core/domain/heal"
	"github.com/Capsule7446/healix-core/domain/node"
	"github.com/Capsule7446/healix-rod"
)

// TestE2E_RunProgramHealsAndValidates 验证内存编译后的唯一执行链：
// Program -> Runtime -> Rod -> Healer -> ExecutionSink，不经过任何文件中间产物。
func TestE2E_RunProgramHealsAndValidates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test that launches a real Chromium in -short mode")
	}
	fixture := startProgramFixtureServer(t)
	driver, err := rod.New(rod.Options{Headless: true})
	if err != nil {
		t.Fatalf("rod.New: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	program := loginProgram(fixture.URL + "/login.html?scenario=heal-high")
	facts := &capturingExecutionSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := coreengine.RunProgram(ctx, program, coreengine.Config{RunID: "program-e2e", Driver: driver,
		Healer: heal.NewDefaultHealer(), Facts: facts}); err != nil {
		t.Fatalf("RunProgram: %v", err)
	}
	if len(facts.decisions) != 1 {
		t.Fatalf("heal decisions = %d, want 1", len(facts.decisions))
	}
	decision := facts.decisions[0]
	if decision.Outcome != heal.OutcomeApplied || decision.Best == nil {
		t.Fatalf("heal outcome = %q, want %q", decision.Outcome, heal.OutcomeApplied)
	}
}

type capturingExecutionSink struct {
	decisions []heal.Decision
}

func (*capturingExecutionSink) RecordEvent(context.Context, node.Event) error { return nil }

func (s *capturingExecutionSink) RecordHealDecision(_ context.Context, _, _, _ string,
	_ fingerprint.Selector, decision heal.Decision) error {
	s.decisions = append(s.decisions, decision)
	return nil
}

func (*capturingExecutionSink) RecordValidationObservation(context.Context, string, node.ValidationObservation) error {
	return nil
}

func loginProgram(url string) node.Program {
	username := fingerprint.NodeSpec{ID: "username", Role: "textbox",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#username"}},
		Fingerprint: fingerprint.Fingerprint{Tag: "input", Attributes: map[string]string{"type": "text", "name": "username", "id": "username"},
			Path: []string{"html", "body", "form#loginForm", "input"}, Neighbors: fingerprint.Neighbors{Next: "div", ParentTag: "form"}}}
	submit := fingerprint.NodeSpec{ID: "submit", Role: "button",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorTestID, Value: "login-submit"}, {Type: fingerprint.SelectorCSS, Value: "#submit", Priority: 1}},
		Fingerprint: fingerprint.Fingerprint{Tag: "button", Attributes: map[string]string{"type": "submit", "class": "primary", "data-testid": "login-submit"},
			Text: "登录", ARIA: fingerprint.ARIA{Role: "button", Name: "登录"},
			Path: []string{"html", "body", "form#loginForm", "div#button-container", "button"}, Neighbors: fingerprint.Neighbors{ParentTag: "div"}}}
	result := fingerprint.NodeSpec{ID: "result",
		Selectors: []fingerprint.Selector{{Type: fingerprint.SelectorCSS, Value: "#result"}}}
	root := &node.WorkflowNode{NodeID: "login", Children: []node.Node{
		&node.StepNode{NodeID: "open", Action: node.Action{Kind: node.ActionNavigate, Value: url}},
		&node.StepNode{NodeID: "username", Target: username, Action: node.Action{Kind: node.ActionInput, Value: "alice"}},
		&node.StepNode{NodeID: "submit", Target: submit, Action: node.Action{Kind: node.ActionClick}},
		&node.ValidationNode{NodeID: "result", Target: result, Assertion: node.ValidationAssertion{Kind: "text_equals", Expected: "欢迎, alice"},
			MaxWait: 3 * time.Second, Stability: 100 * time.Millisecond},
	}}
	return node.Program{Root: root,
		Specs: map[string]fingerprint.NodeSpec{username.ID: username, submit.ID: submit, result.ID: result}}
}

func startProgramFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixtures, err := filepath.Abs(filepath.Join("testdata", "fixtures"))
	if err != nil {
		t.Fatalf("resolve fixtures: %v", err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(fixtures)))
	t.Cleanup(server.Close)
	return server
}
