package handlers

import (
	"github.com/dp229/openpool/pkg/task"
)

// Init registers all default task handlers
func Init() {
	registry := task.Get()
	
	// Register CPU tasks
	registry.Register(NewFibHandler())
	registry.Register(NewSumFibHandler())
	registry.Register(NewMatrixHandler())
	
	// Register AI agent tasks
	registry.Register(NewAgentHandler())
}

// List returns all registered handler names
func List() []string {
	return task.Get().List()
}
