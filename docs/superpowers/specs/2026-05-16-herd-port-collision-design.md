# Herd Template Port Collision Resolution Design

## Date
2026-05-16

## Status
Approved

## Summary
Resolve port collisions in `.herd` template rendering by (a) replacing `DeterministicPort` with a seedable `DeterministicPortWithSeed`, (b) introducing a per-render port allocator that gives each name two candidate slots (h1 with empty seed, h2 with seed `"alt"`), (c) falling back to linear probing when both candidates are taken, and (d) returning an explicit error when the port range is exhausted. The two-hash + first-fit strategy drops the practical probe-needed rate by ~700× for typical port counts.

## Motivation
`internal/herdtemplate.DeterministicPort` allocates ports via FNV-1a 32-bit hash with `% 50000 + 10000`. The hash itself rarely collides, but the modulo squeezes the 4-billion hash space into 50,000 slots, producing collisions that scale quadratically with the number of port names per `(project, branch)` pair.

A real collision was hit on `project=geomonitor`, `branch=feat/favorability-index`:

| name | FNV-1a hash | port |
|---|---|---|
| `backend` | 2943980634 | 40634 |
| `process-compose` | 1600430634 | 40634 |

Both names rendered to the same `BACKEND_PORT` / `PC_PORT` value, causing process startup conflicts at runtime.

The single-hash collision curve grows as `~1 − exp(−N²/(2M))` per (project, branch):

| N ports | single-hash P(any collision) |
|---|---|
| 10 | 0.10% |
| 50 | 2.47% |
| 100 | 9.52% |
| 200 | 33% |

At project scale (many branches × many port names), this is inevitable. A pure-function fix is impossible (pigeonhole), so the design introduces minimal per-render state plus a two-hash strategy that flattens the curve to `~N³/(3M²)`:

| N ports | two-hash E[probe needed] |
|---|---|
| 10 | ~1×10⁻⁷ |
| 50 | ~3×10⁻⁵ |
| 100 | ~1.3×10⁻⁴ (0.013%) |
| 200 | ~1×10⁻³ (0.1%) |

Roughly 700× rarer at N=100, 330× rarer at N=200. For the geomonitor case, two-hash alone resolves the `backend` / `process-compose` collision (their independent h2 values do not collide) — no probing required.

## Requirements

1. Re-rendering the same `.herd` file on the same `(project, branch)` with no template changes produces identical ports every time.
2. Different branches produce different ports for the same name (preserves the original isolation property).
3. The same port name referenced multiple times within one `Process()` call returns the same value.
4. When two distinct names hash to the same slot in one render, the algorithm picks distinct ports for both — silently. Resolution does not write to stderr or modify the rendered output.
5. When the algorithm cannot guarantee non-collision (port range exhausted), it returns an explicit error rather than silently producing duplicate ports.
6. `Service.New` and `Service.Process` keep their current signatures.
7. The public hash function exposes a seed parameter so the allocator can derive multiple candidate slots from one name, and so future code can disambiguate by providing a custom seed.

## Non-Requirements

- No backward compatibility for previously-rendered port numbers. Including the seed in the hash key unconditionally changes every port assignment by one render. A one-time port reshuffle across existing worktrees is acceptable; the user is responsible for restarting any services bound to the old ports.
- No user-facing config to pin or override port assignments. Advanced users can call `DeterministicPortWithSeed` directly from Go code; template-level seed exposure is out of scope.
- No two-pass / sorted allocation. Allocation order is the order in which `port "..."` calls are first evaluated.
- No warning emitted when a probe successfully resolves a collision. Resolution is silent (TUI-driven rendering hides stderr).
- No cuckoo hashing or other eviction schemes. First-fit two-hash with linear-probe fallback is sufficient at expected slot densities (<1% load even at N=500).

## Design

### `DeterministicPortWithSeed`

Replaces `DeterministicPort` as the single public hash function in the package:

```go
// DeterministicPortWithSeed returns a stable port in [10000, 59999] for the
// given project / branch / name / seed. Different seeds produce independent
// outputs for the same name, enabling multi-hash allocation strategies and
// manual disambiguation of collisions.
func DeterministicPortWithSeed(project, branch, name, seed string) int {
    key := project + "\x00" + branch + "\x00" + name + "\x00" + seed
    h := fnv.New32a()
    h.Write([]byte(key))
    return int(h.Sum32()%portRange) + portMin
}
```

Constants `portMin = 10000`, `portMax = 59999`, `portRange = 50000` move from the function body to package-level `const`s.

`DeterministicPort` is removed entirely — there are no external Go callers (verified by grep across the repo); the only callsite is the template `port` func inside `Process()`, which switches to the allocator.

### `portAllocator`

A new package-private type:

```go
type portAllocator struct {
    project, branch string
    byName          map[string]int  // cache: name -> assigned port
    byPort          map[int]string  // reverse: port -> name (collision detection)
}
```

Constructed once per `Process()` call via `newPortAllocator(project, branch string) *portAllocator`. State is discarded when `Process()` returns.

### `allocate(name string) (int, error)`

