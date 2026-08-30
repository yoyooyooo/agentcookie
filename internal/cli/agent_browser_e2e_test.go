package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestAgentBrowserSessionInjectE2E(t *testing.T) {
	if os.Getenv("AGENTCOOKIE_AGENT_BROWSER_E2E") != "1" {
		t.Skip("set AGENTCOOKIE_AGENT_BROWSER_E2E=1 to run against installed agent-browser")
	}
	binary, err := exec.LookPath("agent-browser")
	if err != nil {
		t.Fatalf("find agent-browser: %v", err)
	}
	session := fmt.Sprintf("agentcookie-e2e-%d", time.Now().UnixNano())
	defer func() {
		_, _ = execAgentBrowser(context.Background(), binary, "--session", session, "close")
	}()

	result, err := injectNamedAgentBrowserSession(context.Background(), agentBrowserInjectOptions{
		Session: session,
		Binary:  binary,
		Start:   true,
	}, []chrome.Cookie{{
		HostKey:  ".example.com",
		Name:     "agentcookie_e2e",
		Value:    "probe",
		Path:     "/",
		IsSecure: 1,
	}})
	if err != nil {
		t.Fatalf("inject session: %v", err)
	}
	if !result.Started || result.Contexts < 1 {
		t.Fatalf("result = %+v", result)
	}

	if _, err := execAgentBrowser(context.Background(), binary, "--session", session, "open", "https://example.com"); err != nil {
		t.Fatalf("navigate injected session: %v", err)
	}
	out, err := execAgentBrowser(context.Background(), binary, "--session", session, "cookies", "get", "--json")
	if err != nil {
		t.Fatalf("read session cookies: %v", err)
	}
	if !bytes.Contains(out, []byte("agentcookie_e2e")) {
		t.Fatalf("injected cookie not found in agent-browser response")
	}
}
