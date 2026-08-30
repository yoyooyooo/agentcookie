package livecdp

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

const (
	browserWebSocketReadyTimeout = 5 * time.Second
	browserWebSocketRetryEvery   = 100 * time.Millisecond
)

// InjectBrowserWebSocket connects directly to a browser-level DevTools
// WebSocket and sets cookies without creating, attaching to, or closing any
// page target. This is the safe path for a browser owned by another process
// such as agent-browser, whose daemon retains page-session IDs across calls.
// A newly launched browser may publish its WebSocket URL just before it accepts
// a second client, so the complete browser-level operation is retried within a
// bounded readiness window.
func InjectBrowserWebSocket(ctx context.Context, wsURL string, cookies []chrome.Cookie) (int, error) {
	if len(BuildCookieParams(cookies)) == 0 {
		return 0, nil
	}
	return injectBrowserWebSocketWithRetry(
		ctx,
		wsURL,
		cookies,
		browserWebSocketReadyTimeout,
		browserWebSocketRetryEvery,
		injectBrowserWebSocketOnce,
	)
}

type browserWebSocketInjectFunc func(context.Context, string, []chrome.Cookie) (int, error)

func injectBrowserWebSocketWithRetry(
	ctx context.Context,
	wsURL string,
	cookies []chrome.Cookie,
	timeout time.Duration,
	retryEvery time.Duration,
	inject browserWebSocketInjectFunc,
) (int, error) {
	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		injected, err := inject(retryCtx, wsURL, cookies)
		if err == nil {
			return injected, nil
		}
		lastErr = err

		timer := time.NewTimer(retryEvery)
		select {
		case <-retryCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return 0, fmt.Errorf("livecdp: browser websocket not ready within %s: %w", timeout, lastErr)
		case <-timer.C:
		}
	}
}

func injectBrowserWebSocketOnce(ctx context.Context, wsURL string, cookies []chrome.Cookie) (int, error) {
	params := BuildCookieParams(cookies)
	if len(params) == 0 {
		return 0, nil
	}

	browserCtx, cancel := context.WithCancel(ctx)
	browser, err := chromedp.NewBrowser(browserCtx, wsURL, chromedp.WithDialTimeout(time.Second))
	if err != nil {
		cancel()
		return 0, fmt.Errorf("livecdp: connect browser websocket: %w", err)
	}
	defer cancel()

	var contextResult target.GetBrowserContextsReturns
	if err := browser.Execute(browserCtx, target.CommandGetBrowserContexts, nil, &contextResult); err != nil {
		return 0, fmt.Errorf("livecdp: list browser contexts: %w", err)
	}
	var targetResult target.GetTargetsReturns
	if err := browser.Execute(browserCtx, target.CommandGetTargets, target.GetTargets(), &targetResult); err != nil {
		return 0, fmt.Errorf("livecdp: list targets: %w", err)
	}

	explicit := make(map[cdp.BrowserContextID]bool, len(contextResult.BrowserContextIDs))
	for _, id := range contextResult.BrowserContextIDs {
		explicit[id] = true
	}
	seen := make(map[cdp.BrowserContextID]bool)
	var contextIDs []cdp.BrowserContextID
	for _, info := range targetResult.TargetInfos {
		if !shouldInjectTarget(info) || seen[info.BrowserContextID] {
			continue
		}
		seen[info.BrowserContextID] = true
		contextIDs = append(contextIDs, info.BrowserContextID)
	}

	injected := 0
	var firstErr error
	for _, id := range contextIDs {
		set := storage.SetCookies(params)
		if explicit[id] {
			set = set.WithBrowserContextID(id)
		}
		if err := browser.Execute(browserCtx, storage.CommandSetCookies, set, nil); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("Storage.setCookies (%d cookies, ctx=%q): %w", len(params), id, err)
			}
			continue
		}
		injected++
	}
	return injected, firstErr
}
