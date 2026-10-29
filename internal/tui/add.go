package tui

import (
	"fmt"
	"slices"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/ports"
)

const (
	addModePick = "pick"
	addModeType = "type"
	maxFormWide = 72
)

// addForm holds the add form's bound values. It lives on the heap because
// huh writes through pointers while the Model itself is copied on every
// update.
type addForm struct {
	mode   string
	spec   string
	picked []int
}

// ports returns the ports the user chose.
func (a *addForm) ports() ([]int, error) {
	if a.mode == addModePick {
		return slices.Sorted(slices.Values(a.picked)), nil
	}
	return ports.Parse(a.spec)
}

// candidates are WSL listeners not already forwarded, best first: reachable
// servers before loopback-only ones.
func candidates(snap forward.Snapshot) []netinfo.Listener {
	var out []netinfo.Listener
	for _, l := range snap.Listeners {
		if _, forwarded := snap.Entry(l.Port); !forwarded {
			out = append(out, l)
		}
	}
	slices.SortStableFunc(out, func(a, b netinfo.Listener) int {
		switch {
		case a.Reachable == b.Reachable:
			return 0
		case a.Reachable:
			return -1
		default:
			return 1
		}
	})
	return out
}

func (m *Model) newAddForm() *huh.Form {
	state := &addForm{mode: addModeType}
	m.add = state

	cands := candidates(m.snap)
	input := huh.NewGroup(
		huh.NewInput().
			Title("Which ports?").
			Description("A port, a range, or a list: 3000 · 8000-8010 · 3000, 5173").
			Placeholder("3000").
			Prompt("❯ ").
			Value(&state.spec).
			Validate(func(s string) error {
				_, err := ports.Parse(s)
				return err
			}),
	)
	if len(cands) == 0 {
		return m.styledForm(huh.NewForm(input))
	}

	state.mode = addModePick
	options := make([]huh.Option[int], len(cands))
	for i, l := range cands {
		options[i] = huh.NewOption(m.listenerLabel(l), l.Port)
	}
	return m.styledForm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Open some ports 🚀").
				Description(fmt.Sprintf("Found %s listening in WSL.", plural(len(cands), "server"))).
				Options(
					huh.NewOption("📡 Pick from running servers", addModePick),
					huh.NewOption("🔢 Type a port or range", addModeType),
				).
				Value(&state.mode),
		),
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Which servers?").
				Description("x or space to toggle · enter to continue").
				Options(options...).
				Height(min(len(cands)+2, 12)).
				Value(&state.picked).
				Validate(func(ps []int) error {
					if len(ps) == 0 {
						return fmt.Errorf("pick at least one")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return state.mode != addModePick }),
		input.WithHideFunc(func() bool { return state.mode != addModeType }),
	))
}

func (m Model) listenerLabel(l netinfo.Listener) string {
	name := l.Process
	if name == "" {
		name = "unknown"
	}
	label := fmt.Sprintf("%-6d %-14s", l.Port, name)
	if !l.Reachable {
		label += " ⚠ localhost only"
	}
	return label
}

func (m Model) styledForm(f *huh.Form) *huh.Form {
	// Filtering is turned off because esc, which leaves a filter, cancels
	// the whole form here.
	km := huh.NewDefaultKeyMap()
	km.Select.Filter.SetEnabled(false)
	km.MultiSelect.Filter.SetEnabled(false)
	return f.
		WithKeyMap(km).
		WithTheme(huhTheme(m.theme)).
		WithWidth(min(maxFormWide, m.width-2*gutter)).
		WithShowHelp(true)
}

// huhTheme dresses huh forms in the app's palette.
func huhTheme(t Theme) huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		s := huh.ThemeBase(t.IsDark)
		f := &s.Focused
		f.Base = f.Base.BorderForeground(t.Purple)
		f.Card = f.Base
		f.Title = f.Title.Foreground(t.Pink).Bold(true)
		f.NoteTitle = f.NoteTitle.Foreground(t.Pink).Bold(true)
		f.Description = f.Description.Foreground(t.Muted)
		f.ErrorIndicator = f.ErrorIndicator.Foreground(t.Red)
		f.ErrorMessage = f.ErrorMessage.Foreground(t.Red)
		f.SelectSelector = f.SelectSelector.Foreground(t.Cyan)
		f.NextIndicator = f.NextIndicator.Foreground(t.Cyan)
		f.PrevIndicator = f.PrevIndicator.Foreground(t.Cyan)
		f.Option = f.Option.Foreground(t.Text)
		f.MultiSelectSelector = f.MultiSelectSelector.Foreground(t.Cyan)
		f.SelectedOption = f.SelectedOption.Foreground(t.Green)
		f.SelectedPrefix = lipgloss.NewStyle().Foreground(t.Green).SetString("✓ ")
		f.UnselectedPrefix = lipgloss.NewStyle().Foreground(t.Muted).SetString("• ")
		f.UnselectedOption = f.UnselectedOption.Foreground(t.Text)
		f.TextInput.Cursor = f.TextInput.Cursor.Foreground(t.Pink)
		f.TextInput.Prompt = f.TextInput.Prompt.Foreground(t.Pink)
		f.TextInput.Placeholder = f.TextInput.Placeholder.Foreground(t.Muted)
		f.TextInput.Text = f.TextInput.Text.Foreground(t.Cyan)

		s.Blurred = s.Focused
		s.Blurred.Base = s.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		s.Blurred.Card = s.Blurred.Base
		s.Group.Title = f.Title
		s.Group.Description = f.Description
		s.Help.ShortKey = s.Help.ShortKey.Foreground(t.Pink)
		s.Help.ShortDesc = s.Help.ShortDesc.Foreground(t.Muted)
		return s
	})
}
