package tool

import (
	"context"
	"sync"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
)

type ToolManager struct {
	component.BasicComponent
	mu    sync.RWMutex
	tools map[string]ITool
}

func NewComponent(name string) *ToolManager {
	return &ToolManager{
		BasicComponent: component.NewBasicComponent(name),
		tools:          make(map[string]ITool),
	}
}

func (tm *ToolManager) GetType() component.ComponentType {
	return component.ComponentTool
}

func (tm *ToolManager) Initialize(ctx context.Context, registry *component.Registry) error {
	tm.SetRegistry(registry)
	tm.log(ctx, logger.InfoLevel, "tool manager initialized")
	return nil
}

func (tm *ToolManager) Start(ctx context.Context) error {
	tm.log(ctx, logger.DebugLevel, "tool manager started")
	return nil
}

func (tm *ToolManager) Stop(ctx context.Context) error {
	tm.log(ctx, logger.DebugLevel, "tool manager stopped")
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
	tm.log(ctx, logger.DebugLevel, "tool execute started",
		logger.Field{Key: "tool", Value: name},
	)
	start := time.Now()
	result, err := tool.Execute(ctx, params)
	dur := time.Since(start)
	if err != nil {
		tm.log(ctx, logger.ErrorLevel, "tool execute failed",
			logger.Field{Key: "tool", Value: name},
			logger.Field{Key: "error", Value: err.Error()},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return result, err
	}
	tm.log(ctx, logger.InfoLevel, "tool execute completed",
		logger.Field{Key: "tool", Value: name},
		logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
	)
	return result, nil
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

func (tm *ToolManager) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := tm.Registry()
	if r == nil {
		return
	}
	lc := r.GetDefault(component.ComponentLogger)
	if lc == nil {
		return
	}
	l, ok := lc.(logger.Logger)
	if !ok {
		return
	}
	l.Log(ctx, logger.LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Module:    tm.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
