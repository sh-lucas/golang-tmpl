package tests

// Import boundaries that the Go compiler does not enforce.

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	modulePath   = "github.com/rox-projects/golang-tmpl"
	featuresPath = modulePath + "/internal/features/"
	databasePath = modulePath + "/database"
)

func TestImportBoundaries(t *testing.T) {
	for _, test := range []bool{false, true} {
		format := `{{.ImportPath}} {{join .Imports " "}}`
		if test {
			format = `{{.ImportPath}} {{join .TestImports " "}} {{join .XTestImports " "}}`
		}
		out, err := exec.Command("go", "list", "-f", format, modulePath+"/...").Output()
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
	if test && imported == databasePath {
		return ""
	}
	if _, inFeature := feature(pkg); !inFeature && strings.HasPrefix(pkg, modulePath+"/internal/") && strings.HasPrefix(imported, databasePath) {
		return "shared internal/ packages receive the *sql.DB as a parameter instead of opening the application database"
	}
	if target, ok := feature(imported); ok {
		if own, inFeature := feature(pkg); inFeature && own == target {
			return ""
		}
		if strings.HasPrefix(pkg, modulePath+"/cmd/") {
			return ""
		}
		return "only the feature itself and cmd/ may import a feature; move the shared code into internal/<package> or expose it through cmd/backend wiring"
	}
	return ""
}

func feature(pkg string) (string, bool) {
	rest, ok := strings.CutPrefix(pkg, featuresPath)
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}
