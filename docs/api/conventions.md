# codeherd API conventions (`codeherd.v1alpha1`)

These conventions apply to every RPC in `codeherd.v1alpha1` and to every plugin
service built on its core types.

Each section carries a status:

- **Settled**: decided. Change it only by reopening the decision.
- **Proposal**: a direction agreed in principle, to be confirmed or revised
  when the first RPC that needs it is implemented.

Questions not yet decided are not recorded here.

Errors have their own document: [errors.md](errors.md). CLI addressing is in
[../cli/addressing.md](../cli/addressing.md).

---

## 1. Glossary

**Status: Settled.**

| Term | Meaning |
|---|---|
| **Namespace** | A flat tenant. Every namespaced resource lives in exactly one. Namespaces do not nest. |
| **Repo** | A git repository declared in a profile. Keyed by its URL. |
| **Workspace** | A directory you attach work to: agents, shells, later runs. The unit users create and the identity anchor of their work. Always backed by exactly one worktree, which the backend chooses. Outlives its sessions. |
| **Worktree** | A git working directory: a dedicated worktree, a pool slot, or the repo's main checkout. Infrastructure. Read-only through the API. |
| **Session** | A tmux process of type `agent` or `shell`. Belongs to one workspace or to none. |
| **Operation** | The asynchronous execution of a write. See section 7. |
| **ID** | A resource's identity. Minted by the server, permanent, never reused. Bare: `ws_3f9a1c7e2b844d0a9c17e5b2f8a6d310`. |
| **Name** | An optional, mutable label for display and lookup. Unique within its parent when set. Persisted. |
| **Locator** | A string that points at something: `<scheme>:<value>`. See section 4. |
| **Ref (codeherd)** | A request-local label a batch create item declares for the resource it will create. Referenced as `$ref:<ref>`. Not a git ref. |
| **Git ref** | A git reference (`feat/x`, `origin/main`, `v1.2`). Appears only in git-facing fields such as `WorkspaceSpec.from`. |
| **Outcome** | The application-level result block in every response envelope. Not the resource `status`, not `google.rpc.Status`. |

The word "workspace" means the codeherd resource. The word "status" means the
observed half of a resource (section 3), never the result of a call.

---

## 2. Vocabulary and versioning

**Status: Settled.**

- The proto speaks the target vocabulary: namespace, repo, workspace, worktree,
  session, agent. The CLI keeps its current nouns (project, worktree) until it
  adopts the API vocabulary.
- The package is `codeherd.v1alpha1`. It may change incompatibly until it is
  frozen as `codeherd.v1`. From then on, `buf breaking` guards it and changes
  within a major version are additive.
- Field numbers are the wire format. Removed fields are `reserved`, never
  reused.
- Each RPC has its own `XxxRequest` and `XxxResponse` messages.

---

## 3. Resources

**Status: Settled.**

Every stored resource has three parts:

```proto
message Workspace {
  ObjectMeta metadata = 1;
  WorkspaceSpec spec = 2;      // intent: what the client wants
  WorkspaceStatus status = 3;  // observation: what the backend sees
}

message ObjectMeta {
  string id = 1;                          // output only; ws_..., minted by the server
  string namespace = 2;                   // empty for cluster-scoped kinds
  string parent = 3;                      // locator; immutable containment (see below)
  string name = 4;                        // optional
  string resource_version = 5;            // output only
  map<string, string> labels = 6;
  google.protobuf.Timestamp create_time = 7;  // output only
  google.protobuf.Timestamp delete_time = 8;  // output only; set on tombstones
  // annotations: added when first needed
}
```

- **Spec is intent, status is observation.** Clients write spec. Only the
  backend writes status.
- **Read-only projections stay flat.** Things that are not stored resources
  (`RemoteBranch`, `Progress`, the `Operation` record) have no
  metadata/spec/status split.
- **`metadata.parent` means immutable containment.** It scopes names, never
  changes, and bounds the child's lifetime: deleting a parent deletes or blocks
  on its children. Containment today:
  - workspace and worktree under repo;
  - session under workspace, or under nothing;
  - artifact under task.
- **Relationships that can change live in spec or status**, never in `parent`.
  A task's parent task is `spec.parent_task`. A workspace's worktree is
  `status.worktree`.
- **A delete leaves a tombstone.** The record stays with `delete_time` set.
  `Get` by full ID returns it. `List`, `Subscribe` snapshots and `Resolve`
  leave tombstones out, except `Resolve` with `include_history`.
