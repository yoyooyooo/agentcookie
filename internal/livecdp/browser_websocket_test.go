package livecdp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestInjectBrowserWebSocketWithRetryBecomesReady(t *testing.T) {
	attempts := 0
	cookies := []chrome.Cookie{{HostKey: ".example.com", Name: "session", Value: "value"}}
	got, err := injectBrowserWebSocketWithRetry(
		context.Background(),
		"ws://127.0.0.1:9222/devtools/browser/id",
		cookies,
		time.Second,
		time.Millisecond,
		func(context.Context, string, []chrome.Cookie) (int, error) {
			attempts++
			if attempts < 3 {
				return 0, errors.New("connection reset by peer")
			}
			return 2, nil
		},
	)
	if err != nil {
		t.Fatalf("retry injection: %v", err)
	}
	if got != 2 || attempts != 3 {
		t.Fatalf("contexts=%d attempts=%d, want 2/3", got, attempts)
	}
}

func TestInjectBrowserWebSocketWithRetryReportsLastError(t *testing.T) {
	last := errors.New("connection reset by peer")
	_, err := injectBrowserWebSocketWithRetry(
		context.Background(),
		"ws://127.0.0.1:9222/devtools/browser/id",
		[]chrome.Cookie{{HostKey: ".example.com", Name: "session", Value: "value"}},
		15*time.Millisecond,
		time.Millisecond,
		func(context.Context, string, []chrome.Cookie) (int, error) {
			return 0, last
		},
	)
	if err == nil || !errors.Is(err, last) || !strings.Contains(err.Error(), "not ready within") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestInjectBrowserWebSocketEmptyCookiesDoesNotDial(t *testing.T) {
	got, err := InjectBrowserWebSocket(context.Background(), "not-a-websocket", nil)
	if err != nil || got != 0 {
		t.Fatalf("empty injection = %d, %v", got, err)
	}
}
