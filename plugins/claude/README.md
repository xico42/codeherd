# codeherd-session-status

A Claude Code plugin that tracks codeherd session status and sends desktop notifications when agents need attention.

## What it does

- Monitors Claude Code hook events (PreToolUse, Notification, Stop)
- Marks sessions as "waiting" when the agent needs user input
- Marks sessions as "running" when the agent resumes work
- Adds a ⚡ prefix to tmux session names for quick visibility in `Ctrl+b s`
- Sends desktop notifications when a session needs attention
- Shows status annotations in the codeherd TUI dashboard

## Prerequisites

- `codeherd` must be installed and available in PATH
- Sessions must be started via `ch create session`

## Installation

### From marketplace

Add the codeherd marketplace and install:

```
/plugin marketplace add xico42/codeherd
/plugin install codeherd-session-status@codeherd-plugins
```

### Local development

```bash
claude --plugin-dir ./plugins/claude
```
