package build

import (
	"fmt"
	"sort"
	"strings"
)

// Graph is a validated DAG of PR-stack nodes. It is the deterministic substrate
// the orchestrator walks: spec-cli owns the graph, pi conducts traversal.
type Graph struct {
	// Nodes are stored in their original plan order.
	Nodes []PRStep
	byID  map[string]PRStep
	// deps maps a node ID to the IDs of the nodes it depends on.
	deps map[string][]string
	// dependents maps a node ID to the IDs of nodes that depend on it.
	dependents map[string][]string
}

// BuildGraph validates a PR stack and returns its DAG. It rejects edges that
// reference unknown steps and dependency cycles, naming the offending nodes so
// the author can fix the §7.3 plan.
func BuildGraph(steps []PRStep) (*Graph, error) {
	g := &Graph{
		Nodes:      append([]PRStep(nil), steps...),
		byID:       make(map[string]PRStep, len(steps)),
		deps:       make(map[string][]string, len(steps)),
		dependents: make(map[string][]string, len(steps)),
	}

	byNumber := make(map[int]string, len(steps))
	for _, s := range steps {
		id := s.NodeID()
		if _, dup := g.byID[id]; dup {
			return nil, fmt.Errorf("duplicate node id %q in PR stack — give step %d a unique id", id, s.Number)
		}
		g.byID[id] = s
		byNumber[s.Number] = id
	}

	for _, s := range steps {
		id := s.NodeID()
		for _, depNum := range s.DependsOn {
			depID, ok := byNumber[depNum]
			if !ok {
				return nil, fmt.Errorf(
					"step %d (%s) depends on unknown step %d — fix the (after: ...) edge in \u00a77.3",
					s.Number, id, depNum)
			}
			if depID == id {
				return nil, fmt.Errorf("step %d (%s) depends on itself — remove the self edge in \u00a77.3", s.Number, id)
			}
			g.deps[id] = append(g.deps[id], depID)
			g.dependents[depID] = append(g.dependents[depID], id)
		}
	}

	if cycle := g.findCycle(); cycle != nil {
		return nil, fmt.Errorf(
			"dependency cycle detected in PR stack: %s — break the cycle in \u00a77.3 (after: ...) edges",
			strings.Join(cycle, " \u2192 "))
	}

	return g, nil
}

// Node returns the step for a node ID and whether it exists.
func (g *Graph) Node(id string) (PRStep, bool) {
	s, ok := g.byID[id]
	return s, ok
}

// Dependencies returns the node IDs the given node depends on.
func (g *Graph) Dependencies(id string) []string {
	return append([]string(nil), g.deps[id]...)
}

// Leaves returns the nodes no other node depends on — the tips of the stack,
// which are the PRs that must exist before review.
func (g *Graph) Leaves() []PRStep {
	var leaves []PRStep
	for _, n := range g.Nodes {
		if len(g.dependents[n.NodeID()]) == 0 {
			leaves = append(leaves, n)
		}
	}
	return leaves
}

// Waves returns a Kahn topological sort grouped into waves. Every node in a
// wave has all of its dependencies satisfied by earlier waves, so a wave can be
// fanned out in parallel. Plan order is preserved within each wave.
func (g *Graph) Waves() [][]PRStep {
	indegree := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		indegree[n.NodeID()] = len(g.deps[n.NodeID()])
	}

	remaining := len(g.Nodes)
	var waves [][]PRStep
	for remaining > 0 {
		var wave []PRStep
		for _, n := range g.Nodes {
			if indegree[n.NodeID()] == 0 {
				wave = append(wave, n)
			}
		}
		if len(wave) == 0 {
			// Should never happen on a validated DAG, but guard against a
			// silent infinite loop.
			break
		}
		for _, n := range wave {
			id := n.NodeID()
			indegree[id] = -1 // mark emitted
			for _, dependent := range g.dependents[id] {
				if indegree[dependent] > 0 {
					indegree[dependent]--
				}
			}
		}
		waves = append(waves, wave)
		remaining -= len(wave)
	}
	return waves
}

// ReadySet returns the nodes whose dependencies are all complete and which are
// not themselves complete. This is the resume primitive: given the set of
// completed node IDs, it yields exactly the nodes safe to dispatch next.
func (g *Graph) ReadySet(done map[string]bool) []PRStep {
	var ready []PRStep
	for _, n := range g.Nodes {
		id := n.NodeID()
		if done[id] {
			continue
		}
		if g.depsComplete(id, done) {
			ready = append(ready, n)
		}
	}
	return ready
}

// depsComplete reports whether every dependency of a node is in the done set.
func (g *Graph) depsComplete(id string, done map[string]bool) bool {
	for _, dep := range g.deps[id] {
		if !done[dep] {
			return false
		}
	}
	return true
}

// findCycle returns a node-ID path describing a dependency cycle, or nil if the
// graph is acyclic. It uses DFS with a colour marking (white/grey/black).
func (g *Graph) findCycle() []string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[string]int, len(g.Nodes))

	// Deterministic iteration order for stable error messages.
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.NodeID())
	}
	sort.Strings(ids)

	var stack []string
	var visit func(id string) []string
	visit = func(id string) []string {
		color[id] = grey
		stack = append(stack, id)
		for _, dep := range g.deps[id] {
			switch color[dep] {
			case grey:
				return append(cycleFrom(stack, dep), dep)
			case white:
				if cyc := visit(dep); cyc != nil {
					return cyc
				}
			}
		}
		color[id] = black
		stack = stack[:len(stack)-1]
		return nil
	}

	for _, id := range ids {
		if color[id] == white {
			if cyc := visit(id); cyc != nil {
				return cyc
			}
		}
	}
	return nil
}

// cycleFrom returns the slice of the stack starting at the first occurrence of
// start, used to render a readable cycle path.
func cycleFrom(stack []string, start string) []string {
	for i, id := range stack {
		if id == start {
			return append([]string(nil), stack[i:]...)
		}
	}
	return append([]string(nil), stack...)
}