```
1. If name is in byName, return the cached port. (no error)
2. h1 := DeterministicPortWithSeed(project, branch, name, "")
3. If byPort[h1] is unset:
     byName[name] = h1; byPort[h1] = name; return h1, nil
4. h2 := DeterministicPortWithSeed(project, branch, name, "alt")
5. If h2 != h1 and byPort[h2] is unset:
     byName[name] = h2; byPort[h2] = name; return h2, nil
6. Linear probe from h1+1:
     p := h1
     for {
         p++; if p > portMax { p = portMin }
         if p == h1: return 0, fmt.Errorf("port allocator exhausted: no free slot in [%d, %d] for %q", portMin, portMax, name)
         if byPort[p] is unset:
             byName[name] = p; byPort[p] = name; return p, nil
     }
```

The `h2 != h1` guard in step 5 handles the edge case where the alt-seed hash happens to collide with the primary hash (probability `1/M` per name) — in that case, h2 carries no new information and we go straight to probing.

### Wiring into `Process()`

```go
alloc := newPortAllocator(ctx.Project, ctx.Branch)
funcMap := template.FuncMap{
    "port": func(name string) (int, error) {
        return alloc.allocate(name)
    },
    // env unchanged
}
```

Go's `text/template` natively supports template funcs returning `(T, error)`. When `allocate` returns an error, `tmpl.Execute` halts and surfaces the error through the existing `executing template` wrapping in `renderFile`. No new error-handling plumbing needed in `Process()`.

### What stays stable

- Re-renders with no template changes: identical ports.
- Different branches: different ports.
- Same name in multiple `.herd` files in one render: same port.

### What can shift

- Reordering lines in `.env.herd` such that a colliding name moves before/after its collision partner: the slot ownership flips.
- Adding a new `port "X"` that collides with an existing name *and* is evaluated before it: the existing name shifts.
- Renaming `.herd` files such that `filepath.Walk` order changes: ports for colliding names may flip.

All acceptable per requirement (1) — stability is only guaranteed when the template's set of port names doesn't change. With two-hash, the rate at which these shifts actually trigger drops by ~700× for typical N.

### What errors

The allocator returns an error only when every slot in `[10000, 59999]` is occupied (50,000 distinct port names allocated in one render). Practically unreachable, but explicit.

## Migration

The first time `ch template` runs on any existing worktree after this change, every port for every name will shift: the hash key now includes a trailing `"\x00<seed>"` (with `seed=""` for the primary hash, so the trailing byte is just `"\x00"`), changing every FNV-1a output. The user must restart any services bound to the old ports. This is a one-time event with no rollback path; subsequent renders are stable as before.

No automated migration helper is in scope.

## Testing

New / updated tests in `internal/herdtemplate/herdtemplate_test.go`:

1. **`TestDeterministicPortWithSeed_Idempotent`** — adapted from `TestDeterministicPort_Idempotent`. Verifies same `(project, branch, name, seed)` returns same port.
2. **`TestDeterministicPortWithSeed_InRange`** — port is within `[10000, 59999]`.
3. **`TestDeterministicPortWithSeed_DifferentSeeds`** — same name with different seeds produces different ports (probabilistically; the test picks a seed pair known to differ).
4. **`TestDeterministicPortWithSeed_NullByteSeparation`** — preserves the original test's intent that `(ab, cd, x, "")` ≠ `(a, bcd, x, "")` ≠ `(a, b, cdx, "")` etc.
5. **`TestProcess_PortCollision_ResolvedByAltHash`** — uses a known h1-colliding pair under the new key format: `project=testproj`, `branch=testbranch`, names `svc295` and `svc758` both have h1=58792, with distinct h2 values (13363 and 35555). Renders a template containing both `{{ port "svc295" }}` and `{{ port "svc758" }}` in that order. Asserts the first name renders to 58792 (h1 path) and the second renders to 35555 (h2 path, since its h1 collided). Demonstrates the two-hash fallback resolves a real collision without ever entering the linear-probe code path.
6. **`TestProcess_PortFunction_StableAcrossFiles`** — two `.herd` files in the same render both reference `port "shared"`. Asserts both rendered outputs contain the same port value.
7. **`TestPortAllocator_FallsBackToProbe`** — direct unit test. Engineer a setup where both h1 and h2 for a chosen name are pre-occupied by other names. Assert `allocate` returns a probed slot distinct from h1 and h2.
8. **`TestPortAllocator_Exhausted_Errors`** — direct unit test. Pre-fill `byPort` with every value in `[10000, 59999]`. Call `allocate("anything")`. Assert it returns a non-nil error.
9. **`TestPortAllocator_ProbeWraps`** — direct unit test exercising the wrap branch of the linear probe. Pick a name and compute `h1` and `h2`. Choose the free slot as `freeSlot = ((h1 - portMin - 1 + portRange) % portRange) + portMin` (i.e. the slot immediately *behind* `h1`, wrapping if `h1 == portMin`). If `h2 == freeSlot`, pick a different name. Pre-fill every port in `[portMin, portMax]` with sentinel entries except `freeSlot`. Call `allocate(name)` and assert it returns `freeSlot` — proving the probe wrapped past `portMax` (or all the way around) to find the only hole.

Run `make check` (coverage ≥ 80%, integration, lint, build) before marking work complete.

## Public API impact

- **Removed:** `DeterministicPort(project, branch, name string) int`.
- **Added:** `DeterministicPortWithSeed(project, branch, name, seed string) int`.
- `Service.New` and `Service.Process` signatures unchanged.
- `portAllocator` is unexported.

Since there are no external Go callers of `DeterministicPort` in this repo, no downstream code needs updating. Any future external consumer would migrate by passing `""` as the seed.