- **Clients never set output-only fields.** A create that sets `metadata.id`
  fails with reason `OUTPUT_ONLY_FIELD`.

### 3.1 The workspace

**Status: Settled.**

codeherd is workspace-first. To start anything, a user creates a workspace and
says *what* to work on, never *where*. The backend picks the worktree: a new
one, a pool slot, or the main checkout. Placement shows up in
`status.worktree` and may change over the workspace's life.

```proto
message WorkspaceSpec {
  string branch = 1;  // local branch; empty = the repo's default branch
  string from = 2;    // git commit-ish or a locator; used only if the branch does not exist yet
}
```

The backend applies `git switch` semantics:

1. The local branch exists: use it. `from` is ignored.
2. It does not exist, and `from` is `<remote>/<same name>` (or `from` is empty
   and exactly one remote has the branch): create it tracking that remote
   branch. This matches git's `branch.autoSetupMerge=simple`.
3. Otherwise: create a new branch from `from`, without tracking.

`from` also accepts a locator. `ch:ws_...` means that workspace's current HEAD
commit, read by the backend when the item runs. Uncommitted changes never come
along.

| Intent | Spec |
|---|---|
| Work on the default branch | `{}` |
| New branch | `{branch: "feat/x"}` |
| New branch from a tag or SHA | `{branch: "feat/x", from: "v1.2"}` |
| Branch off another branch | `{branch: "spike/a", from: "feat/x"}` |
| Continue a remote branch | `{branch: "feat-x", from: "origin/feat-x"}` |
| Branch off a workspace | `{branch: "spike/a", from: "ch:ws_3f9a..."}` |

Git allows a branch in only one worktree at a time, so there is at most one
workspace per (repo, local branch).

Not supported: tracking under a different local name. Set the upstream in git
if you need it.

### 3.2 Worktrees and repos

**Status: Settled.**

- Worktrees are read-only resources (`wt_`). Clients can list them and subscribe to them.
  Only the backend creates, reuses, prunes and deletes them.
- Cloning is infrastructure. The backend clones a repo when a workspace needs
  it. There is no `auto_clone` request flag.
- Remote branches are a flat projection under a repo, read with
  `ListRemoteBranches(repo, refresh)`. `refresh` fetches first. A fetch updates
  remote-tracking refs only, so it counts as a cache refresh inside a read.

---

## 4. Identity, names and locators

### 4.1 IDs

**Status: Settled.**

- An ID is a kind prefix, `_`, and a UUIDv4 without dashes:
  `ws_3f9a1c7e2b844d0a9c17e5b2f8a6d310`.
- The prefix is part of the ID everywhere: store, wire, JSON, environment
  variables.
- IDs are globally unique without coordination, so nodes can mint them
  independently. Order comes from `create_time`, not from the ID.
- Every kind has IDs, declared kinds (repos, agents from TOML) included.
- Consequences:
  - renaming a declared TOML key creates a new ID, except for repos, which are
    keyed by URL;
  - rebuilding the store gives declared kinds new IDs.

### 4.2 The prefix registry

**Status: Proposal.** Confirm each prefix when the milestone that introduces
its kind is refined.

| Prefix | Kind | Prefix | Kind |
|---|---|---|---|
| `ns` | namespace | `tsk` | task |
| `node` | node | `art` | artifact |
| `repo` | repo | `run` | run |
| `ws` | workspace | `drv` | driver |
| `wt` | worktree | `sbx` | sandbox |
| `ses` | session | `exe` | executor |
| `agt` | agent | `ini` | init |
| `skl` | skill | `op` | operation |
| `plg` | plugin | `pli` | plugin instance |

Rules: 2 to 4 lowercase letters. A prefix is never renamed or reused. An unknown
or mismatched prefix fails validation with reason `INVALID_ID`. The control
plane and events have no prefix yet.

### 4.3 Names

**Status: Settled.**

- Names are optional for every kind.
- A kind may default one: a workspace from its branch, a session from its type,
  a task from its tracker key (`STAFF-475`, `xico42/codeherd#27`).
- When set, a name is unique within its parent. Names may change and may
  contain `/` and `#`.
- Names exist for display and lookup only. No RPC other than `Resolve`
  accepts one.
- Task sources handle tracker-key collisions themselves, for example by
  qualifying the key or importing the task unnamed with an annotation.

