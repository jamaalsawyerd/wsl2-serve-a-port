package ports

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		spec string
		want []int
	}{
		{"3000", []int{3000}},
		{" 3000 ", []int{3000}},
		{"8000-8003", []int{8000, 8001, 8002, 8003}},
		{"5173,3000", []int{3000, 5173}},
		{"3000, 3000 3001", []int{3000, 3001}},
		{"8080-8081,8081-8082", []int{8080, 8081, 8082}},
		{"1 65535", []int{1, 65535}},
		{"9000-9000", []int{9000}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.spec)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tt.spec, err)
			continue
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Parse(%q) = %v, want %v", tt.spec, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		spec, wantMsg string
	}{
		{"", "enter a port"},
		{"abc", "isn't a port"},
		{"0", "out of range"},
		{"65536", "out of range"},
		{"8010-8000", "backwards"},
		{"3000-", "isn't a port"},
		{"1-1000", "limit is 256"},
		{"1-200 1000-1100", "more than 256"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.spec)
		if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
			t.Errorf("Parse(%q) error = %v, want it to contain %q", tt.spec, err, tt.wantMsg)
		}
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		in   []int
		want string
	}{
		{nil, ""},
		{[]int{3000}, "3000"},
		{[]int{8002, 8000, 8001, 3000}, "3000, 8000-8002"},
		{[]int{1, 3, 4, 4, 6}, "1, 3-4, 6"},
	}
	for _, tt := range tests {
		if got := Format(tt.in); got != tt.want {
			t.Errorf("Format(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
