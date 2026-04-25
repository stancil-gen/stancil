# Phase 7: Deep DAG Planner Implementation

## Background Context
With our `stencil-go` Runtime Engine built, the platform requires an **Orchestrator** to plan precisely *when* each code generation template should fire. If we evaluate templates sequentially as they appear in the AST, we risk triggering a Go import race condition (e.g. attempting to generate an API Service that imports a Table Repository which hasn't been generated yet). 

To solve this, Phase 7 defines a complete **Directed Acyclic Graph (DAG)** leveraging **Kahn’s Algorithm**.

## Proposed Architecture

A new package `internal/plan` will evaluate the unified `ResolvedSpec` and map out an explicit, battle-tested execution tree.

### 1. The Mathematical Engine: `internal/plan/dag.go`
We will design a generic, mathematically sound Graph struct capable of holding Code Generation instructions and detecting circular deadlocks.

```go
type Node struct {
    ID           string      // e.g., "go.api.CreateUser.service"
    Generator    string      // The plugin targeted (e.g., "go.template.service")
    Payload      interface{} // Passes *spec.ResolvedAPI or *spec.ResolvedTable directly
    Dependencies []string    // Explicitly what this Node waits for
}

type Graph struct {
    nodes map[string]*Node
    edges map[string][]string // Adjacency list: map[DependentNode][]PrerequisiteNodes
}
```
**Kahn's Sort Implementation:**
1. Dynamically compute the **in-degree** (number of prerequisite edges) for all nodes.
2. Filter all nodes with an `in-degree == 0` (nodes with zero dependencies) and group them into `Tier 1`.
3. Simulate the "execution" of `Tier 1` nodes by removing their outgoing edges in the graph, thereby decrementing the in-degree of downstream nodes.
4. If this process stalls but nodes are left, Kahn's definitively recognizes an infinite loop (which triggers a `Cyclic Dependency Panic`).
5. Otherwise, the function returns `[][]*Node` -> A multi-dimensional array representing parallel execution pipelines perfectly separated into execution Tiers!

### 2. The Semantic Bridge: `internal/plan/planner.go`
The Planner translates the `ResolvedSpec` into Graph instructions.

*   **Database Foundations**:
    The Planner iterates over `s.Tables`. For each table `X`:
    *   Creates Node: `go.table.X.model`. (Has Zero Dependencies. Stays naturally at Tier 1).
    *   Creates Node: `go.table.X.repo`. (Sets explicit dependency on `go.table.X.model`!).

*   **API Interactions**:
    The Planner iterates over `s.Resources` computing endpoints:
    *   Creates Nodes: `go.api.Y.context` and `go.api.Y.hooks`.
    *   Creates Node: `go.api.Y.service`.
    *   **The Crucial Link**: We examine the `Touches` array natively inside the API context! If endpoint `Y` "touches" table `X` via an action, we inject a Graph edge setting `go.api.Y.service` to depend on `go.table.X.repo`!

*   **The Initialization Boundary**:
    `go.wire` (The container registration file we discussed designing) requires all services and repositories to be built so it can generate their import paths. The Planner creates a single `go.wire` node, but iterates over literally *every* generated repo and service, adding them as dependencies. Kahn's Algorithm will securely force `go.wire` exclusively to the absolute bottom of the execution funnel!

### Verification Matrix
We will design intense unit tests in `dag_test.go` confirming:
1. `TestTopographicalSorting`: Verify Models always drop to index `[0]`, Repos index `[1]`, Services index `[2]`.
2. `TestParallelBucketing`: Confirm that Tables that do not interact with each other are bucketed into the exact same execution Tier array, proving our Emitter can confidently spawn parallel GoRoutines.
3. Add a CLI command `./stencil plan testdata/minimal.yaml` so you can visually see the DAG execution Tiers natively in your terminal.

## Let's Build it!
Does this specific algorithmic structure encompass the profound depth and robustness you require for Phase 7?
