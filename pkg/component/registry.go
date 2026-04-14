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
}

func NewRegistry() *Registry {
	return &Registry{
		components: make(map[string]Component),
		byType:     make(map[ComponentType][]Component),
		defaults:   make(map[ComponentType]string),
	}
}

func (r *Registry) Register(c Component) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := c.Name()
	if _, exists := r.components[name]; exists {
		return fmt.Errorf("component %s already registered", name)
	}

	r.components[name] = c
	r.byType[c.Type()] = append(r.byType[c.Type()], c)

	if _, hasDefault := r.defaults[c.Type()]; !hasDefault {
		r.defaults[c.Type()] = name
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

func (r *Registry) InitializeAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	order, err := r.topologicalSort()
	if err != nil {
		return err
	}

	deps := &dependencies{registry: r}

	for _, name := range order {
		c := r.components[name]
		if err := c.Initialize(ctx, deps); err != nil {
			return fmt.Errorf("initialize component %s: %w", name, err)
		}
	}

	r.initialized = true
	return nil
}

func (r *Registry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, err := r.topologicalSort()
	if err != nil {
		return err
	}

	for _, name := range order {
		c := r.components[name]
		if err := c.Start(ctx); err != nil {
			return fmt.Errorf("start component %s: %w", name, err)
		}
	}
	return nil
}

func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, err := r.topologicalSort()
	if err != nil {
		return err
	}

	for i := len(order) - 1; i >= 0; i-- {
		c := r.components[order[i]]
		if err := c.Stop(ctx); err != nil {
			return fmt.Errorf("stop component %s: %w", c.Name(), err)
		}
	}
	return nil
}

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
