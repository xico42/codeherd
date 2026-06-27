package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Attach      key.Binding
	Shell       key.Binding
	Clone       key.Binding
	New         key.Binding
	Delete      key.Binding
	Remote      key.Binding
	Help        key.Binding
	Quit        key.Binding
	NextProfile key.Binding
	PrevProfile key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Attach: key.NewBinding(
			key.WithKeys("a", "enter"),
			key.WithHelp("a/enter", "agent"),
		),
		Shell: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "shell"),
		),
		Clone: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clone"),
		),
		New: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Remote: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "remote"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		NextProfile: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "next profile"),
		),
		PrevProfile: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "prev profile"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Attach, k.Shell, k.New, k.Delete, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Attach, k.Shell, k.Clone},
		{k.New, k.Delete, k.Remote},
		{k.Help, k.Quit},
	}
}
