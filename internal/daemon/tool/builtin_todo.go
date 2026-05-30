package tool

import (
	"context"
	"fmt"
	"sync"
)

type todoItem struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// todoStore is a simple in-memory todo list. Thread-safe.
// v0.12.4: global singleton, no persistence. Data lost on daemon restart.
type todoStore struct {
	mu     sync.Mutex
	items  []todoItem
	nextID int
}

var globalTodo = &todoStore{nextID: 1}

func execTodo(_ context.Context, params map[string]any) (Result, error) {
	action, _ := params["action"].(string)
	switch action {
	case "add":
		item, _ := params["item"].(string)
		if item == "" {
			return Result{IsError: true, ErrorMsg: "todo: item is required for add"}, nil
		}
		return globalTodo.add(item), nil

	case "list":
		return globalTodo.list(), nil

	case "delete":
		id, _ := params["id"].(float64) // JSON numbers decode as float64
		return globalTodo.delete(int(id)), nil

	case "clear":
		return globalTodo.clear(), nil

	default:
		return Result{IsError: true, ErrorMsg: "todo: unknown action: " + action}, nil
	}
}

func (s *todoStore) add(text string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.items = append(s.items, todoItem{ID: id, Text: text})
	return Result{
		Output:  map[string]any{"id": id, "text": text, "total": len(s.items)},
		IsError: false,
	}
}

func (s *todoStore) list() Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]any, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, map[string]any{"id": item.ID, "text": item.Text})
	}
	return Result{Output: items, IsError: false}
}

func (s *todoStore) delete(id int) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return Result{
				Output:  map[string]any{"deleted": id, "total": len(s.items)},
				IsError: false,
			}
		}
	}
	return Result{IsError: true, ErrorMsg: fmt.Sprintf("todo: item %d not found", id)}
}

func (s *todoStore) clear() Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
	s.nextID = 1
	return Result{Output: "todo list cleared", IsError: false}
}