### 4.4 Locators

**Status: Settled.**

Every field that points at something holds a locator string,
`<scheme>:<value>`. There is no `ResourceRef` message.

| Scheme | Points at | Where valid |
|---|---|---|
| `ch:<full id>` | a stored codeherd resource | everywhere |
| `$ref:<ref>` | the resource an earlier item of the same request creates | inside batches only |
| foreign (`jira:`, `gh:`, ...) | something outside codeherd | only where a field's documentation allows it |

Rules:

- **Clients treat locators as opaque.** They pass them on and display them.
  Only the server parses them, so the grammar can grow on the server alone.
- **ID fields hold bare IDs; locator fields always carry a scheme.**
  `metadata.id: "ws_3f9a..."`, but `metadata.parent: "ch:repo_8c1d..."`.
- **Schemes starting with `$` are request-scoped.** The server resolves them
  during the request. It never stores or returns them.
- **Locators are not URIs.** `$ref` is not a valid URI scheme. Do not parse
  locators with a URI library.
- **No git ref can be mistaken for a locator.** `:` is illegal in git ref
  names. Fields that accept both (`WorkspaceSpec.from`) reject git's `:/`
  search syntax.
- **The wire takes full IDs only.** Prefix matching is a CLI feature
  (section 5).
- **Shell quoting.** In double quotes or an unquoted heredoc, `$ref` expands
  to an empty string. The server rejects a locator with an empty scheme
  (`INVALID_LOCATOR`). Quote examples with single quotes or `<<'EOF'`.

---

## 5. Addressing

**Status: Settled.**

- **Requests use exact locators.** Get, update and delete take `ch:<id>`.
- **Creates carry the object.** `metadata.namespace` scopes it, and
  `metadata.parent` holds the parent's locator.
- **Lists and `Subscribe` take a scope and selectors** (section 10):

  ```proto
  message Scope {
    oneof namespaces {
      string namespace = 1;
      bool all_namespaces = 2;
    }
  }

  message Selector {
    string kind = 1;                    // "workspace"; empty = any kind
    string id = 2;                      // bare ID: exactly this object
    string parent = 3;                  // locator: direct children only
    map<string, string> labels = 4;     // every key must equal its value
    repeated LabelRequirement label_requirements = 5;
  }

  message LabelRequirement {
    string key = 1;
    Operator operator = 2;
    repeated string values = 3;         // IN and NOT_IN only

    enum Operator {
      OPERATOR_UNSPECIFIED = 0;
      OPERATOR_IN = 1;
      OPERATOR_NOT_IN = 2;              // also matches objects without the key
      OPERATOR_EXISTS = 3;
      OPERATOR_DOES_NOT_EXIST = 4;
    }
  }
  ```

  - Every field a selector sets must match. Unset fields match anything.
  - Label semantics are Kubernetes': `NOT_IN` and `DOES_NOT_EXIST` match an
    object that lacks the key.
  - A List takes one selector. Its `kind` is empty or the List's own kind;
    any other kind fails validation.
  - The CLI parses kubectl's `-l` syntax (`status=review,mode in (afk,hitl)`)
    into a selector. The API never parses selector strings.

- **Cluster-scoped kinds** (namespace, node, plugin) leave `namespace` empty.
  Creating a namespace takes the new namespace as the object, not as a scope.
- **`Resolve` turns typed input into candidates:**

  ```proto
  rpc Resolve(ResolveRequest) returns (ResolveResponse);
  message ResolveRequest {
    string kind = 1;            // "workspace"; empty = any kind
    string query = 2;           // an ID prefix (4+ chars) or a name
    string namespace = 3;
    string parent = 4;          // optional locator
    bool include_history = 5;   // default: live resources only
  }
  // ResolveResponse: 0, 1 or many matches.
  ```

  Only clients with typed input need it: the CLI. Limoni, other UIs, plugins
  and agents hold full IDs and skip it.
- **Inside sessions,** codeherd injects `CODEHERD_NAMESPACE`,
  `CODEHERD_WORKSPACE` and `CODEHERD_SESSION` (bare IDs). A client may default a
  missing address from them. Relative paths are never resolved.

### 5.1 Pagination

**Status: Settled.**

Every List RPC pages its results, `ListOperations` and `ListRemoteBranches`
included. The envelope stays `{outcome, result}`: a list's result is a list
message that carries the page token.

