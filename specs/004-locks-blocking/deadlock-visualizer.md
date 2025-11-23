You basically already have 80% of what you need sitting in your `pev` clone – you just need to swap “plan tree” for “wait-for graph” and change the data source from `EXPLAIN` JSON to deadlock info. 

Let me sketch a concrete, Go-only design that would give you a deadlock visualizer at *gocmdpev* quality or better.

---

## 1. Overall architecture

Think in the same layers as your `pev.Visualize`:

1. **Input**

   * Source: Postgres logs (`deadlock detected` blocks), or a custom JSON you emit from `pg_locks + pg_stat_activity`.([PostgreSQL Wiki][1])
   * Your Steep snippet already shows a *normalized* summary. I’d formalize *that* as the input to the visualizer.

2. **Model** – A tiny in-memory wait-for graph:

   * Nodes = backends (PIDs).
   * Edges = “backend A is waiting on a lock held by backend B”.
   * Lock structs for type/mode/relation.

3. **Analysis**

   * Detect cycles / strongly connected components.
   * Figure out victim(s), longest-waiting backend, “root cause” (first locker).

4. **View**

   * ASCII art for the wait-for cycle, using the same style primitives as gocmdpev (colors, prefixes, tags, wordwrap).
   * Per-backend breakdown like gocmdpev’s node detail panel.

5. **Integration**

   * Wire it into Steep as a “deadlock details” pane, with j/k scrolling etc.

---

## 2. Data model for a deadlock event

Start by defining a model that’s *independent* of Postgres’s raw format:

```go
type LockType string
type LockMode string

type Lock struct {
    Type     LockType // "relation", "transaction", "tuple", ...
    Mode     LockMode // "ShareLock", "ExclusiveLock", ...
    Relation string   // "schema.table" or regclass
    Key      string   // optional: tuple/block key
}

type Backend struct {
    PID         int
    User        string
    Application string
    Client      string
    BackendStart time.Time

    CurrentQuery string
    WaitingOn    *Lock // lock this backend is waiting for (if any)
    Holding      []Lock
    IsVictim     bool
}

type Edge struct {
    FromPID int   // waiter
    ToPID   int   // holder
    Lock    Lock
}

type DeadlockEvent struct {
    ID        int64
    DetectedAt time.Time
    Database   string

    ResolvedByPID int    // the backend Postgres chose as victim
    Backend       []Backend
    Edges         []Edge

    // Optional meta
    LogLines []string
}
```

Then you implement **one or more parsers** to produce this type:

* `ParseDeadlockFromLogs(r io.Reader) ([]DeadlockEvent, error)`
* `ParseDeadlockFromJSON(r io.Reader) ([]DeadlockEvent, error)` (if you decide to log JSON yourself)

Keep your visualizer oblivious to where it came from – same as `pev.Visualize` being oblivious to `EXPLAIN` provenance.

---

## 3. Graph construction & analysis (Go-only)

Use a tiny graph helper (or just raw slices) plus maybe `gonum/graph` for SCC if you don’t want to hand-roll:

```go
type WaitForGraph struct {
    Nodes map[int]*Backend // PID -> Backend
    Edges []Edge
}

func BuildWaitForGraph(ev DeadlockEvent) *WaitForGraph {
    m := make(map[int]*Backend, len(ev.Backend))
    for i := range ev.Backend {
        b := ev.Backend[i]
        m[b.PID] = &b
    }
    return &WaitForGraph{
        Nodes: m,
        Edges: ev.Edges,
    }
}
```

Cycle detection: these graphs are tiny (2–10 nodes normally), so you can just do DFS or Tarjan’s SCC yourself:

```go
// Returns each cycle as ordered list of PIDs
func (g *WaitForGraph) Cycles() [][]int {
    // simple DFS-based cycle finder; with small N you don't need anything fancy
}
```

Tag the interesting nodes:

* `IsVictim` via `ResolvedByPID`.
* “Root” locker = node with **no incoming edge** in the SCC.
* “Most blocking” = node with highest out-degree.

