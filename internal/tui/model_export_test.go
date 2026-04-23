package tui

import "github.com/xico42/codeherd/internal/config"

// Test-only accessors — not part of the public API.

func (m Model) SwitchProfileForTest(direction int) Model {
	m2, _ := m.switchProfile(direction)
	return m2
}

func (m Model) Registry() *config.ProfileRegistry { return m.registry }

func (m Model) CurrentConfigForTest() *config.Config { return m.cfg }

func (m Model) StatusMsgForTest() string { return m.statusMsg }

func (m Model) NextProfileEnabledForTest() bool { return m.keys.NextProfile.Enabled() }
