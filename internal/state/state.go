// Package state remembers which forwards this app created, so it never
// touches portproxy rules made by something else.
package state

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const currentVersion = 1

// Forward is one port this app manages.
type Forward struct {
	Port int `json:"port"`
	// ConnectAddress is the WSL IP the rule pointed at when last applied.
	ConnectAddress string    `json:"connect_address,omitempty"`
	Mirrored       bool      `json:"mirrored,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// State is the on-disk record of managed forwards.
type State struct {
	Version  int       `json:"version"`
	Forwards []Forward `json:"forwards"`
}

// Get returns the forward for port, if managed.
func (s *State) Get(port int) (Forward, bool) {
	i := slices.IndexFunc(s.Forwards, func(f Forward) bool { return f.Port == port })
	if i < 0 {
		return Forward{}, false
	}
	return s.Forwards[i], true
}

// Upsert adds or replaces the forward for f.Port, keeping the original
// CreatedAt when replacing.
func (s *State) Upsert(f Forward) {
	if i := slices.IndexFunc(s.Forwards, func(x Forward) bool { return x.Port == f.Port }); i >= 0 {
		f.CreatedAt = s.Forwards[i].CreatedAt
		s.Forwards[i] = f
		return
	}
	s.Forwards = append(s.Forwards, f)
	slices.SortFunc(s.Forwards, func(a, b Forward) int { return cmp.Compare(a.Port, b.Port) })
}

// Remove forgets port.
func (s *State) Remove(port int) {
	s.Forwards = slices.DeleteFunc(s.Forwards, func(f Forward) bool { return f.Port == port })
}

// Ports returns the managed ports in order.
func (s *State) Ports() []int {
	out := make([]int, len(s.Forwards))
	for i, f := range s.Forwards {
		out[i] = f.Port
	}
	return out
}

// Store reads and writes State at Path.
type Store struct {
	Path string
}

// DefaultStore uses $XDG_CONFIG_HOME/wsl2-serve-a-port/state.json, falling
// back to ~/.config.
func DefaultStore() (Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Store{}, fmt.Errorf("finding config dir: %w", err)
	}
	return Store{Path: filepath.Join(dir, "wsl2-serve-a-port", "state.json")}, nil
}

// Load returns the saved state, or an empty one if nothing is saved yet.
func (s Store) Load() (*State, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Version: currentVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.Path, err)
	}
	if st.Version > currentVersion {
		return nil, fmt.Errorf("%s was written by a newer serve-a-port (version %d)", s.Path, st.Version)
	}
	st.Version = currentVersion
	return &st, nil
}

// Save writes st atomically.
func (s Store) Save(st *State) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".state-*.json")
	if err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("saving state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	return os.Rename(tmp.Name(), s.Path)
}
