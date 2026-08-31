// Command mkhandler creates a small database-backed HTTP feature.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	modulePath        = "github.com/rox-projects/golang-tmpl"
	importsMarker     = "\t// mkhandler:imports"
	routesMarker      = "\t// mkhandler:routes"
	queriesMarker     = "      # mkhandler:queries"
	usage             = "usage: go run ./cmd/mkhandler <camelCaseName>"
	featureFileMode   = 0o640
	directoryFileMode = 0o750
)

var (
	camelCaseName = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	goKeywords    = map[string]struct{}{
		"break": {}, "default": {}, "func": {}, "interface": {}, "select": {},
		"case": {}, "defer": {}, "go": {}, "map": {}, "struct": {},
		"chan": {}, "else": {}, "goto": {}, "package": {}, "switch": {},
		"const": {}, "fallthrough": {}, "if": {}, "range": {}, "type": {},
		"continue": {}, "for": {}, "import": {}, "return": {}, "var": {},
	}
)

func main() {
	if len(os.Args) != 2 {
		fail(errors.New(usage))
	}
	name := os.Args[1]
	if err := validateName(name); err != nil {
		fail(err)
	}
	if err := create(name); err != nil {
		fail(err)
	}
	fmt.Printf("created handler %q\n", name)
}

func validateName(name string) error {
	if !camelCaseName.MatchString(name) {
		return fmt.Errorf("handler name %q must be camelCase without spaces, underscores, or hyphens", name)
	}
	if _, reserved := goKeywords[name]; reserved {
		return fmt.Errorf("handler name %q is a Go keyword", name)
	}
	return nil
}

func create(name string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	mainPath := filepath.Join(root, "cmd", "backend", "main.go")
	sqlcPath := filepath.Join(root, "sqlc.yaml")
	featureDir := filepath.Join(root, "internal", "features", name)
	if _, err := os.Stat(featureDir); err == nil {
		return fmt.Errorf("feature directory %q already exists", featureDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check feature directory: %w", err)
	}

	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("read route registration file: %w", err)
	}
	mainSource, err = addBeforeMarker(mainSource, importsMarker, "\t\""+modulePath+"/internal/features/"+name+"\"\n")
	if err != nil {
		return fmt.Errorf("add feature import: %w", err)
	}
	mainSource, err = addBeforeMarker(mainSource, routesMarker, "\t"+name+".RegisterRoutes(mux, db)\n")
	if err != nil {
		return fmt.Errorf("add feature route: %w", err)
	}

	sqlcSource, err := os.ReadFile(sqlcPath)
	if err != nil {
		return fmt.Errorf("read sqlc configuration: %w", err)
	}
	queryPath := "internal/features/" + name + "/" + name + "_queries.sql"
	sqlcSource, err = addBeforeMarker(sqlcSource, queriesMarker, "      - \""+queryPath+"\"\n")
	if err != nil {
		return fmt.Errorf("add feature query file: %w", err)
	}

	if err := os.Mkdir(featureDir, directoryFileMode); err != nil {
		return fmt.Errorf("create feature directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(featureDir, name+".go"), []byte(featureSource(name)), featureFileMode); err != nil {
		return fmt.Errorf("write feature: %w", err)
	}
	if err := os.WriteFile(filepath.Join(featureDir, name+"_queries.sql"), []byte("-- Add "+name+" queries here.\n"), featureFileMode); err != nil {
		return fmt.Errorf("write feature queries: %w", err)
	}
	if err := os.WriteFile(mainPath, mainSource, featureFileMode); err != nil {
		return fmt.Errorf("write route registration file: %w", err)
	}
	if err := os.WriteFile(sqlcPath, sqlcSource, featureFileMode); err != nil {
		return fmt.Errorf("write sqlc configuration: %w", err)
	}
	return nil
}

func addBeforeMarker(source []byte, marker, addition string) ([]byte, error) {
	if strings.Count(string(source), marker) != 1 {
		return nil, fmt.Errorf("expected exactly one %q directive", strings.TrimSpace(marker))
	}
	return []byte(strings.Replace(string(source), marker, addition+marker, 1)), nil
}

func featureSource(name string) string {
	return fmt.Sprintf(`package %[1]s

import (
	"database/sql"
	"net/http"

	"%[2]s/common/mug"
)

type handler struct {
	db *sql.DB
}

func RegisterRoutes(mux *http.ServeMux, db *sql.DB) {
	h := handler{db: db}
	mux.HandleFunc("GET /%[1]s/example", func(w http.ResponseWriter, _ *http.Request) {
		h.GetExample().WriteHTTP(w)
	})
}

func (h handler) GetExample() mug.Response {
	return mug.OK(map[string]string{"status": "ok"})
}
`, name, modulePath)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
