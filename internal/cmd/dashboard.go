package cmd

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/doltserver"
	"github.com/steveyegge/gastown/internal/web"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	dashboardPort int
	dashboardBind string
	dashboardOpen bool
)

var dashboardCmd = &cobra.Command{
	Use:     "dashboard",
	GroupID: GroupDiag,
	Short:   "Start the convoy tracking web dashboard",
	Long: `Start a web server that displays the convoy tracking dashboard.

The dashboard shows real-time convoy status with:
- Convoy list with status indicators
- Progress tracking for each convoy
- Last activity indicator (green/yellow/red)
- Auto-refresh every 30 seconds via htmx

If the default port (8080) is in use, the dashboard automatically tries
the next port (up to 10 attempts). Use --port 0 to let the OS assign
a free port. The actual port is printed on startup.

Example:
  gt dashboard                    # Start on default port 8080 (auto-increment on conflict)
  gt dashboard --port 3000        # Start on port 3000
  gt dashboard --port 0           # Let the OS pick a free port
  gt dashboard --bind 0.0.0.0     # Listen on all interfaces
  gt dashboard --open             # Start and open browser`,
	RunE: runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8080, "HTTP port to listen on")
	defaultBind := "127.0.0.1"
	if os.Getenv("IS_SANDBOX") != "" {
		defaultBind = "0.0.0.0"
	}
	dashboardCmd.Flags().StringVar(&dashboardBind, "bind", defaultBind, "Address to bind to (use 0.0.0.0 for all interfaces)")
	dashboardCmd.Flags().BoolVar(&dashboardOpen, "open", false, "Open browser automatically")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	// Check if we're in a workspace - if not, run in setup mode
	var handler http.Handler
	var err error

	townRoot, wsErr := workspace.FindFromCwdOrError()
	if wsErr != nil {
		// No workspace - run in setup mode
		handler, err = web.NewSetupMux()
		if err != nil {
			return fmt.Errorf("creating setup handler: %w", err)
		}
	} else {
		// In a workspace - run normal dashboard

		// Set BEADS_DOLT_PORT and GT_DOLT_PORT so bd/gt subprocesses connect
		// to the actual Dolt SQL server, not the dashboard's HTTP listen port.
		// Without this, inherited env vars could point bd at the wrong port.
		ensureDoltPortEnv(townRoot)

		fetcher, fetchErr := web.NewLiveConvoyFetcher()
		if fetchErr != nil {
			return fmt.Errorf("creating convoy fetcher: %w", fetchErr)
		}

		// Load web timeouts config (nil-safe: NewDashboardMux applies defaults)
		var webCfg *config.WebTimeoutsConfig
		if ts, loadErr := config.LoadOrCreateTownSettings(config.TownSettingsPath(townRoot)); loadErr == nil {
			webCfg = ts.WebTimeouts
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: loading town settings: %v (using defaults)\n", loadErr)
		}

		handler, err = web.NewDashboardMux(fetcher, webCfg)
		if err != nil {
			return fmt.Errorf("creating dashboard handler: %w", err)
		}
	}

	// Listen with port fallback
	ln, err := listenWithFallback(dashboardBind, dashboardPort)
	if err != nil {
		return fmt.Errorf("could not find available port: %w", err)
	}
	defer ln.Close()

	// Resolve actual port (important for --port 0 and auto-increment)
	actualPort := ln.Addr().(*net.TCPAddr).Port

	displayHost := dashboardBind
	if displayHost == "0.0.0.0" {
		if hostname, err := os.Hostname(); err == nil {
			displayHost = hostname
		} else {
			displayHost = "localhost"
		}
	}
	url := fmt.Sprintf("http://%s:%d", displayHost, actualPort)

	// Open browser if requested
	if dashboardOpen {
		go openBrowser(url)
	}

	// Start the server with timeouts
	// Only show the large banner if the terminal is wide enough (98 cols)
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil && width >= 98 {
		fmt.Print(`
 __       __  ________  __        ______    ______   __       __  ________
|  \  _  |  \|        \|  \      /      \  /      \ |  \     /  \|        \
| $$ / \ | $$| $$$$$$$$| $$     |  $$$$$$\|  $$$$$$\| $$\   /  $$| $$$$$$$$
| $$/  $\| $$| $$__    | $$     | $$   \$$| $$  | $$| $$$\ /  $$$| $$__
| $$  $$$\ $$| $$  \   | $$     | $$      | $$  | $$| $$$$\  $$$$| $$  \
| $$ $$\$$\$$| $$$$$   | $$     | $$   __ | $$  | $$| $$\$$ $$ $$| $$$$$
| $$$$  \$$$$| $$_____ | $$_____| $$__/  \| $$__/ $$| $$ \$$$| $$| $$_____
| $$$    \$$$| $$     \| $$     \\$$    $$ \$$    $$| $$  \$ | $$| $$     \
 \$$      \$$ \$$$$$$$$ \$$$$$$$$ \$$$$$$   \$$$$$$  \$$      \$$ \$$$$$$$$

 ________   ______          ______    ______    ______   ________   ______   __       __  __    __
|        \ /      \        /      \  /      \  /      \ |        \ /      \ |  \  _  |  \|  \  |  \
 \$$$$$$$$|  $$$$$$\      |  $$$$$$\|  $$$$$$\|  $$$$$$\ \$$$$$$$$|  $$$$$$\| $$ / \ | $$| $$\ | $$
   | $$   | $$  | $$      | $$ __\$$| $$__| $$| $$___\$$   | $$   | $$  | $$| $$/  $\| $$| $$$\| $$
   | $$   | $$  | $$      | $$|    \| $$    $$ \$$    \    | $$   | $$  | $$| $$  $$$\ $$| $$$$\ $$
   | $$   | $$  | $$      | $$ \$$$$| $$$$$$$$ _\$$$$$$\   | $$   | $$  | $$| $$ $$\$$\$$| $$\$$ $$
   | $$   | $$__/ $$      | $$__| $$| $$  | $$|  \__| $$   | $$   | $$__/ $$| $$$$  \$$$$| $$ \$$$$
   | $$    \$$    $$       \$$    $$| $$  | $$ \$$    $$   | $$    \$$    $$| $$$    \$$$| $$  \$$$
    \$$     \$$$$$$         \$$$$$$  \$$   \$$  \$$$$$$     \$$     \$$$$$$  \$$      \$$ \$$   \$$

`)
	} else {
		fmt.Print("\n  WELCOME TO GASTOWN\n\n")
	}
	listenAddr := ln.Addr().String()
	fmt.Printf("  launching dashboard at %s  •  api: %s/api/  •  listening on %s  •  ctrl+c to stop\n", url, url, listenAddr)

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return server.Serve(ln)
}

