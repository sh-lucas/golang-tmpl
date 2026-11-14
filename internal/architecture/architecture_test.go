// Package architecture enforces import boundaries that the Go compiler does not.
package architecture_test

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	module   = "github.com/rox-projects/golang-tmpl"
	features = module + "/internal/features/"
	database = module + "/internal/database"
)

func TestImportBoundaries(t *testing.T) {
	for _, test := range []bool{false, true} {
		format := `{{.ImportPath}} {{join .Imports " "}}`
		if test {
			format = `{{.ImportPath}} {{join .TestImports " "}} {{join .XTestImports " "}}`
		}
		out, err := exec.Command("go", "list", "-f", format, module+"/...").Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			for _, imported := range fields[1:] {
				if msg := violation(fields[0], imported, test); msg != "" {
					t.Errorf("%s imports %s: %s", fields[0], imported, msg)
				}
			}
		}
	}
}

func violation(pkg, imported string, test bool) string {
	// Exception: any package's tests may open the migrated application database.
	if test && imported == database {
		return ""
	}
	if strings.HasPrefix(pkg, module+"/common/") && strings.HasPrefix(imported, module+"/internal/") {
		return "common/ is reusable and must not depend on the application; move the shared code into common/ or pass it in as a parameter"
	}
	if target, ok := feature(imported); ok {
		if own, inFeature := feature(pkg); inFeature && own == target {
			return ""
		}
		if strings.HasPrefix(pkg, module+"/cmd/") {
			return ""
		}
		return "only the feature itself and cmd/ may import a feature; move the shared code into common/ or expose it through cmd/backend wiring"
	}
	return ""
}

func feature(pkg string) (string, bool) {
	rest, ok := strings.CutPrefix(pkg, features)
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}
