package internal_test

import (
	"errors"
	"go/build"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The layers of D-20, from the top. Entry points share a rank, so none
// may import another. A leaf is imported only by the command. A test
// helper may import any package, and only tests import it, which this
// gate does not read.
const (
	rankStore = iota
	rankDomain
	rankRoute
	rankEntry
	rankCommand
	leaf       = -1
	testHelper = -2
)

var layers = map[string]int{
	"command":    rankCommand,
	"web":        rankEntry,
	"api":        rankEntry,
	"live":       rankEntry,
	"worker":     rankEntry,
	"domain":     rankDomain,
	"store":      rankStore,
	"route":      rankRoute,
	"migrations": leaf,
	"assets":     leaf,
	"testdb":     testHelper,
}

// layerOf names the layer of a loafkin.com import path, or "" for a path
// outside the module.
func layerOf(importPath string) string {
	switch {
	case importPath == "loafkin.com/cmd/loafkin":
		return "command"
	case strings.HasPrefix(importPath, "loafkin.com/internal/"):
		name, _, _ := strings.Cut(strings.TrimPrefix(importPath, "loafkin.com/internal/"), "/")
		return name
	default:
		return ""
	}
}

// checkImport returns why from may not import to, or "" when it may.
func checkImport(from, to string) string {
	fromRank, ok := layers[from]
	if !ok {
		return "package " + from + " has no layer; add it to the table in layers_test.go"
	}
	toRank, ok := layers[to]
	if !ok {
		return "package " + to + " has no layer; add it to the table in layers_test.go"
	}
	if from == to {
		return ""
	}
	if toRank == testHelper {
		return from + " imports " + to + ", which only tests may import"
	}
	if fromRank == testHelper {
		return ""
	}
	if toRank == leaf {
		if from == "command" {
			return ""
		}
		return from + " imports " + to + ", which only the command may import"
	}
	if fromRank == leaf || fromRank <= toRank {
		return from + " imports " + to + ", which is not below it"
	}
	return ""
}

func TestCheckImport(t *testing.T) {
	tests := []struct {
		from, to string
		allowed  bool
	}{
		{"command", "web", true},
		{"command", "migrations", true},
		{"web", "domain", true},
		{"api", "store", true},
		{"domain", "store", true},
		{"store", "store", true},
		{"store", "domain", false},
		{"domain", "web", false},
		{"web", "api", false},
		{"web", "migrations", false},
		{"migrations", "store", false},
		{"web", "command", false},
		{"web", "unknown", false},
		{"command", "route", true},
		{"testdb", "migrations", true},
		{"command", "testdb", false},
		{"store", "testdb", false},
		{"web", "route", true},
		{"domain", "route", false},
		{"store", "route", false},
		{"route", "domain", true},
		{"route", "web", false},
		{"unknown", "store", false},
	}
	for _, tt := range tests {
		if got := checkImport(tt.from, tt.to) == ""; got != tt.allowed {
			t.Errorf("checkImport(%q, %q) allowed = %v, want %v", tt.from, tt.to, got, tt.allowed)
		}
	}
}

// TestImportDirection holds D-20 over every package under cmd and
// internal. go/build leaves out test files and applies build constraints,
// so it checks the imports that a build sees.
func TestImportDirection(t *testing.T) {
	packages := 0
	for _, root := range []string{"../cmd", "../internal"} {
		err := filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			pkg, err := build.ImportDir(dir, 0)
			if _, ok := errors.AsType[*build.NoGoError](err); ok {
				return nil
			}
			if err != nil {
				return err
			}
			packages++
			from := layerOf("loafkin.com/" + filepath.ToSlash(strings.TrimPrefix(dir, "../")))
			for _, to := range pkg.Imports {
				if layer := layerOf(to); layer != "" {
					if why := checkImport(from, layer); why != "" {
						t.Errorf("%s: %s", dir, why)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	// cmd/loafkin, internal/route, internal/store and internal/migrations at least.
	if packages < 4 {
		t.Errorf("read %d packages, want at least 4", packages)
	}
}
