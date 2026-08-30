package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/agentcookie/internal/chrome"
	"github.com/mvanhorn/agentcookie/internal/config"
	"github.com/mvanhorn/agentcookie/internal/realchrome"
	"github.com/mvanhorn/agentcookie/internal/sinkpush"
)

var (
	chromeUserDataDir string
	chromeNoRestart   bool
	chromeInjectFrom  string
	chromeInjectSites []string
	chromeAutoApprove bool
)

var chromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Deliver browser identity into the ordinary Google Chrome profile",
	Long: `Prepare, inspect, or inject the user's ordinary Google Chrome Default
profile. This surface writes through Chrome's live DevTools endpoint: it does
not edit Chrome SQLite and does not launch a second browser profile.`,
}

var chromeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Inspect ordinary Chrome remote-debugging readiness",
	RunE: func(cmd *cobra.Command, _ []string) error {
		status, err := realchrome.Inspect(cmd.Context(), chromeUserDataDir)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"user_data_dir":      status.UserDataDir,
			"chrome_running":     status.ChromeRunning,
			"preference_set":     status.PreferenceSet,
			"endpoint_active":    status.EndpointActive,
			"endpoint_reachable": status.EndpointReachable,
			"port":               status.Port,
		}, fmt.Sprintf("agentcookie chrome: running=%v preference=%v endpoint=%v reachable=%v port=%d\n",
			status.ChromeRunning, status.PreferenceSet, status.EndpointActive, status.EndpointReachable, status.Port))
	},
}

var chromeEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable ordinary Chrome's persisted loopback DevTools endpoint",
	Long: `Enable Chrome's built-in remote-debugging preference for the normal
profile. By default Chrome is gracefully quit and immediately relaunched once
so the setting takes effect. Existing tabs and profile data remain Chrome-owned.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		status, err := realchrome.Enable(cmd.Context(), chromeUserDataDir, !chromeNoRestart)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"user_data_dir":      status.UserDataDir,
			"chrome_running":     status.ChromeRunning,
			"preference_set":     status.PreferenceSet,
			"endpoint_active":    status.EndpointActive,
			"endpoint_reachable": status.EndpointReachable,
			"port":               status.Port,
		}, fmt.Sprintf("agentcookie chrome: enabled ordinary Chrome endpoint on loopback port %d\n", status.Port))
	},
}

var chromeInjectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject source or sink cookies into ordinary Chrome",
	Long: `Read cookies from the configured source browser or sink sidecar and
inject them into the running ordinary Google Chrome Default profile. --domain
is repeatable and strongly recommended for one-off work.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cookies, source, err := loadAgentBrowserCookies(agentBrowserInjectOptions{
			Domains:  chromeInjectSites,
			From:     chromeInjectFrom,
			SkipDBSC: false,
		})
		if err != nil {
			return err
		}
		if len(cookies) == 0 {
			return fmt.Errorf("chrome inject: no cookies matched source %s and domains %v", source, chromeInjectSites)
		}
		result, err := realChromeInjector(cmd.Context(), realchrome.Options{
			UserDataDir: chromeUserDataDir,
			AutoApprove: chromeAutoApprove,
			Timeout:     40 * time.Second,
		}, cookies)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"source":           source,
			"cookies":          result.Cookies,
			"port":             result.Port,
			"approval_clicked": result.ApprovalClicked,
		}, fmt.Sprintf("agentcookie chrome: injected %d cookies from %s into ordinary Chrome\n", result.Cookies, source))
	},
}

func init() {
	for _, cmd := range []*cobra.Command{chromeStatusCmd, chromeEnableCmd, chromeInjectCmd} {
		cmd.Flags().StringVar(&chromeUserDataDir, "user-data-dir", "", "ordinary Google Chrome user-data root (default: platform Default profile root)")
	}
	chromeEnableCmd.Flags().BoolVar(&chromeNoRestart, "no-restart", false, "write the preference without restarting Chrome (Chrome must already be closed)")
	chromeInjectCmd.Flags().StringVar(&chromeInjectFrom, "from", agentBrowserCookieSourceAuto, "cookie source: auto, source, or sink")
	chromeInjectCmd.Flags().StringSliceVar(&chromeInjectSites, "domain", nil, "limit to a cookie host suffix or SQLite-LIKE pattern (repeatable)")
	chromeInjectCmd.Flags().BoolVar(&chromeAutoApprove, "auto-approve", true, "use macOS Accessibility to approve Chrome's local debugging dialog")
	chromeCmd.AddCommand(chromeStatusCmd, chromeEnableCmd, chromeInjectCmd)
}

var realChromeInjector = realchrome.Inject

func injectConfiguredRealChrome(ctx context.Context, ref config.RealChromeRef, cookies []chrome.Cookie) (realchrome.Result, error) {
	if !ref.Enabled || len(cookies) == 0 {
		return realchrome.Result{}, nil
	}
	filtered := sinkpush.FilterByHostPatterns(cookies, ref.DomainFilter)
	if len(filtered) == 0 {
		return realchrome.Result{}, nil
	}
	return realChromeInjector(ctx, realchrome.Options{
		UserDataDir: ref.UserDataDir,
		AutoApprove: ref.AutoApprove,
		Timeout:     40 * time.Second,
	}, filtered)
}

// SetRealChromeInjectorForTesting replaces the live browser boundary.
func SetRealChromeInjectorForTesting(f func(context.Context, realchrome.Options, []chrome.Cookie) (realchrome.Result, error)) func() {
	previous := realChromeInjector
	realChromeInjector = f
	return func() { realChromeInjector = previous }
}
