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
	chromeInjectMode  string
	chromeInjectSites []string
	chromeProfile     string
	chromeAutoApprove bool
)

var chromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Deliver browser identity into the ordinary Google Chrome profile",
	Long: `Prepare, inspect, or inject the user's ordinary Google Chrome profile.
Live mode writes through Chrome's DevTools endpoint. Offline mode gracefully
stops a running Chrome, writes Chrome's host-bound Cookie rows, and restores
the prior running state. Neither mode launches a second browser profile.`,
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

var chromeDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable ordinary Chrome's persisted DevTools endpoint",
	RunE: func(cmd *cobra.Command, _ []string) error {
		status, err := realchrome.Disable(cmd.Context(), chromeUserDataDir, !chromeNoRestart)
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
		}, "agentcookie chrome: disabled ordinary Chrome remote debugging\n")
	},
}

var chromeInjectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject source or sink cookies into ordinary Chrome",
	Long: `Read cookies from the configured source browser or sink sidecar and
inject them into the ordinary Google Chrome profile. Live mode requires a
user-approved DevTools endpoint. Offline mode performs an unattended,
host-bound write while Chrome is stopped and restores its prior running state.
--domain is repeatable and strongly recommended for one-off work.`,
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
			Mode:        chromeInjectMode,
			UserDataDir: chromeUserDataDir,
			Profile:     chromeProfile,
			AutoApprove: chromeAutoApprove,
			Timeout:     40 * time.Second,
		}, cookies)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"source":           source,
			"mode":             result.Mode,
			"cookies":          result.Cookies,
			"port":             result.Port,
			"approval_clicked": result.ApprovalClicked,
			"restarted":        result.Restarted,
		}, fmt.Sprintf("agentcookie chrome: injected %d cookies from %s into ordinary Chrome (mode=%s, restarted=%v)\n", result.Cookies, source, result.Mode, result.Restarted))
	},
}

func init() {
	for _, cmd := range []*cobra.Command{chromeStatusCmd, chromeEnableCmd, chromeDisableCmd, chromeInjectCmd} {
		cmd.Flags().StringVar(&chromeUserDataDir, "user-data-dir", "", "ordinary Google Chrome user-data root (default: platform Default profile root)")
	}
	chromeEnableCmd.Flags().BoolVar(&chromeNoRestart, "no-restart", false, "write the preference without restarting Chrome (Chrome must already be closed)")
	chromeDisableCmd.Flags().BoolVar(&chromeNoRestart, "no-restart", false, "clear the preference without restarting Chrome (Chrome must already be closed)")
	chromeInjectCmd.Flags().StringVar(&chromeInjectFrom, "from", agentBrowserCookieSourceAuto, "cookie source: auto, source, or sink")
	chromeInjectCmd.Flags().StringVar(&chromeInjectMode, "mode", config.RealChromeModeLive, "delivery mode: live or offline")
	chromeInjectCmd.Flags().StringVar(&chromeProfile, "profile", "Default", "ordinary Google Chrome profile directory")
	chromeInjectCmd.Flags().StringSliceVar(&chromeInjectSites, "domain", nil, "limit to a cookie host suffix or SQLite-LIKE pattern (repeatable)")
	chromeInjectCmd.Flags().BoolVar(&chromeAutoApprove, "auto-approve", true, "use macOS Accessibility to approve Chrome's local debugging dialog")
	chromeCmd.AddCommand(chromeStatusCmd, chromeEnableCmd, chromeDisableCmd, chromeInjectCmd)
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
		Mode:        ref.Mode,
		UserDataDir: ref.UserDataDir,
		Profile:     ref.Profile,
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
