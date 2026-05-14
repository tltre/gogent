package grpctransport

import (
	"fmt"
	"sync"
)

// Pool manages a collection of gRPC connections keyed by target address.
// It reuses existing connections and creates new ones on demand.
// Safe for concurrent use via sync.RWMutex.
type Pool struct {
	mu    sync.RWMutex
	conns map[string]*Conn
}

// NewPool creates a new empty connection pool.
func NewPool() *Pool {
	return &Pool{
		conns: make(map[string]*Conn),
	}
}

// Get returns an existing connection to the target, or creates a new one.
// The returned Conn is shared — do not close it directly. Use Pool.Close() instead.
func (p *Pool) Get(target string) (*Conn, error) {
	// Fast path: read lock for existing connection.
	p.mu.RLock()
	if conn, ok := p.conns[target]; ok {
		p.mu.RUnlock()
		return conn, nil
	}
	p.mu.RUnlock()

	// Slow path: create new connection under write lock with double-check.
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, ok := p.conns[target]; ok {
		return conn, nil
	}

	conn, err := Dial(target)
	if err != nil {
		return nil, fmt.Errorf("grpctransport: pool get %s: %w", target, err)
	}
	p.conns[target] = conn
	return conn, nil
}

// Close closes all managed connections and clears the pool.
// Returns the first error encountered, if any, after attempting to close all connections.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	for target, conn := range p.conns {
		if err := conn.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("grpctransport: pool close %s: %w", target, err)
			}
		}
		delete(p.conns, target)
	}
	return firstErr
}

// Len returns the number of managed connections.
func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// Remove closes and removes the connection for the given target.
func (p *Pool) Remove(target string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, ok := p.conns[target]
	if !ok {
		return fmt.Errorf("grpctransport: pool remove %s: not found", target)
	}
	delete(p.conns, target)
	return conn.Close()
}
