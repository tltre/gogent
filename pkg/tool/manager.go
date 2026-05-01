package tool

import (
	"context"
	"sync"

	"github.com/tltre/gagent/pkg/component"
)

type ToolManager struct {
	name  string
	mu    sync.RWMutex
	tools map[string]ITool
}

func NewComponent(name string) *ToolManager {
	return &ToolManager{
		name:  name,
		tools: make(map[string]ITool),
	}
}

func (tm *ToolManager) GetName() string {
	return tm.name
}

func (tm *ToolManager) GetType() component.ComponentType {
	return component.ComponentTool
}

func (tm *ToolManager) Initialize(ctx context.Context, deps *component.Registry) error {
	return nil
}

func (tm *ToolManager) Start(ctx context.Context) error {
	return nil
}

func (tm *ToolManager) Stop(ctx context.Context) error {
	return nil
}

func (tm *ToolManager) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (tm *ToolManager) Register(tool ITool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	info := tool.Info()
	tm.tools[info.Name] = tool
	return nil
}

func (tm *ToolManager) Get(name string) ITool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tools[name]
}

func (tm *ToolManager) List() []ToolInfo {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	list := make([]ToolInfo, 0, len(tm.tools))
	for _, t := range tm.tools {
		list = append(list, t.Info())
	}
	return list
}

func (tm *ToolManager) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	tm.mu.RLock()
	tool, ok := tm.tools[name]
	tm.mu.RUnlock()
	if !ok {
		return Result{IsError: true, ErrorMsg: "tool not found: " + name}, nil
	}
	return tool.Execute(ctx, params)
}

func (tm *ToolManager) Stream(ctx context.Context, name string, params map[string]any) (<-chan StreamChunk, error) {
	tm.mu.RLock()
	tool, ok := tm.tools[name]
	tm.mu.RUnlock()
	if !ok {
		ch := make(chan StreamChunk)
		go func() {
			ch <- StreamChunk{Error: nil, Done: true}
			close(ch)
		}()
		return ch, nil
	}
	return tool.Stream(ctx, params)
}
