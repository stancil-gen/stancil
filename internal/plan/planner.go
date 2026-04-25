package plan

import (
	"fmt"
	"stencil/internal/spec"
)

// Plan holds the universally sorted execution buckets.
type Plan struct {
	Tiers [][]*Node
}

// Build generates a topological execution plan from a ResolvedSpec.
// NOTE: This planner is a Phase 14 migration target. It currently reads the
// new Objects/Interfaces/Implementations slices from ResolvedSpec.
func Build(s *spec.ResolvedSpec) (*Plan, error) {
	g := NewGraph()

	wireID := "go.wire"
	g.AddNode(&Node{ID: wireID, Generator: "go.wire", Payload: nil})

	var genericDependencies []string

	// 1. Table models and repo interfaces
	for _, obj := range s.ObjectsOfKind(spec.TableModel) {
		modID := fmt.Sprintf("go.table.%s.model", obj.TableName)
		repoID := fmt.Sprintf("go.table.%s.repo", obj.TableName)

		g.AddNode(&Node{ID: modID, Generator: "go.table.model", Payload: obj})
		g.AddNode(&Node{ID: repoID, Generator: "go.table.repo", Payload: obj})
		g.AddEdge(repoID, modID)
		genericDependencies = append(genericDependencies, repoID)
	}

	// 2. Service implementations (one per ResourceGroup)
	for _, impl := range s.ImplsOfKind(spec.ServiceImpl) {
		svcID := fmt.Sprintf("go.api.%s.service", impl.Name)
		hooksID := fmt.Sprintf("go.api.%s.hooks", impl.Name)
		handlerID := fmt.Sprintf("go.api.%s.handler", impl.Name)

		g.AddNode(&Node{ID: svcID, Generator: "go.api.service", Payload: impl})
		g.AddNode(&Node{ID: hooksID, Generator: "go.api.hooks", Payload: impl})
		g.AddNode(&Node{ID: handlerID, Generator: "go.api.handler", Payload: impl})

		g.AddEdge(svcID, hooksID)
		g.AddEdge(handlerID, svcID)

		// Wire service deps to its repo dependencies
		for _, dep := range impl.Dependencies {
			for _, tableObj := range s.ObjectsOfKind(spec.TableModel) {
				repoID := fmt.Sprintf("go.table.%s.repo", tableObj.TableName)
				_ = dep // simplified: add all table repos as deps
				g.AddEdge(svcID, repoID)
				break
			}
		}

		genericDependencies = append(genericDependencies, svcID, handlerID)
	}

	// 3. Wire DI
	for _, depID := range genericDependencies {
		g.AddEdge(wireID, depID)
	}

	tiers, err := g.Sort()
	if err != nil {
		return nil, err
	}

	return &Plan{Tiers: tiers}, nil
}
