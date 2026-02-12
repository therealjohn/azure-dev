// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

// monitor.go implements the `azd ai agent monitor` command.
//
// This command provides two modes for monitoring a deployed hosted agent:
//
// 1. Browser mode (default): Opens the Foundry Portal Monitor page for the agent.
//    This page shows real-time metrics, logs, and container health in the portal UI.
//
// 2. Terminal mode (--logs): Streams container logs directly to the terminal using
//    the Foundry Agent Service logstream REST API. This is useful for debugging
//    startup issues or monitoring agent behavior without leaving the terminal.
//    Equivalent to `az cognitiveservices agent logs show`.
//
// The logstream REST endpoint streams chunked text/plain from:
//   GET {projectEndpoint}/agents/{name}/versions/{version}/containers/default:logstream
//   ?kind={console|system}&tail={N}&api-version=2025-11-15-preview
// See: https://learn.microsoft.com/en-us/azure/ai-foundry/agents/concepts/hosted-agents#view-container-log-stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"

	"azureaiagent/internal/version"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// monitorFlags holds the command-line flags for the monitor command.
type monitorFlags struct {
	service string
	logs    bool
	follow  bool
	tail    int
	logType string
}

func newMonitorCommand() *cobra.Command {
	flags := &monitorFlags{}

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: fmt.Sprintf("Monitor a deployed hosted agent. %s", color.YellowString("(Preview)")),
		Long: `Opens the Foundry Portal Monitor page for the agent, or streams container logs
in the terminal when --logs is specified.

Examples:
  azd ai agent monitor                          # Open monitor page in browser
  azd ai agent monitor --logs                   # Stream console logs
  azd ai agent monitor --logs --type system      # Stream system event logs
  azd ai agent monitor --logs --tail 100         # Fetch last 100 lines
  azd ai agent monitor --logs --follow=false     # Fetch recent logs and exit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(flags)
		},
	}

	cmd.Flags().StringVar(
		&flags.service,
		"service",
		"",
		"The name of the agent service in azure.yaml. Defaults to the first agent service.",
	)

	cmd.Flags().BoolVar(
		&flags.logs,
		"logs",
		false,
		"Stream container logs in the terminal instead of opening the portal.",
	)

	cmd.Flags().BoolVar(
		&flags.follow,
		"follow",
		true,
		"Stream logs in real-time. When false, fetches recent logs and exits. Only used with --logs.",
	)

	cmd.Flags().IntVar(
		&flags.tail,
		"tail",
		50,
		"Number of trailing log lines to fetch (1-300). Only used with --logs.",
	)

	cmd.Flags().StringVar(
		&flags.logType,
		"type",
		"console",
		"Type of logs: 'console' for stdout/stderr, 'system' for container events. Only used with --logs.",
	)

	return cmd
}

func runMonitor(flags *monitorFlags) error {
	// Load agent context by reading azure.yaml and .azure/<env>/.env directly.
	// Standalone commands cannot use the azd gRPC client (see env_helpers.go).
	agentCtx, err := loadAgentContext(flags.service)
	if err != nil {
		return err
	}

	if flags.logs {
		// Terminal mode: stream logs from the logstream REST API
		return streamLogs(agentCtx, flags)
	}

	// Browser mode: open the Foundry Portal Monitor page
	return openMonitorPage(agentCtx)
}

// openMonitorPage constructs the Foundry Portal Monitor URL and opens it in
// the user's default browser. The URL follows the same structure as the
// playground URL but routes to the /monitor page instead of /build.
func openMonitorPage(agentCtx *agentContext) error {
	if agentCtx.ProjectResourceID == "" {
		return fmt.Errorf(
			"AZURE_AI_PROJECT_ID not set. Cannot construct portal URL. " +
				"Run 'azd provision' or 'azd ai agent init --project-id <id>' first",
		)
	}

	monitorURL, err := buildPortalURL(
		agentCtx.ProjectResourceID, agentCtx.AgentName, agentCtx.AgentVersion, "monitor",
	)
	if err != nil {
		return fmt.Errorf("failed to construct monitor URL: %w", err)
	}

	fmt.Printf("Opening monitor page: %s\n", output.WithLinkFormat(monitorURL))

	// Open in the default browser. The extension process runs inside a Windows
	// Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, which kills child
	// processes when the extension exits. Using ShellExecute (via browser.OpenURL)
	// or cmd.exe /c start can result in the browser being killed immediately.
	// Instead, we use powershell Start-Process on Windows, which creates a fully
	// independent process outside the job object. This matches azd's own fallback
	// strategy in cmd/util.go openWithDefaultBrowser().
	if err := openBrowser(monitorURL); err != nil {
		// If the browser can't be opened (e.g. headless environment), print the URL
		// so the user can copy-paste it manually.
		fmt.Fprintf(color.Error, "Could not open browser: %v\n", err)
		fmt.Println("Open this URL manually:")
		fmt.Println(monitorURL)
	}

	return nil
}

// openBrowser opens a URL in the user's default browser.
//
// On Windows, the extension process runs inside a Windows Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (set by azd's exec.CmdTree). This means
// child processes spawned via ShellExecute or cmd.exe are killed when the
// extension process exits. To avoid this, we use powershell.exe Start-Process
// which creates a fully independent process outside the job object.
// This matches azd's own fallback strategy in cmd/util.go.
//
// On macOS/Linux, we use the standard `open` / `xdg-open` commands.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		// Use powershell Start-Process and wait for it to complete (.Run not .Start)
		// so the extension process doesn't exit before the browser is launched.
		// The URL is passed as a single-quoted string to handle commas.
		return exec.Command("powershell.exe",
			"-NoProfile", "-Command", "Start-Process", fmt.Sprintf("'%s'", url),
		).Run()
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

// streamLogs connects to the Foundry Agent Service logstream REST API and
// streams container logs (console or system) to stdout.
//
// The logstream endpoint returns chunked text/plain with one log line per chunk.
// It supports two log types:
//   - "console": container stdout/stderr (application logs)
//   - "system": container system events (start/stop/scaling events)
//
// The connection has server-enforced limits:
//   - Max connection duration: 10 minutes
//   - Idle timeout: 1 minute (closed if no activity)
//
// When --follow is true, we stream until the user presses Ctrl+C or the server
// closes the connection. When false, we fetch recent logs and exit.
//
// Unlike other API calls in this extension that use the Azure SDK pipeline,
// log streaming uses a plain http.Client. The SDK pipeline's retry policy
// buffers response bodies, which prevents streaming — the scanner would block
// waiting for a body that never completes. A plain http.Client lets us read
// the chunked response incrementally as data arrives.
func streamLogs(agentCtx *agentContext, flags *monitorFlags) error {
	// Validate tail range (API enforces 1-300)
	if flags.tail < 1 || flags.tail > 300 {
		return fmt.Errorf("--tail must be between 1 and 300, got %d", flags.tail)
	}

	// Validate log type
	if flags.logType != "console" && flags.logType != "system" {
		return fmt.Errorf("--type must be 'console' or 'system', got %q", flags.logType)
	}

	// Construct the logstream URL
	// Format: {projectEndpoint}/agents/{name}/versions/{version}/containers/default:logstream
	logURL := fmt.Sprintf(
		"%s/agents/%s/versions/%s/containers/default:logstream?kind=%s&tail=%s&api-version=%s",
		agentCtx.ProjectEndpoint,
		agentCtx.AgentName,
		agentCtx.AgentVersion,
		flags.logType,
		strconv.Itoa(flags.tail),
		"2025-11-15-preview",
	)

	fmt.Fprintf(os.Stderr, "Streaming %s logs for agent %s (version %s)...\n",
		flags.logType, agentCtx.AgentName, agentCtx.AgentVersion)
	if flags.follow {
		fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop streaming.\n")
	}
	fmt.Fprintln(os.Stderr)

	// Acquire a bearer token for the Foundry API scope.
	// We use the same scope (https://ai.azure.com/.default) as the AgentClient.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	tokenResp, err := agentCtx.Credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://ai.azure.com/.default"},
	})
	if err != nil {
		return fmt.Errorf("failed to acquire access token: %w", err)
	}

	// Build a plain HTTP request instead of using the Azure SDK pipeline.
	// The SDK pipeline buffers response bodies for retry logic, which blocks
	// streaming — the reader waits for a body that never finishes.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create log stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenResp.Token)
	req.Header.Set("User-Agent", fmt.Sprintf("azd-ext-azure-ai-agents/%s", version.Version))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("log stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("log stream returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Stream the response body line-by-line to stdout.
	// The logstream API returns chunked text/plain, one log line per chunk.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())

		// If not following, we read what's available but the server will close
		// the connection after sending the tail lines.
	}

	if err := scanner.Err(); err != nil {
		// context.Canceled means the user pressed Ctrl+C, which is expected
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "\nLog streaming stopped.")
			return nil
		}
		// io.EOF or unexpected close is normal for streaming endpoints
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}
