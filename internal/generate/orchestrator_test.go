package generate

import (
	"errors"
	"stencil/internal/emit"
	"stencil/internal/plan"
	"stencil/internal/spec"
	"testing"
)

type MockGenerator struct {
	id          string
	shouldError bool
}

func (m *MockGenerator) ID() string {
	return m.id
}

func (m *MockGenerator) Generate(ctx GeneratorContext) ([]emitter.File, error) {
	if m.shouldError {
		return nil, errors.New("simulated template render panic")
	}
	return []emitter.File{{Path: m.id + ".txt", Content: []byte("content")}}, nil
}

func TestOrchestrator_ParallelSuccess(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&MockGenerator{id: "mock.success", shouldError: false})

	emit := emitter.NewEmitter("testdata/out", ".", "hash123")
	orch := NewOrchestrator(reg, emit)

	dagPlan := &plan.Plan{
		Tiers: [][]*plan.Node{
			{
				{ID: "node1", Generator: "mock.success"},
				{ID: "node2", Generator: "mock.success"},
			},
			{
				{ID: "node3", Generator: "mock.success"},
			},
		},
	}

	err := orch.Run(&spec.ResolvedSpec{}, dagPlan)
	if err != nil {
		t.Fatalf("Unexpected parallel failure: %v", err)
	}

	// Assert explicitly that the Orchestrator forwarded the successful files into the Staged buffer properly
	// We expect at least 3 files from the nodes, plus library files embedded from lib/FS
	if len(emit.Staged) < 3 {
		t.Errorf("Expected at least 3 aggregated blueprint outputs, got %d", len(emit.Staged))
	}
}

func TestOrchestrator_ParallelFailureHandling(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&MockGenerator{id: "mock.success", shouldError: false})
	reg.Register(&MockGenerator{id: "mock.fail", shouldError: true})

	emit := emitter.NewEmitter("testdata/out", ".", "hash123")
	orch := NewOrchestrator(reg, emit)

	dagPlan := &plan.Plan{
		Tiers: [][]*plan.Node{
			{
				{ID: "node1", Generator: "mock.success"},
				{ID: "node2", Generator: "mock.fail"}, // This one should trip the overall errgroup exclusively
			},
		},
	}

	err := orch.Run(&spec.ResolvedSpec{}, dagPlan)
	if err == nil {
		t.Fatalf("Expected errgroup wait evaluation mapping to strictly catch the singular parallel error!")
	}
}

func TestOrchestrator_UnregisteredTrap(t *testing.T) {
	reg := NewRegistry()
	
	emit := emitter.NewEmitter("testdata/out", ".", "hash123")
	orch := NewOrchestrator(reg, emit)

	dagPlan := &plan.Plan{
		Tiers: [][]*plan.Node{
			{{ID: "node1", Generator: "missing.plugin"}},
		},
	}

	err := orch.Run(&spec.ResolvedSpec{}, dagPlan)
	if err == nil {
		t.Errorf("Expected explicitly thrown unregistered plugin trap logic returning cleanly")
	}
}
