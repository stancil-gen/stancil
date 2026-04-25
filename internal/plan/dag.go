package plan

import (
	"fmt"
)

// Node represents a discrete Generation Task waiting for evaluation.
type Node struct {
	ID           string
	Generator    string      // The stencil plugin targeted (e.g. "go.table.repo")
	Payload      interface{} // Passes the resolved domain struct directly 
	Dependencies []string
}

// Graph is our Directed Acyclic Graph enforcing Kahn's structural sort.
type Graph struct {
	nodes map[string]*Node
	edges map[string][]string // A -> B (A depends on B)
}

func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string]*Node),
		edges: make(map[string][]string),
	}
}

// AddNode pushes a generic generation task into the unstructured pool.
func (g *Graph) AddNode(node *Node) {
	g.nodes[node.ID] = node
}

// AddEdge declares a strict Topographical constraint: `from` must NEVER execute before `to`.
func (g *Graph) AddEdge(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

// Sort evaluates Kahn's algorithm returning 2-Dimensional execution buckets.
// If any mathematical overlaps occur, it returns a precise Cyclic anomaly error.
func (g *Graph) Sort() ([][]*Node, error) {
	inDegree := make(map[string]int)
	adjList := make(map[string][]string) // B -> A (if A depends on B, B unlocks A upon completion)

	// 1. Initialize table counts
	for id := range g.nodes {
		inDegree[id] = 0
	}

	// 2. Compute in-degrees and reverse unlock adjacency
	for from, edges := range g.edges {
		for _, to := range edges {
			adjList[to] = append(adjList[to], from)
			inDegree[from]++
		}
	}

	var tiers [][]*Node
	var queue []string

	// 3. Funnel Tier 1 (Nodes possessing Zero Dependencies)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	processedCount := 0

	// 4. Cascade resolutions downstream
	for len(queue) > 0 {
		var currentTier []*Node
		var nextQueue []string

		for _, id := range queue {
			currentTier = append(currentTier, g.nodes[id])
			processedCount++

			// Decrement the prerequisites of anything downstream
			for _, unlocked := range adjList[id] {
				inDegree[unlocked]--
				if inDegree[unlocked] == 0 {
					nextQueue = append(nextQueue, unlocked)
				}
			}
		}

		tiers = append(tiers, currentTier)
		queue = nextQueue
	}

	// 5. Detect anomalies
	if processedCount != len(g.nodes) {
		return nil, fmt.Errorf("fatal cyclic dependency anomaly detected within DAG planner! %d nodes evaluated but grid expected %d", processedCount, len(g.nodes))
	}

	return tiers, nil
}
