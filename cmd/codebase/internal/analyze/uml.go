package analyzecmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/spf13/cobra"
)

func newUMLCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var (
		flagDomain   string
		flagSchema   string
		flagNoFields bool
	)

	cmd := &cobra.Command{
		Use:   "uml",
		Short: "Generate UML diagrams (PlantUML or Mermaid)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			scope := resolveAppScope(cmd.Context(), cfg.Graph, *flagApp)
			return runUML(cfg.SDK, flagDomain, flagSchema, flagNoFields, scope, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to entities from one domain")
	cmd.Flags().StringVar(&flagSchema, "schema", "plantuml", "Diagram schema (plantuml|mermaid)")
	cmd.Flags().BoolVar(&flagNoFields, "no-fields", false, "Omit field details")

	return cmd
}

func runUML(client *sdk.Client, domain, schemaType string, noFields bool, scope *appScope, format string) error {
	ctx := context.Background()
	data, err := fetchUMLData(ctx, client)
	if err != nil {
		return err
	}

	var out io.Writer = os.Stdout
	if schemaType == "mermaid" {
		return renderMermaid(out, data, domain, noFields, scope)
	}
	return renderPlantUML(out, data, domain, noFields, scope)
}

type UMLData struct {
	Entities   []*sdkgraph.GraphObject
	Fields     []*sdkgraph.GraphObject
	HasField   []*sdkgraph.GraphRelationship
	References []*sdkgraph.GraphRelationship
}

func fetchUMLData(ctx context.Context, client *sdk.Client) (*UMLData, error) {
	data := &UMLData{}
	var wg sync.WaitGroup
	var errs []error
	var mu sync.Mutex

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	adapter := graph.NewSDKAdapter(client.Graph)
	fetch(func() error { items, err := cbgraph.ListAll(ctx, adapter, "Entity"); data.Entities = items; return err })
	fetch(func() error { items, err := cbgraph.ListAll(ctx, adapter, "Field"); data.Fields = items; return err })
	fetch(func() error {
		items, err := cbgraph.ListAllRels(ctx, adapter, "has_field")
		data.HasField = items
		return err
	})
	fetch(func() error {
		items, err := cbgraph.ListAllRels(ctx, adapter, "references")
		data.References = items
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return data, nil
}

func renderPlantUML(w io.Writer, data *UMLData, domainFilter string, noFields bool, scope *appScope) error {
	fmt.Fprintln(w, "@startuml")
	fmt.Fprintln(w, "!theme plain")
	fmt.Fprintln(w, "skinparam classAttributeIconSize 0")
	fmt.Fprintln(w, "skinparam classFontSize 12")
	fmt.Fprintln(w, "skinparam packageStyle rectangle")

	entityMap := make(map[string]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		entityMap[e.EntityID] = e
	}

	fieldMap := make(map[string]*sdkgraph.GraphObject)
	for _, f := range data.Fields {
		fieldMap[f.EntityID] = f
	}

	entityFields := make(map[string][]*sdkgraph.GraphObject)
	for _, rel := range data.HasField {
		if f, ok := fieldMap[rel.DstID]; ok {
			entityFields[rel.SrcID] = append(entityFields[rel.SrcID], f)
		}
	}

	schemas := make(map[string][]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		domain := cbgraph.StrProp(e, "domain")
		if domainFilter != "" && domain != domainFilter {
			continue
		}
		if !scope.domainPasses(domain) {
			continue
		}
		schema := cbgraph.StrProp(e, "db_schema")
		schemas[schema] = append(schemas[schema], e)
	}

	schemaNames := make([]string, 0, len(schemas))
	for s := range schemas {
		schemaNames = append(schemaNames, s)
	}
	sort.Strings(schemaNames)

	for _, s := range schemaNames {
		fmt.Fprintf(w, "\npackage \"%s\" {\n", s)
		ents := schemas[s]
		sort.Slice(ents, func(i, j int) bool { return cbgraph.StrProp(ents[i], "name") < cbgraph.StrProp(ents[j], "name") })
		for _, e := range ents {
			alias := strings.ReplaceAll(strings.TrimPrefix(cbgraph.DerefKey(e.Key), "entity-"), "-", "_")
			fmt.Fprintf(w, "  class \"%s\" as entity_%s {\n", cbgraph.StrProp(e, "name"), alias)
			if !noFields {
				fields := entityFields[e.EntityID]
				sort.Slice(fields, func(i, j int) bool { return int(cbgraph.AnyPropInt(fields[i], "ordinal")) < int(cbgraph.AnyPropInt(fields[j], "ordinal")) })
				for _, f := range fields {
					renderPlantUMLField(w, f)
				}
			}
			fmt.Fprintln(w, "  }")
		}
		fmt.Fprintln(w, "}")
	}

	for _, rel := range data.References {
		src, ok1 := entityMap[rel.SrcID]
		dst, ok2 := entityMap[rel.DstID]
		if !ok1 || !ok2 {
			continue
		}
		if (domainFilter != "" && cbgraph.StrProp(src, "domain") != domainFilter) || (domainFilter != "" && cbgraph.StrProp(dst, "domain") != domainFilter) {
			continue
		}
		if !scope.domainPasses(cbgraph.StrProp(src, "domain")) || !scope.domainPasses(cbgraph.StrProp(dst, "domain")) {
			continue
		}
		srcAlias := strings.ReplaceAll(strings.TrimPrefix(cbgraph.DerefKey(src.Key), "entity-"), "-", "_")
		dstAlias := strings.ReplaceAll(strings.TrimPrefix(cbgraph.DerefKey(dst.Key), "entity-"), "-", "_")
		label := ""
		if v, ok := rel.Properties["via_field"]; ok {
			label = fmt.Sprintf(" : %v", v)
		}
		fmt.Fprintf(w, "entity_%s --> entity_%s%s\n", srcAlias, dstAlias, label)
	}

	fmt.Fprintln(w, "@enduml")
	return nil
}

func renderPlantUMLField(w io.Writer, f *sdkgraph.GraphObject) {
	name := cbgraph.StrProp(f, "name")
	typ := cbgraph.StrProp(f, "db_type")
	if typ == "" {
		typ = inferDBType(cbgraph.StrProp(f, "go_type"))
	}
	typ = strings.ReplaceAll(typ, ",", ";")
	suffix := ""
	if cbgraph.AnyPropBool(f, "is_pk") {
		suffix = " <<PK>>"
	}
	fmt.Fprintf(w, "    + %s : %s%s\n", name, typ, suffix)
}

func renderMermaid(w io.Writer, data *UMLData, domainFilter string, noFields bool, scope *appScope) error {
	fmt.Fprintln(w, "erDiagram")
	entityMap := make(map[string]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		entityMap[e.EntityID] = e
	}
	fieldMap := make(map[string]*sdkgraph.GraphObject)
	for _, f := range data.Fields {
		fieldMap[f.EntityID] = f
	}
	entityFields := make(map[string][]*sdkgraph.GraphObject)
	for _, rel := range data.HasField {
		if f, ok := fieldMap[rel.DstID]; ok {
			entityFields[rel.SrcID] = append(entityFields[rel.SrcID], f)
		}
	}

	for _, e := range data.Entities {
		if domainFilter != "" && cbgraph.StrProp(e, "domain") != domainFilter {
			continue
		}
		if !scope.domainPasses(cbgraph.StrProp(e, "domain")) {
			continue
		}
		tableName := cbgraph.StrProp(e, "table")
		if tableName == "" {
			tableName = strings.ReplaceAll(cbgraph.DerefKey(e.Key), "-", "_")
		}
		fmt.Fprintf(w, "    %s {\n", tableName)
		if !noFields {
			fields := entityFields[e.EntityID]
			sort.Slice(fields, func(i, j int) bool { return int(cbgraph.AnyPropInt(fields[i], "ordinal")) < int(cbgraph.AnyPropInt(fields[j], "ordinal")) })
			for _, f := range fields {
				typ := cbgraph.StrProp(f, "db_type")
				if typ == "" {
					typ = inferDBType(cbgraph.StrProp(f, "go_type"))
				}
				fmt.Fprintf(w, "        %s %s\n", typ, cbgraph.StrProp(f, "name"))
			}
		}
		fmt.Fprintln(w, "    }")
	}

	for _, rel := range data.References {
		src, ok1 := entityMap[rel.SrcID]
		dst, ok2 := entityMap[rel.DstID]
		if !ok1 || !ok2 {
			continue
		}
		if (domainFilter != "" && cbgraph.StrProp(src, "domain") != domainFilter) || (domainFilter != "" && cbgraph.StrProp(dst, "domain") != domainFilter) {
			continue
		}
		if !scope.domainPasses(cbgraph.StrProp(src, "domain")) || !scope.domainPasses(cbgraph.StrProp(dst, "domain")) {
			continue
		}
		srcTable := cbgraph.StrProp(src, "table")
		if srcTable == "" {
			srcTable = strings.ReplaceAll(cbgraph.DerefKey(src.Key), "-", "_")
		}
		dstTable := cbgraph.StrProp(dst, "table")
		if dstTable == "" {
			dstTable = strings.ReplaceAll(cbgraph.DerefKey(dst.Key), "-", "_")
		}
		label := spRel(rel, "via_field")
		fmt.Fprintf(w, "    %s }o--|| %s : \"%s\"\n", srcTable, dstTable, label)
	}
	return nil
}

func inferDBType(goType string) string {
	goType = strings.TrimPrefix(goType, "*")
	switch goType {
	case "uuid.UUID", "uuid.NullUUID":
		return "uuid"
	case "string":
		return "text"
	case "bool":
		return "boolean"
	case "int", "int64", "int32":
		return "integer"
	case "time.Time":
		return "timestamptz"
	default:
		return "text"
	}
}

func getPropBool(obj *sdkgraph.GraphObject, key string) bool {
	if v, ok := obj.Properties[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getPropInt(obj *sdkgraph.GraphObject, key string) int {
	if v, ok := obj.Properties[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

func spRel(r *sdkgraph.GraphRelationship, key string) string {
	if v, ok := r.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