// ensureDoltPortEnv sets GT_DOLT_PORT, BEADS_DOLT_PORT, and BEADS_DOLT_SERVER_HOST
// to the actual Dolt server connection info. This prevents bd subprocesses from
// inheriting stale or incorrect values from the environment.
// Reads the running state from daemon/dolt-state.json; falls back to
// doltserver.DefaultConfig; otherwise uses the Dolt defaults.
func ensureDoltPortEnv(townRoot string) {
	var port int
	if state, err := doltserver.LoadState(townRoot); err == nil && state.Port > 0 {
		port = state.Port
	} else {
		port = doltserver.DefaultPort
	}
	portStr := strconv.Itoa(port)
	os.Setenv("GT_DOLT_PORT", portStr)
	os.Setenv("BEADS_DOLT_PORT", portStr)

	// Propagate host so bd doesn't fall back to 127.0.0.1.
	doltCfg := doltserver.DefaultConfig(townRoot)
	if doltCfg.Host != "" {
		os.Setenv("BEADS_DOLT_SERVER_HOST", doltCfg.Host)
	}
}

// maxPortRetries is the number of consecutive ports to try on EADDRINUSE.
const maxPortRetries = 10

// listenWithFallback tries to listen on bind:port. If port is 0, the OS picks
// a free port. Otherwise, on EADDRINUSE it auto-increments the port up to
// maxPortRetries times.
func listenWithFallback(bind string, port int) (net.Listener, error) {
	// --port 0: let the OS assign a free port, no retry needed
	if port == 0 {
		return net.Listen("tcp", fmt.Sprintf("%s:0", bind))
	}

	var lastErr error
	for i := 0; i < maxPortRetries; i++ {
		addr := fmt.Sprintf("%s:%d", bind, port+i)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			if i > 0 {
				fmt.Fprintf(os.Stderr, "note: port %d in use, using %d instead\n", port, port+i)
			}
			return ln, nil
		}
		lastErr = err
		if !isAddrInUse(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("ports %d-%d all in use: %w", port, port+maxPortRetries-1, lastErr)
}

// isAddrInUse reports whether err is an EADDRINUSE error.
func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.EADDRINUSE)
		}
	}
	return false
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
