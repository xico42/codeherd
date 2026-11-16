# codeherd plugins

What a codeherd plugin is, how the daemon talks to it, and how a user trusts and
configures one. Background: [the API conventions](conventions.md) and
[errors](errors.md).

Each section carries a status:

- **Settled**: decided. Change it only by reopening the decision.
- **Proposal**: a direction agreed in principle, to be confirmed or revised
  when the first plugin kind that needs it is implemented.
- **Open**: deliberately undecided, waiting for a concrete use case.

This document supersedes the plugin contract in the "Agentic OS on codeherd"
vision doc: there is no `codeherd-plugin.toml` manifest, no build step, no
exit-code protocol and no secret references. Installing a plugin from a remote
source is a future convenience, not part of what a plugin is.

---

## 1. What a plugin is

**Status: Settled.**

- **A plugin is one executable.** Any language: Go, Bash, Node, Rust. codeherd
  runs it as an argv with no arguments, never through a shell. No SDK is
  required; a thin Go helper may be offered.
- **It describes itself.** Everything codeherd needs to know about a plugin
  comes from its answer to `describe` (section 4). There is no metadata file
  next to it.
- **It is registered, not installed.** The user points codeherd at an
  executable that already exists. Registering it is the user's statement that
  this binary is trusted (section 5). How the binary got there is not
  codeherd's concern.
- **One plugin, many instances.** A plugin is registered once and instantiated
  any number of times with different config, for example `jira-qive` and
  `jira-freelance` from one `jira` plugin (section 6).
- **Only the daemon calls plugins.** Clients (the CLI, Limoni, agents) never
  talk to a plugin directly; they see plugin results through the API.
- **Builtins and plugins share one registry.** A builtin (for example the
  `local` artifact store) implements the same kind in-process, with the same
  methods and results. A plugin can stand in for any builtin.
- **A plugin calls codeherd through the CLI.** To read or change codeherd
  state, a plugin runs `ch` with JSON output, the same commands a person uses,
  carrying the callback value from its request (section 3.4). There is no
  second callback API.
- **A plugin runs on the machine that holds what it acts on.** A hook on a
  workspace event runs where that workspace's worktree is.

---

## 2. The protocol

**Status: Settled.**

codeherd's own protocol, shaped like the MCP stdio transport (revision
2026-07-28) so that authors who know MCP feel at home. It is not MCP: the
methods, params and results are codeherd's.

- **JSON-RPC 2.0** (`"jsonrpc": "2.0"` on every message).
- **One message per line** on stdin and stdout, UTF-8. A message must not
  contain an embedded newline. No batches: every line is one request or one
  response.
- **stdout carries only protocol messages.** Anything else on stdout is a
  protocol error.
- **stderr is for logs.** codeherd captures it per instance. Output on stderr
  never means an error.
- **Stateless.** There is no handshake and no session. Every request carries
  everything the plugin needs, including the contract version and all of
  codeherd's context (section 3). A plugin must not rely on an earlier request
  having happened.
- **codeherd sends requests; the plugin sends responses.** A plugin never sends
  a JSON-RPC request to codeherd. It calls back through `ch` instead.
- **Concurrent calls.** Several requests may be in flight to a long-lived
  plugin; it may answer in any order, and responses match requests by `id`.
  codeherd may also run several one-shot processes of the same instance at
  once. A plugin that keeps state locks it itself.
- **Bulk data travels as files.** File contents, transcripts and other large
  payloads are never inlined in a message. codeherd passes a path to read
  from, or a directory to write into, and the message carries only paths.

### 2.1 Lifetimes

The plugin implements one loop: read a line, answer it, repeat until stdin
closes. codeherd decides per call how long the process lives.

- **One-shot:** codeherd spawns the plugin, sends one request, reads the reply,
  and closes stdin.
- **Long-lived:** codeherd keeps the process running and sends further
  requests on the same stdin. One process serves one instance.
