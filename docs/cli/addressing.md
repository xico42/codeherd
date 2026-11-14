# CLI addressing and the API

How the `ch` CLI should address resources and run writes once it becomes a
client of `codeherd.v1alpha1`. Background: [the API conventions](../api/conventions.md).

**Status: Proposal.** Today's CLI does not work this way yet. It will be adapted
to the API, and details here may change while that happens. The caveats at the
end describe how the current commands differ.

---

## Addressing a resource

The API addresses resources by ID only. The CLI accepts three forms and turns
each into a full ID before calling the API.

| Input | Example | How it resolves |
|---|---|---|
| Full ID | `ws_3f9a1c7e2b844d0a9c17e5b2f8a6d310` | used as is, no lookup |
| ID prefix (4+ hex characters after the kind) | `ws_3f9a1c` | `Resolve`, among live resources |
| Name | `feat-x` | `Resolve`, scoped by `-n` and a parent flag such as `--repo` |

- **Live resources only.** Prefixes and names match live resources by default.
  History needs a full ID (or a later opt-in flag).
- **Ambiguity is an error.** When several resources match, the CLI lists the
  candidates and stops. It never guesses.
- **The kind prefix makes the kind clear**, so `ch describe ws_3f9a1c` can work
  without naming the kind.
- **Inside a session**, a missing address defaults to the current workspace,
  session or namespace from `CODEHERD_WORKSPACE`, `CODEHERD_SESSION` and
  `CODEHERD_NAMESPACE`. Relative paths are never resolved.

```
$ ch describe workspace ws_3f9a1c
$ ch describe workspace feat-x --repo saas/app
$ ch describe workspace              # inside a session: the current one
```

## Tables

Every NAME and ID cell must paste straight into the next command.

```
$ ch get workspaces -n personal
ID             NAME        REPO       BRANCH      SESSIONS      AGE
ws_3f9a1c7e2b  feat-x      saas/app   feat/x      agent,shell   2d
ws_a07d5e19c3  main        saas/app   main        shell         41d
```

- IDs show the kind prefix plus the first 10 hex characters. The CLI lengthens
  them only when two live resources would clash.
- `-A` lists every namespace and adds a NAMESPACE column.
- The CLI fetches every page by default, like kubectl's `--chunk-size`.
- `-l` takes kubectl's label selector syntax (`status=review,mode in (afk,hitl)`)
  and sends it as a structured selector.
- `-w` subscribes from a snapshot and keeps the table current, like
  `kubectl get -w`.
- `-o wide` adds infrastructure columns (worktree, path). `-o json` and
  `-o yaml` print full IDs.

## Writes

Every write is asynchronous in the API. The CLI hides that by default.

```
$ ch create workspace feat-x --repo saas/app --branch feat/x
op_4e1c9a7b2d
  cloning saas/app
  adding worktree for feat/x
created workspace ws_3f9a1c7e2b (feat-x)

$ ch create workspace feat-y --repo saas/app --branch feat/y --no-wait
op_9d02b6e81f

$ ch wait op_9d02b6e81f
$ ch get operations
```

- **The CLI waits by default** and prints progress. It follows the operation
  through `Subscribe` from the earliest retained event and closes the stream
  at DONE. `ch wait` does the same. `--no-wait` prints the
  operation ID and returns, for scripts and agents that fan out work.
- **Ctrl-C stops waiting. It never cancels.** The operation keeps running in
  the daemon.
- **Output comes from the operation result,** not from a follow-up read, so it
  never shows stale state.
- **Each invocation generates its own idempotency key.** Retrying the same
  command is a new request; the backend's best-effort idempotency covers it.
- **Multi-resource commands send one batch.** `ch create session --repo
  saas/app --branch feat/x` sends a workspace item and a session item that
  points at it with `$ref:`.
- **`--cascade` replaces `--force`** on deletes. `--force` stays as an alias
  for now.

### Workspaces

Users say *what* to work on. The backend decides where.

```
$ ch create workspace --repo saas/app                          # the default branch
$ ch create workspace --repo saas/app --branch feat/x          # new or existing branch
$ ch create workspace --repo saas/app --branch feat/x --from v1.2
$ ch create workspace --repo saas/app --branch feat-x --from origin/feat-x   # tracks it
$ ch create workspace --repo saas/app --branch spike/a --from .              # from the current workspace
```

`--from .` expands to `ch:$CODEHERD_WORKSPACE`. `--from ws_3f9a1c` is resolved
as a prefix and sent as `ch:<full id>`.

### Batches

A batch file lists write requests in order. Quote `$ref:` locators: in double
quotes or an unquoted heredoc, the shell expands `$ref` to nothing.

```
$ ch apply -f new-feature.yaml
$ ch apply -f - <<'EOF'
items:
  - ref: ws
    create_workspace: { workspace: { metadata: { parent: "ch:repo_8c1d..." }, spec: { branch: "feat/x" } } }
  - create_session:   { session: { metadata: { parent: '$ref:ws' }, spec: { type: SHELL } } }
EOF
```

## Errors

- The CLI turns `Error.reason` into a hint; the API never sends hints.
  Example: `LIVE_CHILDREN` becomes "use --cascade to stop them first".
- `-o json` prints the API's outcome as is, on success and on failure, so
  agents read one shape.

```
$ ch delete workspace ws_3f9a1c
error: workspace ws_3f9a1c7e2b has live sessions: ses_7be04f1a9d, ses_c2d81e7f05
       (use --cascade to stop them first)
```

---

## Caveats: today's CLI

- **Nouns.** The CLI says *project* where the API says *repo*, and its
  `worktree` commands manage what the API calls workspaces. The renames come
  later.
- **Two-positional addressing.** `ch create session <project> <branch>` and
  the other two-positional forms stay as deprecated aliases for a while.
- **Hooks.** The pre/post clone, worktree, session, copy and template hooks
  will be removed (see the API conventions, section 13).
- **Precedent.** codeherd addresses by ID, like Docker and AWS, not by name
  like Kubernetes. Kubernetes UIDs are cluster-unique UUIDs that are never used
  as addresses. codeherd's names are optional and mutable, so they cannot serve
  as addresses.
