package server

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestConfigDocKeyMatrix is the anti-drift guard for CFGDOC-001: the
// config.go schema is the single source of truth, and the two operator docs
// (server.md parameter table + .env.example) must enumerate exactly its leaf
// key set — no missing keys, no stale extras. Changing the schema without
// updating both docs turns this test red.
func TestConfigDocKeyMatrix(t *testing.T) {
	wantPaths, wantEnv := schemaLeafKeys()
	if len(wantPaths) != len(wantEnv) {
		t.Fatalf("schema walk inconsistent: %d paths vs %d env names", len(wantPaths), len(wantEnv))
	}

	t.Run("server.md parameter table", func(t *testing.T) {
		got := parseServerMDParamTable(t, filepath.Join("..", "..", "docs", "design", "server", "server.md"))
		assertSetEqual(t, "server.md param-table yaml key", wantPaths, got)
	})

	t.Run(".env.example", func(t *testing.T) {
		got := parseEnvExampleNames(t, filepath.Join("..", "..", ".env.example"))
		assertSetEqual(t, ".env.example MPC_ name", wantEnv, got)
	})
}

// schemaLeafKeys independently re-derives, by reflection over Config, the set
// of leaf yaml dotted paths and their generated MPC_ env names. It mirrors
// walkEnv's rules (yaml-tag first segment, recurse structs, leaf = non-struct
// and non-map; env name = envPrefix + upper-cased path with every separator a
// single '_') on purpose: an independent reimplementation is what makes this
// a real drift guard rather than a tautology.
//
// Map-typed fields (e.g. coord.external.expected_members) are intentionally
// skipped — they cannot be expressed unambiguously via env/CLI (walkEnv also
// skips them) and so are not candidates for the env-name doc table; the param
// table and .env.example only enumerate env/CLI-addressable leaves.
func schemaLeafKeys() (paths map[string]struct{}, envs map[string]struct{}) {
	paths = map[string]struct{}{}
	envs = map[string]struct{}{}
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("yaml")
			if tag == "" || tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			if name == "" {
				continue
			}
			path := prefix + name
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path+".")
				continue
			}
			if f.Type.Kind() == reflect.Map {
				continue
			}
			paths[path] = struct{}{}
			envs[envPrefix+strings.ToUpper(strings.ReplaceAll(path, ".", envSep))] = struct{}{}
		}
	}
	walk(reflect.TypeOf(Config{}), "")
	return paths, envs
}

// firstBacktick captures the first `...` token in a string.
var firstBacktick = regexp.MustCompile("`([^`]+)`")

// parseServerMDParamTable extracts the yaml-key column (first cell, first
// backtick token) of every data row of the parameter-table markdown table.
// The table is located by its ASCII header signature ("yaml", "MPC_", "CLI"
// all present in the header row) rather than by the section heading, so the
// parser carries no non-ASCII literal; rows are collected until the table
// ends (first line not starting with "|").
func parseServerMDParamTable(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]struct{}{}
	inTable := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inTable {
			if strings.HasPrefix(trimmed, "|") &&
				strings.Contains(trimmed, "yaml") &&
				strings.Contains(trimmed, "MPC_") &&
				strings.Contains(trimmed, "CLI") {
				inTable = true // this is the header row; data follows
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			break // table ended
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) == 0 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		// Skip the |---|---| separator row (no backtick key).
		if !strings.Contains(first, "`") {
			continue
		}
		m := firstBacktick.FindStringSubmatch(first)
		if m == nil {
			continue
		}
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatalf("no parameter-table rows parsed from %s", path)
	}
	return out
}

// envAssign matches a real env assignment line (not a comment mention).
var envAssign = regexp.MustCompile(`(?m)^(MPC_[A-Z0-9_]+)=`)

// parseEnvExampleNames collects every MPC_* assignment name in .env.example,
// ignoring comment lines (so prose mentions of MPC_ names do not count).
func parseEnvExampleNames(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]struct{}{}
	for _, m := range envAssign.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatalf("no MPC_ assignments parsed from %s", path)
	}
	return out
}

func assertSetEqual(t *testing.T, label string, want, got map[string]struct{}) {
	t.Helper()
	var missing, extra []string
	for k := range want {
		if _, ok := got[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("%s set is missing schema keys (doc not updated for new schema keys): %v", label, missing)
	}
	if len(extra) > 0 {
		t.Errorf("%s set has stale keys not in schema (doc not updated for removed keys): %v", label, extra)
	}
}
