package seed

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"strings"
	"sync"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
)

type Entity struct {
	Key, Name, Schema, Table, Domain, ID string
	Fields                               []Field
	Relations                            []Relation
}

type Field struct {
	Key, Name, Column, GoType, DBType, DefaultVal, ID string
	IsPK, IsFK, Nullable                              bool
	Ordinal                                           int
}

type Relation struct {
	Type, Join, ViaField, Target string
}

type EntitiesOptions struct {
	Repo    string
	Glob    string
	Domain  string
	DryRun  bool
	Verbose bool
}

func RunEntities(ctx context.Context, g graph.GraphClient, opts *EntitiesOptions, out io.Writer, stderr io.Writer) error {
	var files []string
	for _, p := range strings.Split(opts.Glob, ",") {
		matches, _ := filepath.Glob(filepath.Join(opts.Repo, p))
		files = append(files, matches...)
	}

	entities := parseEntities(files, opts.Repo)
	if opts.Domain != "" {
		var filtered []Entity
		for _, e := range entities {
			if e.Domain == opts.Domain {
				filtered = append(filtered, e)
			}
		}
		entities = filtered
	}

	fmt.Fprintf(stderr, "→ Creating %d entities...\n", len(entities))
	limit := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for i := range entities {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			e := &entities[idx]
			if opts.DryRun {
				if opts.Verbose {
					fmt.Fprintf(out, "[DRY-RUN] Create Entity: %s\n", e.Key)
				}
				e.ID = "dry-run-" + e.Key
				return
			}
			obj, err := g.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
				Type: "Entity", Key: &e.Key,
				Properties: map[string]any{"name": e.Name, "db_schema": e.Schema, "table": e.Table, "domain": e.Domain},
			})
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return
			}
			e.ID = obj.EntityID
		}(i)
	}
	wg.Wait()

	return nil
}

func parseEntities(files []string, repo string) []Entity {
	var entities []Entity
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		domainVal := inferDomain(path)
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				tSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				sTyp, ok := tSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				_ = sTyp
				_ = domainVal
			}
		}
	}
	return entities
}

func inferDomain(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "domain" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "unknown"
}
