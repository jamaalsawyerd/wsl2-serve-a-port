package state

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "nested", "state.json")}

	st, err := store.Load()
	if err != nil || len(st.Forwards) != 0 || st.Version != currentVersion {
		t.Fatalf("Load on missing file = %+v, %v", st, err)
	}

	created := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st.Upsert(Forward{Port: 5173, ConnectAddress: "172.17.0.2", CreatedAt: created})
	st.Upsert(Forward{Port: 3000, ConnectAddress: "172.17.0.2", CreatedAt: created})
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Ports(), []int{3000, 5173}) {
		t.Errorf("Ports() = %v", got.Ports())
	}
	if f, ok := got.Get(3000); !ok || !f.CreatedAt.Equal(created) {
		t.Errorf("Get(3000) = %+v, %v", f, ok)
	}

	if entries, _ := os.ReadDir(filepath.Dir(store.Path)); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestUpsertKeepsCreatedAt(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var st State
	st.Upsert(Forward{Port: 3000, ConnectAddress: "old", CreatedAt: first})
	st.Upsert(Forward{Port: 3000, ConnectAddress: "new", CreatedAt: first.Add(time.Hour)})

	f, _ := st.Get(3000)
	if f.ConnectAddress != "new" || !f.CreatedAt.Equal(first) || len(st.Forwards) != 1 {
		t.Errorf("after re-upsert: %+v", st.Forwards)
	}

	st.Remove(3000)
	st.Remove(9999)
	if _, ok := st.Get(3000); ok {
		t.Error("Remove(3000) didn't remove it")
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"garbage": "{nope",
		"future":  `{"version": 99, "forwards": []}`,
	} {
		path := filepath.Join(dir, name+".json")
		os.WriteFile(path, []byte(body), 0o644)
		if _, err := (Store{Path: path}).Load(); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		} else if name == "future" && !strings.Contains(err.Error(), "newer") {
			t.Errorf("future: err = %v", err)
		}
	}
}
