package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/agentcookie/internal/chrome"
	"github.com/mvanhorn/agentcookie/internal/livecdp"
)

var (
	importFile     string
	importEndpoint string
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import cookies from a JSON file and inject them into a running Chrome via CDP",
	Long: `import reads a JSON cookie file (produced by 'agentcookie export' on the source
Mac) and injects the cookies into a running Chrome browser via the Chrome
DevTools Protocol (CDP). This is the Linux sink's primary injection path:
no Keychain, no Chrome SQLite rewrite, just live CDP injection.

The target Chrome must be running with --remote-debugging-port (default 9223).
Typical Grok Bot / agent runtime setups already launch Chrome this way.

Security: the input file must be mode 0600 (owner read/write only). The command
refuses world-readable or group-readable files to prevent accidental exposure
of session cookies via chat paste or misconfigured file drops.

Example workflow:
  # On Mac (source): export cookies
  agentcookie export > cookies.json
  chmod 600 cookies.json
  scp cookies.json linux-agent:~/

  # On Linux (sink): import into running Chrome
  agentcookie import -f cookies.json

  # Or pipe directly (stdin mode):
  ssh mac-source 'agentcookie export' | agentcookie import -f -`,
	RunE: runImport,
}

func init() {
	importCmd.Flags().StringVarP(&importFile, "file", "f", "", "path to JSON cookie file (use '-' for stdin)")
	importCmd.Flags().StringVar(&importEndpoint, "endpoint", livecdp.DefaultCDPEndpoint, "Chrome DevTools Protocol endpoint")
	_ = importCmd.MarkFlagRequired("file")
}

// importCookie is the JSON shape produced by 'agentcookie export' and consumed
// here. Domain, name, value, path, secure, httpOnly, sameSite, expirationDate.
type importCookie struct {
	Domain         string `json:"domain"`
	Name           string `json:"name"`
	Value          string `json:"value"`
	Path           string `json:"path"`
	Secure         bool   `json:"secure"`
	HTTPOnly       bool   `json:"httpOnly"`
	SameSite       string `json:"sameSite"`
	ExpirationDate *int64 `json:"expirationDate,omitempty"`
}

func runImport(cmd *cobra.Command, args []string) error {
	var data []byte
	var err error

	if importFile == "-" {
		data, err = readFromStdin()
		if err != nil {
			return fmt.Errorf("import: read stdin: %w", err)
		}
	} else {
		if err := checkFilePermissions(importFile); err != nil {
			return err
		}
		data, err = os.ReadFile(importFile)
		if err != nil {
			return fmt.Errorf("import: read file: %w", err)
		}
	}

	var importedCookies []importCookie
	if err := json.Unmarshal(data, &importedCookies); err != nil {
		return fmt.Errorf("import: parse JSON: %w", err)
	}

	if len(importedCookies) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "import: no cookies in input file")
		return nil
	}

	cookies := toChromeCookies(importedCookies)

	ctx, cancel := context.WithTimeout(context.Background(), 30*livecdp.DefaultPollInterval)
	defer cancel()

	n, err := livecdp.AttachAndInject(ctx, importEndpoint, cookies)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "import: injected %d cookies into %d browser context(s)\n", len(cookies), n)
	return nil
}

func readFromStdin() ([]byte, error) {
	return os.ReadFile("/dev/stdin")
}

// checkFilePermissions verifies the import file is mode 0600 (owner read/write only).
// Refuses world-readable or group-readable files to prevent accidental credential
// exposure via chat paste or misconfigured file drops.
func checkFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("import: stat %q: %w", path, err)
	}

	mode := info.Mode().Perm()

	// Allow 0600 (owner rw) or 0400 (owner ro). Reject anything with group
	// or world bits set.
	if mode&0o077 != 0 {
		return fmt.Errorf("import: refusing %q with mode %#o (group/world readable); use 'chmod 600 %s' to restrict access before importing. Cookie values are sensitive credentials that must not be shared via chat or world-readable files", path, mode, path)
	}

	return nil
}

// toChromeCookies converts the import JSON shape to chrome.Cookie rows for
// CDP injection.
func toChromeCookies(imported []importCookie) []chrome.Cookie {
	cookies := make([]chrome.Cookie, 0, len(imported))
	for _, ic := range imported {
		c := chrome.Cookie{
			HostKey:    ic.Domain,
			Name:       ic.Name,
			Value:      ic.Value,
			Path:       ic.Path,
			IsSecure:   boolToInt(ic.Secure),
			IsHTTPOnly: boolToInt(ic.HTTPOnly),
			SameSite:   importSameSite(ic.SameSite),
		}
		if ic.ExpirationDate != nil {
			// Convert Unix seconds to Chrome's microseconds-since-1601
			const chromeEpochOffsetSec = 11644473600
			c.ExpiresUTC = (*ic.ExpirationDate + chromeEpochOffsetSec) * 1_000_000
			c.HasExpires = 1
			c.IsPersistent = 1
		}
		cookies = append(cookies, c)
	}
	return cookies
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// importSameSite maps the export JSON's SameSite string to Chrome's numeric
// encoding.
func importSameSite(s string) int {
	switch s {
	case "no_restriction":
		return 0 // None
	case "lax":
		return 1
	case "strict":
		return 2
	default:
		return -1 // unspecified
	}
}