```proto
message ListWorkspacesRequest {
  Scope scope = 1;
  Selector selector = 2;
  int32 page_size = 3;    // 0 = server default
  string page_token = 4;  // from the previous page; empty = first page
}

message ListWorkspacesResponse {
  Outcome outcome = 1;
  WorkspaceList result = 2;
}

message WorkspaceList {
  repeated Workspace items = 1;
  string next_page_token = 2;  // empty = no more pages
}
```

- **Page size.** `0` means the server default, 100. The maximum is 1000;
  larger values are clamped, not rejected. A negative value fails validation.
- **Short pages.** A page may hold fewer items than asked, even none, and still
  carry a token. Clients stop only when `next_page_token` is empty.
- **Tokens are opaque** and bound to the request that produced them. A token
  sent with different scope or filters, or one that has expired, fails with
  `INVALID_PAGE_TOKEN`; the client starts over.
- **Order is stable.** Stored resources and operations are ordered by
  `create_time`, then ID. Projections without a `create_time` order by their
  natural key (`ListRemoteBranches`: branch name). A resource that exists for
  the whole iteration appears exactly once; one created or deleted during it
  may or may not appear.
- **No total count.** Responses do not carry `total_size`.
- **Per-call options apply to the first page only.** `ListRemoteBranches`
  fetches when `refresh` is set on the first page; later pages read the same
  snapshot.
- **Streams do not page.** `Subscribe`'s snapshot arrives on its stream.
  `Resolve` does not page either: it exists to disambiguate.

This follows AIP-158, with one change: the token travels inside `result`, so
the envelope keeps one shape.

---

## 6. Response envelope

**Status: Settled.**

Two carriers report failure, split by who failed:

- **The gRPC status** reports transport and infrastructure failures, where no
  handler ran or understood the request:

  | Code | When |
  |---|---|
  | `UNAVAILABLE` | the daemon is down or restarting |
  | `DEADLINE_EXCEEDED` | the client deadline passed |
  | `CANCELLED` | the client cancelled the call |
  | `PERMISSION_DENIED` | the peer UID check failed |
  | `UNIMPLEMENTED` | version skew: the server lacks the RPC |
  | `RESOURCE_EXHAUSTED`, `INTERNAL` | codec errors |
  | `INTERNAL` | bugs and recovered panics |

- **The envelope** reports everything else. If a handler ran and understood the
  request, the outcome goes in the envelope, and the gRPC status is `OK`.
  Request validation goes in the envelope too.

Every response has the same shape:

```proto
message GetWorkspaceResponse {
  Outcome outcome = 1;
  Workspace result = 2;
}

message Outcome {
  OutcomeStatus status = 1;
  Error error = 2;               // see errors.md
  repeated Warning warnings = 3;
}

enum OutcomeStatus {
  OUTCOME_STATUS_UNSPECIFIED = 0;
  OUTCOME_STATUS_OK = 1;
  OUTCOME_STATUS_FAILED = 2;
  OUTCOME_STATUS_PARTIAL = 3;    // result and error both set
  OUTCOME_STATUS_SKIPPED = 4;    // batch items only: an earlier item stopped the batch
}
```

- `Outcome` avoids confusion with resource `status` and `google.rpc.Status`.
- In Go, a generic `unwrap[T]` helper reads any response through its
  `GetOutcome()` and `GetResult()` getters.
- A server interceptor logs the application outcome, so observability does not
  depend on the gRPC code.
- The API carries no CLI hints. The CLI maps reasons to hints.

This departs from AIP-193, which puts application errors in the gRPC status.
Reason: one decoder for single calls, batch items and operation results.

---

## 7. Writes are asynchronous

**Status: Settled.**

Reads (`Get`, `List`, `Subscribe`, `Resolve`, `ListRemoteBranches`) answer
synchronously. Every write is asynchronous: the server accepts it, runs it
later, and reports through an operation.

### 7.1 Submitting

Each write has its own RPC (`CreateWorkspace`, `DeleteSession`, ...). There is
also a `Batch` RPC. All of them return the same response:

```proto
message SubmitResponse {
  Outcome outcome = 1;   // acceptance only
  Operation result = 2;  // set when accepted
}
```

- **At submit time** the server runs only static checks: fields, ID and
  locator formats, references, namespace. A rejection is synchronous and
  nothing is queued.
- **Everything else** happens during execution and is reported on the
  operation: git and tmux failures, conflicts, stale versions.
