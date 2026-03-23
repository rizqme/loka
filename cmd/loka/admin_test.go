package main

import (
	"testing"
)

func TestNewAdminCmd(t *testing.T) {
	cmd := newAdminCmd()

	if cmd.Use != "admin" {
		t.Errorf("Use = %q, want admin", cmd.Use)
	}

	// Verify it has gc and retention subcommands.
	subCmds := cmd.Commands()
	found := map[string]bool{"gc": false, "retention": false}
	for _, sub := range subCmds {
		if _, ok := found[sub.Use]; ok {
			found[sub.Use] = true
		}
	}
	for name, ok := range found {
		if !ok {
			t.Errorf("expected subcommand %q not found", name)
		}
	}
}

func TestNewAdminGCCmd(t *testing.T) {
	cmd := newAdminGCCmd()

	if cmd.Use != "gc" {
		t.Errorf("Use = %q, want gc", cmd.Use)
	}

	// Verify --dry-run flag exists.
	flag := cmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("expected --dry-run flag to exist")
	}
	if flag.DefValue != "false" {
		t.Errorf("--dry-run default = %q, want false", flag.DefValue)
	}
}
