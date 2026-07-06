package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var syscallExec = syscall.Exec

type RunAgentCmd struct{}

func (c *RunAgentCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:   "run <agent> [-- <args>]",
		Short: "Run a registered agent in the current shell",
		Long: "Resolves a registered agent from config and replaces the current process with it. " +
			"The agent inherits the current environment with its configured env vars overlaid.\n\n" +
			"Arguments after -- are forwarded to the agent command verbatim, appended after the " +
			"agent's configured args. Example: ch run claude -- --model opus",
		Args: func(cmd *cobra.Command, args []string) error {
			if dash := cmd.ArgsLenAtDash(); dash == -1 {
				if len(args) != 1 {
					return fmt.Errorf("accepts exactly one agent name; pass extra args after --")
				}
			} else if dash != 1 {
				return fmt.Errorf("accepts exactly one agent name before --; pass extra args after --")
			}
			return nil
		},
		RunE: c.Run,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeAgents(cmd, args, toComplete)
		},
	}
}

func (c *RunAgentCmd) Run(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	agent, err := cfg.AgentByName(agentName)
	if err != nil {
		return fmt.Errorf("resolving agent: %w", err)
	}

	// agent.Cmd may contain the full command string (e.g. "ai-jail -- claude")
	// when Args is empty. Parse it to extract the binary for lookPath and
	// build execArgs correctly.
	cmdFields := strings.Fields(agent.Cmd)
	if len(cmdFields) == 0 {
		return fmt.Errorf("agent command is empty")
	}
	binaryName := cmdFields[0]
	cmdPrefixArgs := cmdFields[1:]

	// Resolve the agent binary using the current shell's PATH.
	// Agent-configured PATH overrides are applied to the runtime environment
	// but do not affect binary resolution.
	binary, err := lookPath(binaryName)
	if err != nil {
		return fmt.Errorf("agent command not found: %w", err)
	}

	execArgs := append([]string{binaryName}, cmdPrefixArgs...)
	execArgs = append(execArgs, agent.Args...)
	execArgs = append(execArgs, args[1:]...)
	mergedEnv := mergeEnv(os.Environ(), agent.Env)

	if err := syscallExec(binary, execArgs, mergedEnv); err != nil {
		return fmt.Errorf("exec agent: %w", err)
	}
	return nil
}

func mergeEnv(current []string, overrides map[string]string) []string {
	result := make([]string, len(current))
	copy(result, current)

	existing := make(map[string]int, len(result))
	for i, entry := range result {
		if idx := strings.Index(entry, "="); idx >= 0 {
			existing[entry[:idx]] = i
		}
	}

	for key, val := range overrides {
		if i, ok := existing[key]; ok {
			result[i] = key + "=" + val
		} else {
			result = append(result, key+"="+val)
		}
	}

	return result
}