- **Every write carries an `idempotency_key`.** A retry with the same key and
  the same request returns the original operation. The same key with a
  different request fails with `IDEMPOTENCY_KEY_REUSED`.
- **Accepted means durable.** The server records the operation before it
  answers.

### 7.2 The operation

```proto
message Operation {
  ObjectMeta metadata = 1;     // id: op_..., namespace, create_time
  OperationState state = 2;
  OperationResult result = 3;  // set when DONE
}

enum OperationState {
  OPERATION_STATE_UNSPECIFIED = 0;
  OPERATION_STATE_PENDING = 1;  // accepted, queued
  OPERATION_STATE_RUNNING = 2;
  OPERATION_STATE_DONE = 3;     // terminal; result.outcome says how it ended
}

message OperationResult {
  Outcome outcome = 1;          // overall: OK, FAILED or PARTIAL
  repeated ItemResult items = 2;  // items[i] answers request item i
}

message ItemResult {
  Outcome outcome = 1;          // OK, FAILED or SKIPPED
  oneof result {
    Workspace workspace = 2;
    Session session = 3;
    DeletedResources deleted = 4;
    // more core kinds, numbered below 100
    google.protobuf.Any extension = 100;  // plugin-defined results
  }
}
```

- **A single write is an operation with one item.** A batch is an operation
  with many. Both end with the same `OperationResult`.
- **The operation is a flat record.** No spec/status split, no method name, no
  list of targets, no step list.
- **Results carry the written state.** Clients that need it read it from the
  result, not from a follow-up `Get` (section 9).

### 7.3 Following an operation

```proto
service OperationService {
  rpc GetOperation(GetOperationRequest) returns (GetOperationResponse);
  rpc ListOperations(ListOperationsRequest) returns (ListOperationsResponse);
}
```

- **Clients follow operations through `Subscribe`** (section 10). There is no
  `WatchOperation` or `WaitOperation`.
- **To follow one operation**, a client subscribes with
  `selectors: [{id: "op_..."}]` and `start: START_EARLIEST`. It receives every
  retained change and progress event of the operation in order, then live
  ones, and closes the stream itself once the operation is DONE. A client that
  attaches late sees the whole history.
- **To wait without progress**, a client calls `GetOperation` first and
  subscribes only if the operation is not DONE yet.
- `Subscribe` carries every operation in scope, so Limoni sees operations
  started by the CLI or by agents.
- Operations run independently of the client connection. A client that
  disconnects does not stop its operation.

### 7.4 Progress

```proto
message Progress {
  string operation = 1;   // "ch:op_4e1c..."
  int32 item = 2;         // position in the request's items
  string message = 3;     // "cloning saas/app"
  google.protobuf.Timestamp time = 4;
}
```

- Progress is free-form text for people. No client parses it. Clients act on
  `state` and `result` only, so handlers and plugins may reword messages
  freely.
- Progress travels on `Subscribe` as an event whose subject is its operation.
  It is not stored on the `Operation` record.
- `START_EARLIEST` replays past progress, so a client that attaches late sees
  the whole history.
- Steps are not part of the model.

### 7.5 Retention

Finished operations stay visible for 7 days by default (configurable). The
record, its progress log and its idempotency key expire together. PENDING and
RUNNING operations never expire. A lookup after expiry returns `NOT_FOUND`.

### 7.6 Plugins

Plugin services import the core protos and reuse `SubmitResponse`, `Operation`,
`Outcome`, `Error` and `ObjectMeta`. A plugin's typed result goes in
`ItemResult.extension`. The protos live in `proto/codeherd/v1alpha1/`. The
generated Go code lives in `api/` in the main module
(`github.com/xico42/codeherd/api/codeherd/v1alpha1`). It moves to its own
module when a plugin needs to import it without the CLI, tmux or git packages.

---

## 8. Batches

**Status: Settled.**

A batch is one operation with one idempotency key. It holds an ordered list of
write requests. The backend applies them in order, each with best-effort
idempotency.

```proto
message BatchRequest {
  string namespace = 1;
  string idempotency_key = 2;
  bool continue_on_failure = 3;  // default false
  repeated BatchItem items = 4;
}

message BatchItem {
  string ref = 1;  // optional; create items only
  oneof request {
    CreateWorkspaceRequest create_workspace = 2;
    DeleteWorkspaceRequest delete_workspace = 3;
    CreateSessionRequest create_session = 4;
    DeleteSessionRequest delete_session = 5;
    // one field per core write
  }
}
```

