package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down  key.Binding
	Add       key.Binding
	Remove    key.Binding
	RemoveAll key.Binding
	Repair    key.Binding
	Refresh   key.Binding
	Share     key.Binding
	Copy      key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Add:       key.NewBinding(key.WithKeys("a", "+"), key.WithHelp("a", "add port")),
		Remove:    key.NewBinding(key.WithKeys("d", "x", "delete"), key.WithHelp("d", "remove")),
		RemoveAll: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove all")),
		Repair:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repair")),
		Refresh:   key.NewBinding(key.WithKeys("R", "ctrl+r"), key.WithHelp("R", "refresh")),
		Share:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "qr code")),
		Copy:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy url")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Remove, k.Repair, k.Share, k.Copy, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Add, k.Remove, k.RemoveAll},
		{k.Repair, k.Refresh},
		{k.Share, k.Copy},
		{k.Help, k.Quit},
	}
}
