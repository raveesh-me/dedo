package app

import (
	"strconv"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	todov1 "github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1"
)

// store keeps to-dos in creation order. It hands out clones, so callers can
// never change stored state.
type store struct {
	mu     sync.Mutex
	todos  map[string]*todov1.Todo
	order  []string
	nextID int
}

func newStore() *store { return &store{todos: map[string]*todov1.Todo{}} }

func (s *store) get(name string) (*todov1.Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	todo, ok := s.todos[name]
	if !ok {
		return nil, false
	}
	return clone(todo), true
}

// list returns up to size to-dos from offset, and whether more follow.
func (s *store) list(offset, size int) ([]*todov1.Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset >= len(s.order) {
		return nil, false
	}
	end := min(offset+size, len(s.order))
	todos := make([]*todov1.Todo, 0, end-offset)
	for _, name := range s.order[offset:end] {
		todos = append(todos, clone(s.todos[name]))
	}
	return todos, end < len(s.order)
}

// create stores a copy of todo under id, or a generated ID when id is empty.
// It reports false when the name is taken.
func (s *store) create(id string, todo *todov1.Todo) (*todov1.Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		s.nextID++
		id = strconv.Itoa(s.nextID)
	}
	name := "todos/" + id
	if _, taken := s.todos[name]; taken {
		return nil, false
	}
	stored := clone(todo)
	stored.Name = name
	stored.CreateTime = timestamppb.Now()
	s.todos[name] = stored
	s.order = append(s.order, name)
	return clone(stored), true
}

// update applies the masked fields of patch to the stored to-do.
func (s *store) update(patch *todov1.Todo, paths []string) (*todov1.Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.todos[patch.GetName()]
	if !ok {
		return nil, false
	}
	next := clone(current)
	if len(paths) == 0 {
		paths = []string{"title", "done", "budget_amount", "assignee_email"}
	}
	for _, path := range paths {
		switch path {
		case "title":
			next.Title = patch.GetTitle()
		case "done":
			next.Done = patch.GetDone()
		case "budget_amount":
			next.BudgetAmount = patch.GetBudgetAmount()
		case "assignee_email":
			next.AssigneeEmail = patch.GetAssigneeEmail()
		}
	}
	s.todos[next.GetName()] = next
	return clone(next), true
}

// delete removes the to-do and returns its last state.
func (s *store) delete(name string) (*todov1.Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.todos[name]
	if !ok {
		return nil, false
	}
	delete(s.todos, name)
	for i, n := range s.order {
		if n == name {
			s.order = append(s.order[:i:i], s.order[i+1:]...)
			break
		}
	}
	return clone(last), true
}

func clone(todo *todov1.Todo) *todov1.Todo {
	return proto.Clone(todo).(*todov1.Todo)
}