Example: a workspace with an agent and a shell.

```yaml
items:
  - ref: ws
    create_workspace:
      workspace:
        metadata: { parent: "ch:repo_8c1d..." }
        spec: { branch: "feat/x" }
  - ref: agent
    create_session:
      session:
        metadata: { parent: "$ref:ws" }
        spec: { type: AGENT, agent: "claude", prompt: "..." }
  - create_session:
      session:
        metadata: { parent: "$ref:ws" }
        spec: { type: SHELL }
```

Rules:

- **Order.** Items run one at a time, in request order. `result.items[i]`
  answers `items[i]`, and the counts always match. The response never echoes
  the request.
- **Failure.** By default the first failing item stops the batch: the outcome
  is `PARTIAL` (or `FAILED` if nothing succeeded) and the remaining items are
  `SKIPPED`. Nothing is rolled back; every intermediate state is valid. With
  `continue_on_failure`, every item runs.
- **Static validation first.** The server validates every item before running
  any. A static failure rejects the whole batch at submit time, with reason
  `BATCH_INVALID` and a cause per bad field.
- **Refs.** `ref` is optional, matches `[a-z][a-z0-9_-]{0,31}`, is unique within
  the request, and exists only for that request. Only create items may declare
  one. A `$ref:` locator must point to an earlier item of a compatible kind.
  Forward references and unknown refs fail static validation.
- **The daemon finishes the batch** even if the client disconnects.

This departs from AIP-23x, which prefers atomic batches. External side effects
(git, tmux) cannot be rolled back. Firestore's `BatchWrite` is the precedent for
non-atomic batches.

---

## 9. Consistency and idempotency

**Status: Settled.**

- **Consistency is eventual.** A read right after a write may not show it.
  Clients that need the written state take it from the operation result.
- **Write requests state intent, not commands.** They describe the desired
  result, which leaves the backend free to converge.
- **Writes are best-effort idempotent.** The mechanism is the backend's
  business and not part of the contract. Possible techniques: checking for the
  result before acting, stamping IDs into external artifacts (tmux session
  names), creating atomically (clone to a temporary directory, then rename),
  recording each item's result as it finishes.
- **Clients never read before writing,** except to resolve names a user typed.
- **Known gap.** An agent that started with its initial prompt and exited
  before the daemon recorded the start may run twice after a restart.
- **Restarts.** An accepted operation runs to DONE, even across daemon
  restarts. Whether a RUNNING operation re-runs from its first item or resumes
  from the interrupted one is the backend's business, not contract; best-effort
  idempotency makes either safe.

---

## 10. Subscribe

**Status: Settled.**

`Subscribe` is the one streaming read. It carries resource changes, operation
changes and progress for everything a client selects. There are no per-kind
`Watch` RPCs.

```proto
rpc Subscribe(SubscribeRequest) returns (stream SubscribeResponse);

message SubscribeRequest {
  Scope scope = 1;
  repeated Selector selectors = 2;  // any one may match; empty = everything in scope
  Start start = 3;
}

enum Start {
  START_UNSPECIFIED = 0;  // = START_SNAPSHOT
  START_SNAPSHOT = 1;     // current state, snapshot_end, then live events
  START_LATEST = 2;       // live events only
  START_EARLIEST = 3;     // every retained event in order, then live events
}
```

### 10.1 Selecting

- `Scope` and `Selector` are the ones Lists use (section 5).
- **Fields within a selector must all match. Several selectors are
  alternatives:** an event matches when any one of them does.
- **Every change and progress event has a subject**, a `ch:` locator.
  A change's subject is the object that changed. A progress event's subject is
  its operation. Control events (`snapshot_end`, heartbeats, `missed`) have
  none.
- **Selectors match the subject only.** Selecting a workspace does not bring
  in its sessions; add a selector with `parent` for them.

### 10.2 Starting

Clients hold no cursor. They pick where the stream starts, as with Kafka's
`auto.offset.reset` or a JetStream deliver policy.

- **`START_SNAPSHOT`, the default.** The current state of every matching live
  object, then `snapshot_end`, then live events. This is the state view:
  Limoni and `ch get -w` use it.
