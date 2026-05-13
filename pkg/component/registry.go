package component

import (
	"context"
	"fmt"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	components  map[string]Component
	byType      map[ComponentType][]Component
	defaults    map[ComponentType]string
	initialized bool
	statuses    map[string]ComponentStatus
}

func NewRegistry() *Registry {
	return &Registry{
		components: make(map[string]Component),
		byType:     make(map[ComponentType][]Component),
		defaults:   make(map[ComponentType]string),
		statuses:   make(map[string]ComponentStatus),
	}
}

func (r *Registry) Register(c Component) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := c.GetName()
	if _, exists := r.components[name]; exists {
		return fmt.Errorf("%w: component name %s", ErrComponentAlreadyExists, name)
	}

	r.components[name] = c
	r.byType[c.GetType()] = append(r.byType[c.GetType()], c)

	if _, hasDefault := r.defaults[c.GetType()]; !hasDefault {
		r.defaults[c.GetType()] = name
	}

	return nil
}

func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	comp, exists := r.components[name]
	if !exists {
		return fmt.Errorf("%w: component name %s", ErrComponentNotFound, name)
	}

	delete(r.components, name)

	compType := comp.GetType()
	components := r.byType[compType]
	for i, c := range components {
		if c.GetName() == name {
			r.byType[compType] = append(components[:i], components[i+1:]...)
			break
		}
	}

	if defaultName, hasDefault := r.defaults[compType]; hasDefault && defaultName == name {
		delete(r.defaults, compType)
		if len(r.byType[compType]) > 0 {
			r.defaults[compType] = r.byType[compType][0].GetName()
		}
	}

	return nil
}

func (r *Registry) Get(name string) Component {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.components[name]
}

func (r *Registry) GetByType(typ ComponentType) []Component {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byType[typ]
}

func (r *Registry) GetDefault(typ ComponentType) Component {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name, ok := r.defaults[typ]
	if !ok {
		return nil
	}
	return r.components[name]
}

func (r *Registry) SetDefault(typ ComponentType, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.components[name]; !exists {
		return fmt.Errorf("component %s not found", name)
	}
	r.defaults[typ] = name
	return nil
}

func (r *Registry) SetComponentStatus(name string, status ComponentStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[name] = status
}

func (r *Registry) GetComponentStatus(name string) ComponentStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.statuses[name]; ok {
		return s
	}
	return StatusUninitialized
}

func (r *Registry) InitializeAll(ctx context.Context) error {
	r.mu.RLock()
	order, err := r.topologicalSort()
	if err != nil {
		r.mu.RUnlock()
		return err
	}
	comps := make([]Component, len(order))
	for i, name := range order {
		comps[i] = r.components[name]
	}
	r.mu.RUnlock()

	for _, c := range comps {
		if err := c.Initialize(ctx, r); err != nil {
			return fmt.Errorf("initialize component %s: %w", c.GetName(), err)
		}
		r.SetComponentStatus(c.GetName(), StatusInitialized)
	}

	r.mu.Lock()
	r.initialized = true
	r.mu.Unlock()
	return nil
}

func (r *Registry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	order, err := r.topologicalSort()
	if err != nil {
		r.mu.RUnlock()
		return err
	}
	comps := make([]Component, len(order))
	for i, name := range order {
		comps[i] = r.components[name]
	}
	r.mu.RUnlock()

	for _, c := range comps {
		if err := c.Start(ctx); err != nil {
			return fmt.Errorf("start component %s: %w", c.GetName(), err)
		}
		r.SetComponentStatus(c.GetName(), StatusStarted)
	}
	return nil
}

func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.RLock()
	order, err := r.topologicalSort()
	if err != nil {
		r.mu.RUnlock()
		return err
	}
	comps := make([]Component, len(order))
	for i, name := range order {
		comps[i] = r.components[name]
	}
	r.mu.RUnlock()

	for i := len(comps) - 1; i >= 0; i-- {
		if err := comps[i].Stop(ctx); err != nil {
			return fmt.Errorf("stop component %s: %w", comps[i].GetName(), err)
		}
		r.SetComponentStatus(comps[i].GetName(), StatusStopped)
	}
	return nil
}

// topologicalSort
//
//	@Description: sort for all register component
//	@receiver r
//	@return []string
//	@return error
func (r *Registry) topologicalSort() ([]string, error) {
	graph := make(map[string][]string)
	inDegree := make(map[string]int)

	for name := range r.components {
		inDegree[name] = 0
	}

	for name, c := range r.components {
		for _, dep := range c.Dependencies() {
			if dep.Name != "" {
				graph[dep.Name] = append(graph[dep.Name], name)
				inDegree[name]++
			}
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var result []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, curr)

		for _, neighbor := range graph[curr] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(r.components) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

type dependencies struct {
	registry *Registry
}

func (d *dependencies) Get(name string) Component {
	return d.registry.Get(name)
}

func (d *dependencies) GetByType(typ ComponentType) []Component {
	return d.registry.GetByType(typ)
}

func (d *dependencies) GetDefault(typ ComponentType) Component {
	return d.registry.GetDefault(typ)
}
