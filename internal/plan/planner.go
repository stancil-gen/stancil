package plan

import (
	"fmt"
	"stencil/internal/spec"
	"unicode"
)

// Plan holds the universally sorted execution buckets.
type Plan struct {
	Tiers [][]*Node
}

// Build generates a topological execution plan from a ResolvedSpec.
// It creates nodes for all 12 generator types and wires their dependencies
// using Kahn's algorithm via the DAG.
func Build(s *spec.ResolvedSpec) (*Plan, error) {
	g := NewGraph()

	var allRepoIDs, allExtIDs, allHandlerIDs []string

	// Tier 0: types (no deps)
	typesID := "go.types"
	g.AddNode(&Node{ID: typesID, Generator: "go.types"})

	// Tier 0: go.mod scaffold (no deps)
	gomodID := "go.gomod"
	g.AddNode(&Node{ID: gomodID, Generator: "go.gomod"})

	// Tier 0: config (no deps)
	configID := "go.config"
	g.AddNode(&Node{ID: configID, Generator: "go.config"})

	// Tier 0: db (no deps)
	dbID := "go.db"
	g.AddNode(&Node{ID: dbID, Generator: "go.db"})

	// Tier 0: table models and errors (no deps)
	for _, obj := range s.ObjectsOfKind(spec.TableModel) {
		modelID := fmt.Sprintf("go.table.%s.model", obj.TableName)
		errorsID := fmt.Sprintf("go.table.%s.errors", obj.TableName)

		g.AddNode(&Node{ID: modelID, Generator: "go.table.model", Payload: obj})
		g.AddNode(&Node{ID: errorsID, Generator: "go.table.errors", Payload: obj})

		// Tier 1: repo depends on model
		repoID := fmt.Sprintf("go.table.%s.repo", obj.TableName)
		g.AddNode(&Node{ID: repoID, Generator: "go.table.repo", Payload: obj})
		g.AddEdge(repoID, modelID)

		allRepoIDs = append(allRepoIDs, repoID)
	}

	// Tier 1: externals (depend on types)
	for _, impl := range s.ImplsOfKind(spec.ExternalImpl) {
		extID := fmt.Sprintf("go.external.%s", planSnakeCase(impl.Name))
		g.AddNode(&Node{ID: extID, Generator: "go.external", Payload: impl})
		g.AddEdge(extID, typesID)
		allExtIDs = append(allExtIDs, extID)
	}

	// Tier 2-6: API generators per service group
	for _, impl := range s.ImplsOfKind(spec.ServiceImpl) {
		groupSlug := planSnakeCase(impl.Name)

		dtoID := fmt.Sprintf("go.api.%s.dto", groupSlug)
		ctxID := fmt.Sprintf("go.api.%s.context", groupSlug)
		hooksID := fmt.Sprintf("go.api.%s.hooks", groupSlug)
		mappersID := fmt.Sprintf("go.api.%s.mappers", groupSlug)
		svcID := fmt.Sprintf("go.api.%s.service", groupSlug)
		handlerID := fmt.Sprintf("go.api.%s.handler", groupSlug)

		g.AddNode(&Node{ID: dtoID, Generator: "go.api.dto", Payload: impl})
		g.AddNode(&Node{ID: ctxID, Generator: "go.api.context", Payload: impl})
		g.AddNode(&Node{ID: hooksID, Generator: "go.api.hooks", Payload: impl})
		g.AddNode(&Node{ID: mappersID, Generator: "go.api.mappers", Payload: impl})
		g.AddNode(&Node{ID: svcID, Generator: "go.api.service", Payload: impl})
		g.AddNode(&Node{ID: handlerID, Generator: "go.api.handler", Payload: impl})

		// Intra-group dependency chain: dto -> context -> hooks -> mappers -> service -> handler
		g.AddEdge(ctxID, dtoID)
		g.AddEdge(hooksID, ctxID)
		g.AddEdge(mappersID, hooksID)
		g.AddEdge(svcID, mappersID)
		g.AddEdge(handlerID, svcID)

		// Service depends on all repos and externals
		for _, repoID := range allRepoIDs {
			g.AddEdge(svcID, repoID)
		}
		for _, extID := range allExtIDs {
			g.AddEdge(svcID, extID)
		}

		allHandlerIDs = append(allHandlerIDs, handlerID)
	}

	// Tier 7: routes depends on all handlers
	routesID := "go.routes"
	g.AddNode(&Node{ID: routesID, Generator: "go.routes"})
	for _, hID := range allHandlerIDs {
		g.AddEdge(routesID, hID)
	}

	// Tier 8: wire depends on routes (and transitively everything else)
	wireID := "go.wire"
	g.AddNode(&Node{ID: wireID, Generator: "go.wire"})
	g.AddEdge(wireID, routesID)

	// Final tier: main scaffold depends on wire
	mainID := "go.main"
	g.AddNode(&Node{ID: mainID, Generator: "go.main"})
	g.AddEdge(mainID, wireID)

	// Final tier: hooks scaffold depends on wire
	hooksID := "go.hooks"
	g.AddNode(&Node{ID: hooksID, Generator: "go.hooks"})
	g.AddEdge(hooksID, wireID)

	tiers, err := g.Sort()
	if err != nil {
		return nil, err
	}

	return &Plan{Tiers: tiers}, nil
}

// planSnakeCase converts PascalCase to snake_case.
// Local helper to avoid circular imports with other packages.
func planSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}
