package realchrome

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var errApprovalTimeout = errors.New("realchrome: approval dialog did not appear")

type approvalResult struct {
	clicked bool
	err     error
}

func waitForApproval(ctx context.Context, timeout time.Duration) approvalResult {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := runApprovalScript(ctx)
		if err != nil {
			return approvalResult{err: err}
		}
		if strings.HasPrefix(result, "OK:") {
			return approvalResult{clicked: true}
		}
		select {
		case <-ctx.Done():
			return approvalResult{err: ctx.Err()}
		case <-time.After(250 * time.Millisecond):
		}
	}
	return approvalResult{err: errApprovalTimeout}
}

func runApprovalScript(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", approvalScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %v (%s)", ErrApprovalUnavailable, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Chrome localizes the button label but keeps it as a direct AX button on
// the permission window or sheet. Restricting the search to direct dialog
// controls avoids clicking similarly named buttons in web content.
const approvalScript = `
tell application "System Events"
  if not (exists process "Google Chrome") then return "NO_CHROME"
  tell process "Google Chrome"
    repeat with w in windows
      repeat with labelName in {"Allow", "允许"}
        try
          if exists (button labelName of w) then
            click button labelName of w
            return "OK:window"
          end if
        end try
        try
          if exists (sheet 1 of w) and exists (button labelName of sheet 1 of w) then
            click button labelName of sheet 1 of w
            return "OK:sheet"
          end if
        end try
      end repeat
    end repeat
    return "NO_DIALOG"
  end tell
end tell
`
