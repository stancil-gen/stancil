package plan

import (
	"testing"
)

func TestGraph_SortSuccess(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "Model"})
	g.AddNode(&Node{ID: "Repo"})
	g.AddNode(&Node{ID: "Service"})
	g.AddNode(&Node{ID: "Wire"})

	g.AddEdge("Repo", "Model")
	g.AddEdge("Service", "Repo")
	g.AddEdge("Wire", "Service")
	g.AddEdge("Wire", "Repo") // Redundant safety edge evaluated cleanly

	tiers, err := g.Sort()
	if err != nil {
		t.Fatalf("Unexpected error sorting DAG: %v", err)
	}

	// Should explicitly split into Model -> Repo -> Service -> Wire
	if len(tiers) != 4 {
		t.Fatalf("Expected 4 distinct topological tiers, got %d", len(tiers))
	}

	if tiers[0][0].ID != "Model" {
		t.Errorf("Tier 1 strictly expected Model")
	}
	if tiers[1][0].ID != "Repo" {
		t.Errorf("Tier 2 strictly expected Repo")
	}
	if tiers[2][0].ID != "Service" {
		t.Errorf("Tier 3 strictly expected Service")
	}
	if tiers[3][0].ID != "Wire" {
		t.Errorf("Tier 4 strictly expected Wire")
	}
}

func TestGraph_ParallelEquivalence(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "ModelA"})
	g.AddNode(&Node{ID: "ModelB"})
	g.AddNode(&Node{ID: "RepoA"})
	g.AddNode(&Node{ID: "RepoB"})

	g.AddEdge("RepoA", "ModelA")
	g.AddEdge("RepoB", "ModelB")

	tiers, err := g.Sort()
	if err != nil {
		t.Fatalf("Unexpected algorithm failure: %v", err)
	}

	if len(tiers) != 2 {
		t.Fatalf("Expected exactly 2 topographical sequence buckets, got %d", len(tiers))
	}
	if len(tiers[0]) != 2 || len(tiers[1]) != 2 {
		t.Errorf("Expected 2 parallel generation operations bucketed cleanly per tier")
	}
}

func TestGraph_CyclicPanicSafety(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "A"})
	g.AddNode(&Node{ID: "B"})
	g.AddNode(&Node{ID: "C"})

	// Force an infinite loop trap
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("C", "A")

	_, err := g.Sort()
	if err == nil {
		t.Fatalf("Expected mathematical cyclic anomaly to trip protective error!")
	}
}