Those tags can surface as gocmdpev-style badges (`[victim]`, `[root]`, `[heavy blocker]`, `[waits longest]`).

---

## 4. Reuse your gocmdpev style engine

Your `pev` fork already has:

* Color helpers: `prefixFormat`, `boldFormat`, `mutedFormat`, etc.
* Word wrapping via `mitchellh/go-wordwrap`.
* Tree prefixes and joints (`├`, `└`, `│`, `─⌠`).
* A “write the whole thing with a `writer io.Writer, width uint`” API. 

Leverage that hard, so the deadlock page feels like a sibling of your EXPLAIN view.

### Example public API

```go
package deadviz

func VisualizeDeadlock(w io.Writer, ev DeadlockEvent, width uint) error {
    g := BuildWaitForGraph(ev)
    cycles := g.Cycles()
    // analysis, tagging, then rendering...
    writeDeadlock(w, ev, g, cycles, width)
    return nil
}
```

Signature mirrors `pev.Visualize`, making it feel “first class” in your codebase.

---

## 5. ASCII layout for deadlocks

You don’t need a full graph layout engine; these graphs are tiny and always cyclic. A couple of patterns goes a long way.

### 5.1 2-backend deadlock

Special-case the most common case with a **horizontal diagram**:

```text
   ┌─────────────┐       waits for       ┌─────────────┐
   │ PID 5755    │ ────────────────────▶ │ PID 5758    │
   │ ShareLock   │                       │ ShareLock   │
   │ test_dead.. │ ◀──────────────────── │ test_dead.. │
   └─────────────┘       waits for       └─────────────┘
          ▲                                   ▲
          │              deadlock             │
          └───────────────────────────────────┘
```

Rendered with your existing prefix machinery and `fatih/color` for emphasis:

* Victim backend in red, others normal.
* Lock relation/row in cyan (`outputFormat`).

Because you control width, you can abbreviate: “test_deadlock_5748” → “test_dead…” at narrow widths.

### 5.2 N-backend cycles

For 3+ nodes, you can do a simple **ring or list layout**:

Ring-ish:

```text
   ┌───────┐        ┌───────┐        ┌───────┐
   │ 5755  │ ─────▶ │ 5758  │ ─────▶ │ 5762  │
   └───┬───┘        └───┬───┘        └───┬───┘
       └────────────────┴───────────────▶│
                     waits for           │
                                         └── back to 5755 (cycle)
```

Or a vertical “wait chain”, annotated as “CYCLE #1”:

```text
CYCLE #1 (3 backends)

┌─────────┐
│ PID 5755│ waits for ShareLock on test_deadlock_5748 held by PID 5758
└─────────┘
    │
    ▼
┌─────────┐
│ PID 5758│ waits for ShareLock on test_deadlock_5748 held by PID 5762
└─────────┘
    │
    ▼
┌─────────┐
│ PID 5762│ waits for ShareLock on test_deadlock_5748 held by PID 5755 [victim]
└─────────┘
    ▲
    └─────────────── back to start (deadlock)
```

This is easy to generate with the same prefix mechanics as `writePlan`, just with a different layout rule.

---

## 6. Deadlock “detail panel” (like gocmdpev nodes)

Under the cycle diagram, do a **per-backend breakdown** reusing your plan-node styling:

```text
○ Backends Involved: 2
○ Database: brandon
○ Resolved by: PID 5758 [victim]

┬
│ PID 5755 [waiter]
│   User:        brandon
│   Application: psql
│   Client:      local
│   Lock:        ShareLock on test_deadlock_5748
│   Blocked by:  PID 5758
│   Query:
│     UPDATE test_deadlock_5748
│     SET data = 'session1_1'
│     WHERE id = 2;
│
└ PID 5758 [victim, waiter]
    User:        brandon
    Application: psql
    Client:      local
    Lock:        ShareLock on test_deadlock_5748
    Blocked by:  PID 5755
    Query:
      UPDATE test_deadlock_5748
      SET data = 'session2_1'
      WHERE id = 1;
```

