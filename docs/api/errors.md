# codeherd API errors

How `codeherd.v1alpha1` reports application errors, and the catalog of reasons.
For when an error uses the envelope rather than the gRPC status, see
[conventions.md, section 6](conventions.md#6-response-envelope).

**Status:** the structure is settled. The catalog is open: add each reason when
an RPC first needs it.

---

## Structure

```proto
message Error {
  google.rpc.Code code = 1;                  // the category: NOT_FOUND, FAILED_PRECONDITION, ...
  string reason = 2;                         // the stable contract: UPPER_SNAKE
  string domain = 3;                         // "codeherd.dev", or the plugin's name
  string message = 4;                        // for people; never parsed
  repeated Cause causes = 5;
  map<string, string> metadata = 6;          // per-reason facts
  repeated google.protobuf.Any details = 7;  // escape hatch
}

message Cause {
  string type = 1;           // how the subject relates to the error: UPPER_SNAKE
  oneof subject {            // what the cause points at; may be unset
    string locator = 2;      // "ch:ses_7be0..."
    string field = 3;        // request field path: "workspace.metadata.parent"
  }
  string description = 4;    // for people
}

message Warning {
  string reason = 1;         // UPPER_SNAKE
  string message = 2;
  repeated Cause causes = 3;
}
```

### Fields

- **`code`** gives the broad category, with gRPC code semantics. Generic
  clients branch on it.
- **`reason` with `domain`** is what clients match on. A reason never changes
  meaning. Plugins use their own domain, so their reasons cannot collide with
  core ones.
- **`message`** is for people. Wording may change at any time.
- **`causes`** list what the error is about and how each thing relates to it.
  A bare list of IDs would say *which* resources are involved but not *how*.
- **`metadata`** holds the facts a reason documents (a command, its stderr, a
  conflicting value). Keys are lower_snake.
- **`details`** is for typed payloads that fit nowhere else. Prefer causes and
  metadata.

### Cause types

Cause types form one small vocabulary that all reasons share. A client can act
on them without knowing the reason: stop every `LIVE_CHILD`, fix every
`INVALID_FIELD`, inspect every `HELD_BY`.

| Type | Subject | Meaning |
|---|---|---|
| `INVALID_FIELD` | field | the value is malformed or out of range |
| `REQUIRED_FIELD` | field | the field is missing |
| `CONFLICTING_FIELD` | field | the value collides with existing state |
| `HELD_BY` | locator | this resource holds what the request wanted |
| `LIVE_CHILD` | locator | this live child blocks the request |
| `MISSING_REFERENCE` | locator or field | the referenced resource does not exist |
| `GIT_FAILED` | none | a git command failed; facts in metadata |

Add a type only when no existing one describes the relationship.

### No CLI hints

The API never says what to type next. The CLI maps reasons to hints, for
example `LIVE_CHILDREN` to "use --cascade to stop them first".

---

## Examples

`LIVE_CHILDREN`, from deleting a workspace with running sessions:

```json
{
  "code": "FAILED_PRECONDITION",
  "reason": "LIVE_CHILDREN",
  "domain": "codeherd.dev",
  "message": "workspace ws_3f9a1c7e2b has live sessions",
  "causes": [
    { "type": "LIVE_CHILD", "locator": "ch:ses_7be04f1a9d264b1e8c0a5f3d2e9b7a16", "description": "session agent is running" },
    { "type": "LIVE_CHILD", "locator": "ch:ses_c2d81e7f05b34a9d9e1c6b0f8a2d4e57", "description": "session shell is running" }
  ]
}
```

A validation failure, with no resource involved:

```json
{
  "code": "INVALID_ARGUMENT",
  "reason": "BATCH_INVALID",
  "domain": "codeherd.dev",
  "message": "batch item 1 is invalid",
  "causes": [
    { "type": "INVALID_FIELD", "field": "items[1].create_session.session.metadata.parent", "description": "$ref:wss is not declared by an earlier item" }
  ]
}
```

A git failure, with the facts in metadata:

```json
{
  "code": "FAILED_PRECONDITION",
  "reason": "GIT_FAILED",
  "domain": "codeherd.dev",
  "message": "git worktree add failed",
  "causes": [ { "type": "GIT_FAILED", "description": "git exited 128" } ],
  "metadata": { "command": "git worktree add ...", "stderr": "fatal: invalid reference: feat/y" }
}
```

---

## Catalog

Each entry lists its code, when it happens, the cause types
it may carry, and its metadata keys. Entries marked *draft* came up during
design and still need confirming when their RPC is implemented.

### Submit and validation

| Reason | Code | When | Causes | Metadata |
|---|---|---|---|---|
| `INVALID_ID` (draft) | `INVALID_ARGUMENT` | an ID is malformed, or its prefix is unknown or wrong for the field | `INVALID_FIELD` | |
| `INVALID_LOCATOR` (draft) | `INVALID_ARGUMENT` | a locator is malformed, has an empty or unknown scheme, or uses a scheme the field does not allow | `INVALID_FIELD` | |
| `OUTPUT_ONLY_FIELD` (draft) | `INVALID_ARGUMENT` | a request sets an output-only field such as `metadata.id` | `INVALID_FIELD` | |
| `UNKNOWN_REFERENCE` (draft) | `NOT_FOUND` | a locator points at nothing | `MISSING_REFERENCE` | |
| `BATCH_INVALID` (draft) | `INVALID_ARGUMENT` | static validation of a batch failed; nothing ran | `INVALID_FIELD`, `REQUIRED_FIELD`, `MISSING_REFERENCE` | |
| `INVALID_PAGE_TOKEN` (draft) | `INVALID_ARGUMENT` | the page token is malformed, expired, or was issued for a different request | `INVALID_FIELD` | |
| `IDEMPOTENCY_KEY_REUSED` (draft) | `FAILED_PRECONDITION` | the key belongs to a different request | `CONFLICTING_FIELD` | `operation` |

### Execution

| Reason | Code | When | Causes | Metadata |
|---|---|---|---|---|
| `LIVE_CHILDREN` (draft) | `FAILED_PRECONDITION` | a REFUSE delete found live children | `LIVE_CHILD` | |
| `NAME_TAKEN` (draft) | `ALREADY_EXISTS` | the name is in use within the parent | `HELD_BY`, `CONFLICTING_FIELD` | |
| `BRANCH_CONFLICT` (draft) | `FAILED_PRECONDITION` | the branch is held by another workspace, or exists with a different upstream | `HELD_BY`, `CONFLICTING_FIELD` | `branch` |
| `AGENT_NOT_FOUND` (draft) | `NOT_FOUND` | the named agent is not configured | `MISSING_REFERENCE` | `agent` |
| `AGENT_COMMAND_NOT_FOUND` (draft) | `FAILED_PRECONDITION` | the agent's command is not on the PATH | | `command` |
| `GIT_FAILED` (draft) | `FAILED_PRECONDITION` | a git command failed | `GIT_FAILED` | `command`, `stderr` |
| `BATCH_STOPPED` (draft) | `ABORTED` | an item failed and stopped the batch; top-level error of a PARTIAL batch | | `failed_item` |
| `INTERRUPTED` (draft) | `ABORTED` | a daemon restart interrupted an item the backend could not safely resume | | `item` |