- **A plugin may exit after one reply.** codeherd then treats it as one-shot
  and spawns it again for the next call. Supporting long-lived use costs the
  author nothing but a loop.
- **Shutdown:** codeherd closes stdin and waits; if the process is still
  running it sends SIGTERM, then SIGKILL. A plugin should exit when stdin
  reaches EOF.

Because one long-lived process serves many calls, anything that varies per
call (namespace, task, run, callback) can only travel in the request, never in
the process environment.

### 2.2 The smallest plugin

A hook plugin in Bash that denies every session start:

```bash
#!/usr/bin/env bash
read -r req
id=$(jq -c .id <<<"$req")
case "$(jq -r .method <<<"$req")" in
  describe) echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"id\":\"guard\",\"kinds\":[\"hooks\"],\"hooks\":[{\"method\":\"check\",\"role\":\"guard\",\"on\":[\"session.pre\"]}]}}" ;;
  check)    echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"error\":{\"code\":1,\"message\":\"not today\",\"data\":{\"code\":\"PERMISSION_DENIED\",\"reason\":\"DENIED\",\"domain\":\"codeherd.dev\"}}}" ;;
  *)        echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"error\":{\"code\":-32601,\"message\":\"method not found\"}}" ;;
esac
```

Replies must be single lines: use `jq -c`, never pretty-printed JSON. When a
reply is malformed, codeherd's error says so in those words.

---

## 3. Requests, results and errors

**Status: Proposal.** The shape is agreed; field names are confirmed by the
first kind that is implemented (the artifact store).

### 3.1 Methods

- `describe` is the only method every plugin implements.
- A kind's methods are named `<kind>.<method>`, for example
  `artifactStore.put` or `taskSource.query`. Each kind's methods, params and
  results are specified when that kind is implemented.
- A hook handler has whatever name the plugin gives it in `describe`
  (section 4). codeherd calls it with the event as input.

### 3.2 Request params

Every request except `describe` has the same params:

```json
{ "jsonrpc": "2.0", "id": 7, "method": "taskSource.query",
  "params": {
    "apiVersion": "codeherd.dev/v1alpha1",
    "instance": "jira-qive",
    "config": { "base_url": "https://example.atlassian.net",
                "token_cmd": ["op", "read", "op://work/jira/token"] },
    "host": {
      "bin": "/home/user/.local/bin/ch",
      "callback": "chcb1.eyJzb2NrZXQiOi...",
      "stateDir": "/home/user/.local/state/codeherd/plugins/jira-qive" },
    "context": { "namespace": "ns_...", "task": null, "run": null },
    "input": { "query": "parent = STAFF-569" } } }
```

| Field | Meaning |
|---|---|
| `apiVersion` | The plugin contract version: `codeherd.dev/v1alpha1` until the plugin system is complete, then `codeherd.dev/v1`. Independent of the `codeherd.v1alpha1` API version. |
| `instance` | The instance name |
| `config` | The instance's config (section 6) |
| `host.bin` | Absolute path of the `ch` binary of the daemon making the call |
| `host.callback` | An opaque value for `ch` callbacks (section 3.4) |
| `host.stateDir` | The instance's private state directory |
| `context` | On whose behalf the call runs; its fields grow with the kinds |
| `input` | The method's own input |

`describe` takes only `apiVersion`.

### 3.3 Errors

- **Method not found** (`-32601`) means the plugin does not implement that
  method. codeherd treats it as "unsupported", never as a failure of the
  operation that asked.
- **Application errors** use JSON-RPC code `1`, with a codeherd error in
  `data`: the fields of `Error` in [errors.md](errors.md) (`code`, `reason`,
  `domain`, `causes`, `metadata`), with `message` taken from the JSON-RPC
  error. A plugin's own reasons use its plugin id as `domain`, so they cannot
  collide with core reasons.