You can lift:

* bullet formatting (`○`) from `writeExplain`.
* prefix logic from `writePlan` for `┬`, `│`, `└`.

Also: use `wordwrap.WrapString(sql, width)` so even long queries look sane in narrow terminals – exactly what you do for plan descriptions. 

---

## 7. Features that *exceed* gocmdpev, specifically for deadlocks

This is where you beat “just a pretty picture”:

1. **Lock-graph explanation section**

   * “Lock chain summary”:

     ```text
     Analysis:
       • 5755 and 5758 both hold ShareLock on test_deadlock_5748.
       • 5755 waits on row id=2; 5758 waits on row id=1.
       • Deadlock caused by inconsistent row access order (1 → 2 vs 2 → 1).
     ```

   Detect “classic pattern” examples by looking at WHERE predicates or tuple keys.([Stack Overflow][2])

2. **Application hints**

   * “Suggested fix” bullets:

     * “Ensure both transactions update rows in ascending primary key order.”
     * “Keep transactions shorter; both backends were active for > N seconds before deadlock.”

3. **Historical ranking**

   * If Steep already has multiple deadlock events, show counts per query fingerprint:

     * “This statement participated in 5 deadlocks in the last 24h.”
   * That’s a big upgrade over “single event pretty print”.

4. **EXPLAIN integration**

   * For each backend’s query, offer a key to open your `pev.Visualize` view:

     * e.g. press `e` on a backend to run `EXPLAIN (ANALYZE, ...)` against that query and show the plan using your existing code.

   That bridges “deadlock debugging” and “performance tuning” nicely.

---

## 8. Implementation sketch for `writeDeadlock`

Something along these lines:

```go
func writeDeadlock(w io.Writer, ev DeadlockEvent, g *WaitForGraph, cycles [][]int, width uint) {
    fmt.Fprintf(w, "Deadlock Event #%d\n", ev.ID)
    fmt.Fprintf(w, "Detected:    %s\n", ev.DetectedAt.Format(time.RFC3339))
    fmt.Fprintf(w, "Database:    %s\n", ev.Database)
    fmt.Fprintf(w, "Resolved by: PID %d\n", ev.ResolvedByPID)
    fmt.Fprintln(w)

    // 1) Summary
    fmt.Fprintf(w, "Backends Involved: %d\n\n", len(ev.Backend))

    // 2) Cycle diagram(s)
    for i, cycle := range cycles {
        fmt.Fprintf(w, "Cycle #%d:\n", i+1)
        renderCycleDiagram(w, ev, g, cycle, width)
        fmt.Fprintln(w)
    }

    // 3) Per-backend detail section (tree-like style)
    fmt.Fprintf(w, "%s\n", prefixFormat("┬"))
    for i, b := range ev.Backend {
        last := i == len(ev.Backend)-1
        writeBackend(w, &b, width, last)
    }

    // 4) Optional analysis / hints
    writeAnalysis(w, ev, g, cycles, width)
}
```

`writeBackend` can look a *lot* like your `writePlan`, just with different fields.

---

## 9. TUI layer (Bubble Tea / Lip Gloss, still all Go)

Since your Steep output already has `[j/k]scroll`, I’m guessing you’re in Bubble Tea land. If not, it’s a good fit:

* Use **Bubble Tea** for key handling and paging.
* Use **Lip Gloss** for border styles, titles, colors, and you call into `deadviz.Visualize` to render into a `strings.Builder` inside your `View()`.

That way, the “visualizer” is just a pure function that writes ASCII; your TUI handles scroll, focus, keybindings, etc.

---

[1]: https://wiki.postgresql.org/wiki/Lock_Monitoring?utm_source=chatgpt.com "Lock Monitoring"
[2]: https://stackoverflow.com/questions/10245560/deadlocks-in-postgresql-when-running-update?utm_source=chatgpt.com "Deadlocks in PostgreSQL when running UPDATE"
