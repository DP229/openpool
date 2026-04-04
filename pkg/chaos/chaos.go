package chaos

import (
	"fmt"
	"math/rand"
	"sync"
)

type ChaosMonkey struct {
	actions []ChaosAction
	mu      sync.RWMutex
	enabled bool
}

type ChaosAction struct {
	Name    string
	Prob    float64
	Action  func() error
	Enabled bool
	Count   int64
}

func NewChaosMonkey() *ChaosMonkey {
	return &ChaosMonkey{
		actions: make([]ChaosAction, 0),
		enabled: true,
	}
}

func (cm *ChaosMonkey) AddAction(name string, prob float64, action func() error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.actions = append(cm.actions, ChaosAction{
		Name:    name,
		Prob:    prob,
		Action:  action,
		Enabled: true,
	})
}

func (cm *ChaosMonkey) Execute() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if !cm.enabled {
		return nil
	}

	for i := range cm.actions {
		if cm.actions[i].Enabled && rand.Float64() < cm.actions[i].Prob {
			cm.actions[i].Count++
			if err := cm.actions[i].Action(); err != nil {
				return fmt.Errorf("chaos action %s failed: %w", cm.actions[i].Name, err)
			}
		}
	}

	return nil
}

func (cm *ChaosMonkey) Enable() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.enabled = true
}

func (cm *ChaosMonkey) Disable() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.enabled = false
}

func (cm *ChaosMonkey) Stats() map[string]int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := make(map[string]int64)
	for _, action := range cm.actions {
		stats[action.Name] = action.Count
	}
	return stats
}
