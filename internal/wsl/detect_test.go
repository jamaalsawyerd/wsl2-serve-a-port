package wsl

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeEnv struct {
	kernel  string
	env     map[string]string
	interop bool
	onPath  map[string]string
	mode    string
}

func (f fakeEnv) detector() *Detector {
	return &Detector{
		ReadFile: func(string) ([]byte, error) { return []byte(f.kernel + "\n"), nil },
		Getenv:   func(k string) string { return f.env[k] },
		Glob: func(string) ([]string, error) {
			if f.interop {
				return []string{"/proc/sys/fs/binfmt_misc/WSLInterop"}, nil
			}
			return nil, nil
		},
		LookPath: func(name string) (string, error) {
			if p, ok := f.onPath[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Stat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		Output: func(context.Context, string, ...string) ([]byte, error) {
			if f.mode == "" {
				return nil, errors.New("no wslinfo")
			}
			return []byte(f.mode + "\n"), nil
		},
	}
}

var bothExes = map[string]string{
	"powershell.exe": "/mnt/c/ps.exe",
	"netsh.exe":      "/mnt/c/netsh.exe",
}

func TestDetectWSL2(t *testing.T) {
	r := fakeEnv{
		kernel:  "6.18.54.1-microsoft-standard-WSL2",
		env:     map[string]string{"WSL_DISTRO_NAME": "Ubuntu"},
		interop: true,
		onPath:  bothExes,
		mode:    "nat",
	}.detector().Detect(context.Background())

	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if r.Distro != "Ubuntu" || r.NetworkingMode != ModeNAT || r.PowerShell != "/mnt/c/ps.exe" {
		t.Errorf("unexpected report: %+v", r)
	}
}

func TestDetectFailures(t *testing.T) {
	tests := []struct {
		name     string
		env      fakeEnv
		wantFail string
	}{
		{
			name:     "plain linux",
			env:      fakeEnv{kernel: "6.8.0-45-generic", interop: true, onPath: bothExes},
			wantFail: "WSL2 kernel",
		},
		{
			name:     "wsl1",
			env:      fakeEnv{kernel: "4.4.0-19041-Microsoft", interop: true, onPath: bothExes},
			wantFail: "WSL1",
		},
		{
			name:     "interop disabled",
			env:      fakeEnv{kernel: "5.15.0-microsoft-standard-WSL2", onPath: bothExes},
			wantFail: "Windows interop",
		},
		{
			name:     "no powershell",
			env:      fakeEnv{kernel: "5.15.0-microsoft-standard-WSL2", interop: true, onPath: map[string]string{"netsh.exe": "x"}},
			wantFail: "PowerShell",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.env.detector().Detect(context.Background()).Err()
			if !errors.Is(err, ErrNotWSL2) {
				t.Fatalf("Err() = %v, want ErrNotWSL2", err)
			}
			if !strings.Contains(err.Error(), tt.wantFail) {
				t.Errorf("Err() = %q, want it to mention %q", err, tt.wantFail)
			}
		})
	}
}

func TestDetectCustomKernel(t *testing.T) {
	r := fakeEnv{
		kernel:  "6.6.0-my-kernel",
		env:     map[string]string{"WSL_INTEROP": "/run/WSL/1_interop"},
		interop: true,
		onPath:  bothExes,
	}.detector().Detect(context.Background())
	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil for custom WSL2 kernel", err)
	}
}

func TestNetworkingMode(t *testing.T) {
	for in, want := range map[string]NetworkingMode{
		"nat":         ModeNAT,
		"Mirrored":    ModeMirrored,
		"virtioproxy": ModeUnknown,
		"":            ModeUnknown,
	} {
		env := fakeEnv{kernel: "x-microsoft-standard-WSL2", interop: true, onPath: bothExes, mode: in}
		if got := env.detector().Detect(context.Background()).NetworkingMode; got != want {
			t.Errorf("mode %q: got %q, want %q", in, got, want)
		}
	}
}
