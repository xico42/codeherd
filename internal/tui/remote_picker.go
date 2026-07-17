package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/herd"
)

// remoteBranchItem adapts a RemoteBranch to the bubbles list.Item interface.
type remoteBranchItem struct {
	rb herd.RemoteBranch
}

func (i remoteBranchItem) FilterValue() string { return i.rb.Ref }
func (i remoteBranchItem) Title() string       { return i.rb.Ref }
func (i remoteBranchItem) Description() string { return "" }

// remotePickerModel shows a filterable list of remote-tracking branches.
type remotePickerModel struct {
	list    list.Model
	project string
	errText string
}

func newRemotePicker(project string) *remotePickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), maxWidth, 20)
	l.Title = "Remote branches"
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	return &remotePickerModel{
		list:    l,
		project: project,
	}
}

func (p *remotePickerModel) setBranches(branches []herd.RemoteBranch) {
	items := make([]list.Item, len(branches))
	for i, b := range branches {
		items[i] = remoteBranchItem{rb: b}
	}
	p.list.SetItems(items)
}

func (p *remotePickerModel) selected() (herd.RemoteBranch, bool) {
	it, ok := p.list.SelectedItem().(remoteBranchItem)
	if !ok {
		return herd.RemoteBranch{}, false
	}
	return it.rb, true
}

func (p *remotePickerModel) Update(msg tea.Msg) (*remotePickerModel, tea.Cmd) {
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p *remotePickerModel) View() string {
	switch {
	case p.errText != "":
		return "Error: " + p.errText + "\n\nEsc: cancel"
	case len(p.list.Items()) == 0:
		return "No remote branches found (try again after pushing/fetching).\n\nEsc: cancel"
	default:
		return p.list.View()
	}
}
