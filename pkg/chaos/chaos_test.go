package chaos

import (
	"testing"
)

func TestChaosMonkey_Execute(t *testing.T) {
	cm := NewChaosMonkey()

	executed := false
	cm.AddAction("test-action", 1.0, func() error {
		executed = true
		return nil
	})

	cm.Execute()

	if !executed {
		t.Error("Expected action to be executed")
	}
}

func TestChaosMonkey_Probability(t *testing.T) {
	cm := NewChaosMonkey()

	cm.AddAction("prob-test", 0.5, func() error {
		return nil
	})

	for i := 0; i < 100; i++ {
		cm.Execute()
	}

	stats := cm.Stats()
	if stats["prob-test"] == 0 {
		t.Error("Expected some executions")
	}
}

func TestChaosMonkey_Disable(t *testing.T) {
	cm := NewChaosMonkey()

	executed := false
	cm.AddAction("disabled-test", 1.0, func() error {
		executed = true
		return nil
	})

	cm.Disable()
	cm.Execute()

	if executed {
		t.Error("Expected no execution when disabled")
	}
}