- **Deny** is the application error with reason `DENIED` in domain
  `codeherd.dev`. It is only meaningful as the answer to a guard hook
  (section 7); anywhere else it is an ordinary failure.
- Other JSON-RPC errors (`-32700`, `-32600`, `-32602`, `-32603`) are protocol
  failures and fail the call with reason `PLUGIN_PROTOCOL_ERROR`.

### 3.4 Calling back

A plugin calls codeherd by running `host.bin` with the request's
`host.callback` value, either as an environment variable on that one child
process or as a flag:

```bash
ch=$(jq -r .params.host.bin <<<"$req")
cb=$(jq -r .params.host.callback <<<"$req")
CODEHERD_CALLBACK="$cb" "$ch" get task "$task" -o json
"$ch" --callback "$cb" get task "$task" -o json
```

- **The value is opaque to the plugin.** `ch` decodes it. It holds what a
  callback needs: the daemon to reach, the namespace, the calling instance and
  run. codeherd may add to it, for example a short-lived credential, without
  any plugin changing.
- **The callback is authoritative.** It overrides any `CODEHERD_*` context
  variable that `ch` would otherwise default from.
- **Prefer the environment variable** once the value carries a credential: it
  stays out of `ps`, where argv is visible.
- `ch` reads its own command input from stdin as usual; the callback never
  travels there.

### 3.5 Process environment

**Status: Settled.**

A plugin process gets only:

1. **A base allowlist** from the daemon's environment: `HOME`, `LOGNAME`,
   `PATH`, `SHELL`, `TERM`, `USER`, `LANG`, `LC_*`, `TMPDIR` and `XDG_*`. Values
   that start with `()` (exported shell functions) are dropped.
2. **The instance's `env`**: fixed values set in config.
3. **The instance's `pass`**: names copied from the daemon's environment at
   call time.

Later layers override earlier ones. Nothing else reaches the plugin:

- **No `CODEHERD_*` variables.** codeherd sets none on a plugin process, strips
  any it inherited (a daemon started from inside a session carries that
  session's), and `pass` cannot name them. Everything codeherd-specific
  travels in the request.
- **No forwarding from clients.** Values come only from the daemon's own
  environment, whoever started it: a systemd user service gets what the user
  manager holds (`environment.d`, `systemctl --user import-environment`); an
  auto-started daemon holds the environment of the shell that started it, and
  a daemon restart refreshes it. Many calls have no client at all (a periodic
  sync, an operation that outlives the `ch` that submitted it).
- **Variables tied to one login or connection go stale** in a long-running
  daemon (`SSH_AUTH_SOCK`, `DISPLAY`, `KRB5CCNAME`). Point `env` at a stable
  location instead where one exists, for example a systemd, 1Password or
  gnome-keyring SSH agent socket.

### 3.6 Credentials

**Status: Settled.**

- **Plugins fetch their own credentials.** codeherd resolves no secrets. A
  plugin reads the files its tools already use (`~/.config/gh`, `~/.netrc`,
  git's credential helpers) or runs a command named in its instance config,
  such as `token_cmd = ["op", "read", "op://work/jira/token"]`.
- **Never source a shell profile.** `~/.zshrc` and friends assume an
  interactive shell, can block on a prompt, and can print to stdout, which
  breaks the protocol.
- **Never log a credential**, on stderr or anywhere else. codeherd cannot
  redact what it never sees; the conformance kit checks for it where it can.

### 3.7 State

A plugin keeps durable state only in `host.stateDir` or in codeherd's store
through `ch`. Memory in a long-lived process is a cache: the plugin must behave
the same after a restart.

---

## 4. describe

**Status: Proposal.** The fields below are the ones agreed; new fields are
added with the kinds that need them.

`describe` is how a plugin registers itself. Its result holds everything a
manifest would have:

```json
{ "jsonrpc": "2.0", "id": 1, "result": {
    "id": "jira",
    "version": "0.3.1",
    "apiVersions": ["codeherd.dev/v1alpha1"],
    "kinds": [
      { "kind": "taskSource", "methods": ["query", "get"] },
      { "kind": "hooks" } ],
    "hooks": [
      { "method": "hook.pushStatus", "role": "observe",
        "on": ["task.after_transition"], "match": { "to": "in-review" } } ],
    "env": [
      { "name": "SSH_AUTH_SOCK", "required": false,
        "description": "pushes over SSH when set" } ],
    "config": { "$schema": "https://json-schema.org/draft/2020-12/schema",
                "type": "object", "required": ["base_url"],
                "properties": { "base_url": { "type": "string" } } } } }
```

| Field | Meaning |
|---|---|
| `id` | The plugin's name: the instance's `use`, and the `domain` of its errors |
| `version` | The plugin's own version, for people; codeherd never compares it |
| `apiVersions` | Contract versions the plugin speaks; codeherd picks one it also speaks |
| `kinds` | The kinds it implements and, per kind, which methods |
| `hooks` | Its hook handlers: the method, its role (section 7), the events it subscribes to by default, and an optional default match |
| `env` | Environment variables the plugin reads beyond the base allowlist; `required` ones must be provided by the instance's `env` or `pass` |
| `config` | A JSON Schema for instance config; codeherd validates instances against it |

- **`describe` is static.** It describes the binary, not an instance: it gets no
  config, no host block and no context, and must return the same result every
  time for the same binary.
- **codeherd calls it at registration and caches the result.** It is not called
  per connection or per call (section 5).
- **`env` is a declaration, not a grant.** codeherd shows it at registration
  and reports an instance broken, by variable name, when a required one is not
  provided. It never passes a variable the instance did not name.
- **"Requires" beyond `env` is open** (section 10).

---

## 5. Registration and trust

**Status: Proposal.** The CLI spelling is settled with the rest of the CLI.

- **Registering is the trust decision.** The user registers a path to an
  executable. codeherd records the path, a SHA-256 of the file, and the result
  of `describe`.
- **Registering shows, then asks.** The CLI prints the plugin's declarations
  (kinds, hooks, env, config schema), or when re-registering what changed, and
  asks for confirmation. A flag skips the question for scripts. The `plugin`
  noun is free once today's hidden `ch plugin handle-claude` is removed in M0.
- **A plugin is a cluster-scoped resource** (ID prefix `plg`, see
  [conventions.md section 4.2](conventions.md#42-the-prefix-registry)), owned
  by the daemon's store.
- **A changed binary is a different binary.** Before starting a plugin process,
  codeherd checks the file's hash against the recorded one. On a mismatch the
  plugin is disabled: calls to it fail with reason `PLUGIN_CHANGED`, and it
  stays disabled until the user registers it again.
- **The hash covers only the registered file.** An interpreted plugin that
  loads other files (a sourced script, a module) can change behind it without
  detection, and so can its interpreter. Authors who want full coverage ship a
  single file.
- **Builtins appear as plugins.** They are listed alongside registered plugins,
  marked builtin, with no path and no hash. They cannot be unregistered.
- **Remote install helpers are future work.** Something like fetching a release
  binary from `owner/repo` may come later, and will end in the same
  registration step.

---

## 6. Instances

**Status: Proposal.** Where instance config lives follows the namespace config
design.

An instance is a named, configured use of a registered plugin. Instances are
namespaced: they are declared in a namespace's config, and the daemon exposes
each as a read-only resource (ID prefix `pli`) the way repos declared in config
are exposed. An instance's identity is its namespace and name; renaming it
makes a new instance with a new, empty state directory.

```toml
[plugins.jira-qive]
use = "jira"
base_url = "https://example.atlassian.net"
token_cmd = ["op", "read", "op://work/jira/token"]

[plugins.gitrefs-work]
use = "artifact-gitrefs"
env  = { SSH_AUTH_SOCK = "/run/user/1000/ssh-agent.socket" }
pass = ["GIT_SSH_COMMAND"]
timeout = "60s"
```

- `use` names the plugin by its `describe` id.
- `env`, `pass` (section 3.5), `timeout` (section 8) and `hooks` (section 7)
  are codeherd's. Everything else is the plugin's config, validated against
  its `config` schema and sent in every request.
- Which instance serves a kind for a namespace is specified with each kind.
  For the artifact store, the namespace config names one instance, and the
  builtin `local` store is the default when it names none.
- Each instance has its own state directory and its own captured stderr.
- An instance of a disabled or unregistered plugin, or one missing a required
  environment variable, is reported as broken; it does not stop codeherd from
  starting.

---

## 7. Hooks

**Status: Proposal.** The event catalog is designed with the plugin hooks
milestone.

### 7.1 Roles

Every hook handler has one role, declared in `describe`:

| Role | Runs | Does | If it fails or cannot run |
|---|---|---|---|
| `guard` | before the event | may deny it with `DENIED` and a reason | the operation fails |
| `inject` | before the event | returns patches: env, files, mounts, a supervised process, opaque state | the operation fails |
| `observe` | after the event | reacts, for example sets git config on a fresh clone | the operation succeeds with a `Warning` |

- **The operation waits for its hooks**, observers included, so a workspace is
  ready only after its observers ran.
- **Guards and injectors fail closed.** An operation that depends on a hook
  does not go ahead without it. This does not contradict section 8: codeherd
  itself keeps running; only that operation fails.
- **Several hooks on one event run in config order.** Two injectors setting the
  same key is an error.
- **Injection is additive.** Patches cannot remove anything, edit an agent's
  argv, or change its sandbox or agent. The opaque state an injector returns
  comes back to the matching end event, so a token minted on start can be
  revoked on end.
- A denial's `message` is shown to the person or agent that caused the event.

### 7.2 Events

- **Events are about workspaces and sessions, not git operations:** a workspace
  is provisioned (it gets its worktree, which may be a reused pool slot), is
  released, is deleted; a session starts, ends. Task and run events join with
  the task layer.
- **End events always fire,** including when a session died outside codeherd
  (its tmux session was killed), so state handed out on start can be cleaned
  up.

### 7.3 The user's config decides

`describe` declares each hook handler with its default events and match. An
instance can override them:

- turn a declared hook off;
- narrow when it fires with a `match`;
- attach a declared hook method to other events.

Config can only use methods the plugin declared as hook handlers, in the role
the plugin declared. It cannot turn an arbitrary method into a hook.

```toml
[plugins.qv-creds]
use = "qv"

[plugins.qv-creds.hooks."hook.inject"]
on = ["session.pre"]
match = { sandbox = "ai-jail" }

[plugins.qv-creds.hooks."hook.audit"]
enabled = false
```

---

## 8. Failure

**Status: Settled.**

- **A broken plugin degrades codeherd; it never stops it.** codeherd starts,
  lists and serves everything else when a plugin is missing, changed, slow or
  crashing. An operation that depends on a broken plugin fails (section 7.1);
  nothing else does.
- **Every failure names the plugin and the instance** and keeps the tail of its
  stderr in the error's metadata.
- Failure reasons, all in domain `codeherd.dev`:

| Reason | When |
|---|---|
| `PLUGIN_CHANGED` | the binary's hash differs from the registered one |
| `PLUGIN_NOT_FOUND` | the registered path no longer exists or is not executable |
| `PLUGIN_ENV_MISSING` | a variable the plugin declares as required is not provided |
| `PLUGIN_PROTOCOL_ERROR` | a non-protocol line on stdout, a malformed reply, or a JSON-RPC protocol error |
| `PLUGIN_EXITED` | the process exited before answering |
| `PLUGIN_TIMEOUT` | no answer within the call's deadline |

These join the catalog in [errors.md](errors.md) when the plugin host is
implemented.

**Deadlines.** Every plugin call has its own deadline, independent of any
client: a default per kind, which an instance may override with `timeout`.
A call that runs inside an asynchronous operation is bounded by it too.

---

## 9. Rollout

**Status: Settled.** Milestones are the PRD's.

- **Prerequisites (M0).** The daemon with its store, and the whole CLI as a
  daemon client with JSON output and `--callback`. Plugins are resources in
  that store and call back through that CLI, so the plugin host is not built
  before them.
- **The plugin host (M2), for the artifact store kind:**
  - `describe`, registration with the hash check, and instances with `env`,
    `pass` and `timeout`;
  - one-shot calls only;
  - the `artifactStore.*` methods, the builtin `local` store, and the
    `artifact-gitrefs` plugin as the first external one;
  - a conformance kit that runs a plugin through the protocol and its kind's
    methods.

  `describe` may already declare `hooks`; codeherd records them and shows them
  at registration, but does not dispatch them.
- **Task sources (M5)** are the second kind on the same host.
- **The complete plugin system (M9):** long-lived processes, hook dispatch with
  the three roles and the instance hook overrides, the remaining kinds, and the
  contract frozen as `codeherd.dev/v1`.

---

## 10. Open questions

**Status: Open.**

- **Whether minimal hook dispatch moves up to M2.** M0 removes the config hooks
  and M9 brings plugin hooks, so in between nothing replaces `post-worktree =
  "npm install"` or setting git identity on a clone. Guard and observe on
  workspace events, one-shot only, would close that gap.
- **Where `.herd` rendering, ports and file copy go** when they leave the core:
  a builtin plugin on by default (keeps zero setup), or an external plugin the
  user registers. Either way rendering moves to the workspace-provisioned
  event, its context becomes the workspace (`.SessionName` no longer applies),
  `env "VAR"` reads the plugin's environment, and a port registry in the state
  directory can avoid collisions across workspaces.
- **Whether plugins can add CLI commands.** `ch template --dry-run` has no home
  otherwise.
- **What "requires" means beyond `env`.** Candidates: a minimum codeherd
  version, host facilities (docker, a logged-in CLI), other plugins. Each is
  checked at a different time. Decided when a concrete plugin needs one.
- **Plugin-initiated notifications.** Some kinds (event-driven task sources,
  live driver output) may need a long-lived plugin to push messages. If so,
  they will be JSON-RPC notifications, defined per kind.
- **Whether plugins can run under a sandbox** (OQ-17).

---

## 11. Alternatives considered

| Option | Why not |
|---|---|
| A `codeherd-plugin.toml` manifest next to the binary | Two sources of truth. Its only job that `describe` cannot do (how to run and build) is replaced by "one executable, no arguments" and by registration. |
| Subcommands (`describe`, `invoke`, `serve`) with exit codes 0/1/2/3 | Slightly shorter for a throwaway script, but two protocols to specify and test, with different error reporting per lifetime. |
| gRPC over a socket (go-plugin style) | Ties plugins to codeherd's protos and an SDK; rules out "any executable", Bash included. |
| Plugins as MCP stdio servers | The contract would live in tool-name conventions and `_meta` on a model-facing protocol, and would follow MCP's breaking revisions. Stock MCP servers would still need adapters. |
| Inheriting the daemon's whole environment | Leaks every variable of whichever shell started the daemon to every plugin, and makes behavior depend on that shell. |
| Forwarding variables from the latest client, as tmux's `update-environment` does | Many calls have no client, the latest of several terminals wins, and a forwarded SSH agent dies with its connection. |
| `CODEHERD_*` context in the plugin's environment | Fixed for the life of a process, so wrong for a long-lived plugin serving many calls; a daemon started inside a session would leak that session's context. |
| Secret references resolved by codeherd | Most credentials already sit behind files or commands the plugin can use directly; a resolver adds config syntax and a milestone for little gain. |