- **`START_LATEST`.** Live events only.
- **`START_EARLIEST`.** Every retained event that matches, in order, then live
  events. This is history, not state: `ch create` and `ch wait` use it to
  follow an operation and its progress.
- **Event sequence numbers stay inside the daemon.** No client sees or sends
  one.
- **Clients persist nothing.** A client that reconnects subscribes again with
  `START_SNAPSHOT` and replaces its whole view. Replayed progress on reconnect
  is acceptable.

### 10.3 Falling behind

The daemon never blocks on a slow subscriber. A subscriber that falls too far
behind receives `missed`, followed by a fresh snapshot and `snapshot_end`. It
replaces its whole view, as on reconnect.

### 10.4 Deletions

- **Live and with `START_EARLIEST`,** a delete arrives as an explicit `DELETED`
  change that carries the object's last state.
- **In a snapshot,** a deleted object is absent. Clients replace their view at
  `snapshot_end`, so absence is the deletion signal.
- Tombstones (section 3) are never part of a snapshot.

### 10.5 The event

**Status: Proposal.**

```proto
message SubscribeResponse {
  Outcome outcome = 1;  // a rejected request sends one FAILED message, then the stream ends
  Event result = 2;
}

message Event {
  google.protobuf.Timestamp time = 1;
  string subject = 2;  // locator; empty on control events
  oneof event {
    Change change = 3;
    Progress progress = 4;
    SnapshotEnd snapshot_end = 5;
    Heartbeat heartbeat = 6;
    Missed missed = 7;
  }
}

message Change {
  ChangeType type = 1;
  Object object = 2;  // the object after the change; DELETED: its last state
}

enum ChangeType {
  CHANGE_TYPE_UNSPECIFIED = 0;
  CHANGE_TYPE_SNAPSHOT = 1;  // part of a snapshot
  CHANGE_TYPE_CREATED = 2;
  CHANGE_TYPE_UPDATED = 3;
  CHANGE_TYPE_DELETED = 4;
}

message Object {
  oneof kind {
    Repo repo = 1;
    Workspace workspace = 2;
    Worktree worktree = 3;
    Session session = 4;
    Operation operation = 5;
    // more core kinds, numbered below 100
    google.protobuf.Any extension = 100;  // plugin-defined kinds
  }
}

message SnapshotEnd {}
message Heartbeat {}

message Missed {
  Reason reason = 1;
  enum Reason {
    REASON_UNSPECIFIED = 0;
    REASON_LAGGING = 1;  // the subscriber fell too far behind
  }
}
```

---

## 11. Cross-resource rules

**Status: Settled.**

- **Deletes cascade down containment, on the server.**

  ```proto
  message DeleteOptions {
    Propagation propagation = 1;  // UNSPECIFIED = REFUSE
    string resource_version = 2;  // optional precondition
  }
  enum Propagation {
    PROPAGATION_UNSPECIFIED = 0;
    PROPAGATION_REFUSE = 1;   // fail with LIVE_CHILDREN if live children exist
    PROPAGATION_CASCADE = 2;  // delete children deepest-first, then the object
  }
  ```

  There is no orphan policy: containment rules it out.
- **Other multi-resource flows are composed by the client** from single writes,
  usually as a batch. There are no composite RPCs.

---

## 12. Decisions recorded here

**Status: Settled.**

- **Hooks are removed.** The pre/post clone, worktree, session, copy and
  template hooks go away. User-defined shell code cuts across async execution
  and idempotency, so hooks will be redesigned as plugin hooks rather than
  carried over. This removal applies to codeherd's config hooks only.
  Agent-side hooks (for example Claude Code's own hooks, used for identity and
  resume) are a different thing.
- **codeherd does not send input to running agents.** An agent may start with
  an initial prompt, given in its `CreateSession` request.
- **The daemon has a store from its first release.** Server-minted IDs and
  durable operations need records from day one.
- **Storage:** SQLite is the daemon's store and source of truth. A write and
  its event commit in one transaction, and each node numbers its events with
  its own `seq`.

### Departures from common practice

| Practice | codeherd | Why |
|---|---|---|
| Kubernetes addresses by name | addresses by ID, Docker/AWS style | Names are optional and mutable |
| AIP-193: application errors in the gRPC status | envelope | One decoder everywhere |
| AIP-23x: atomic batches | ordered, non-atomic | External side effects cannot be rolled back |
| AIP-122: structured resource names | opaque locators with schemes | IDs are global; one string form for all pointers |
