package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/notify"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
)

const maxAnnotationLen = 120

// hookInput represents the JSON payload from a Claude Code hook.
type hookInput struct {
	HookEventName        string `json:"hook_event_name"`
	Message              string `json:"message"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

var pluginCmd = &cobra.Command{
	Use:    "plugin",
	Short:  "Plugin commands",
	Hidden: true,
}

var pluginHandleClaudeCmd = &cobra.Command{
	Use:    "handle-claude",
	Short:  "Handle Claude Code hook events",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName := os.Getenv(semconv.SessionEnvVar)
		if sessionName == "" {
			return nil
		}

		var input hookInput
		if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
			return nil // fail-open
		}

		tc := tmux.NewClient(tmux.NewRealRunner())
		sesSvc := session.NewService(tc, &hooks.NoOp{})
		notifySvc := notify.NewDefaultService()

		switch input.HookEventName {
		case "UserPromptSubmit":
			_ = sesSvc.SetStatus(sessionName, semconv.StatusRunning, "")

		case "Notification":
			annotation := truncate(input.Message, maxAnnotationLen)
			_ = sesSvc.SetStatus(sessionName, semconv.StatusWaiting, annotation)
			_ = notifySvc.Send("codeherd", annotation)

		case "Stop":
			annotation := truncate(input.LastAssistantMessage, maxAnnotationLen)
			_ = sesSvc.SetStatus(sessionName, semconv.StatusWaiting, annotation)
			_ = notifySvc.Send("codeherd", annotation)
		}

		return nil
	},
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func init() {
	pluginCmd.AddCommand(pluginHandleClaudeCmd)
	rootCmd.AddCommand(pluginCmd)
}
