package pipeline

import (
	"fmt"

	"github.com/DerDaehne/gitops-playground/internal/feature"
)

// DAG is a directed acyclic graph of features.
type DAG struct {
	nodes map[feature.ID]feature.Feature
	edges map[feature.ID][]feature.ID // node → its dependencies
}

// Build constructs a DAG from the given features.
// Returns an error if a feature declares a dependency on an unknown feature ID.
func Build(features []feature.Feature) (*DAG, error) {
	dag := &DAG{
		nodes: make(map[feature.ID]feature.Feature),
		edges: make(map[feature.ID][]feature.ID),
	}
	for _, f := range features {
		dag.nodes[f.ID()] = f
	}
	for _, f := range features {
		for _, dep := range f.DependsOn() {
			if _, ok := dag.nodes[dep]; !ok {
				return nil, fmt.Errorf("feature %q declares unknown dependency %q", f.ID(), dep)
			}
		}
		dag.edges[f.ID()] = f.DependsOn()
	}
	return dag, nil
}

// Levels returns features grouped into topological levels.
// Features in the same level have no dependencies on each other and can run in parallel.
// Returns an error if a dependency cycle is detected.
func (d *DAG) Levels() ([][]feature.Feature, error) {
	// Kahn's algorithm
	inDegree := make(map[feature.ID]int)
	revEdges := make(map[feature.ID][]feature.ID) // dep → dependents

	for id := range d.nodes {
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
	}
	for id, deps := range d.edges {
		for _, dep := range deps {
			inDegree[id]++
			revEdges[dep] = append(revEdges[dep], id)
		}
	}

	var levels [][]feature.Feature
	var queue []feature.ID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	processed := 0
	for len(queue) > 0 {
		var level []feature.Feature
		nextQueue := []feature.ID{}
		for _, id := range queue {
			level = append(level, d.nodes[id])
			processed++
			for _, dependent := range revEdges[id] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					nextQueue = append(nextQueue, dependent)
				}
			}
		}
		levels = append(levels, level)
		queue = nextQueue
	}

	if processed != len(d.nodes) {
		return nil, fmt.Errorf("dependency cycle detected in feature graph")
	}
	return levels, nil
}
