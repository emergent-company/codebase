package analyze

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/schema"
)

func RunUML(ctx context.Context, gc graph.GraphClient, w io.Writer, domain, schemaType string, noFields bool, format string) error {
	data, err := fetchUMLData(ctx, gc)
	if err != nil {
		return err
	}

	if schemaType == "mermaid" {
		return renderMermaid(w, data, domain, noFields)
	}
	return renderPlantUML(w, data, domain, noFields)
}

type UMLData struct {
	Entities   []*sdkgraph.GraphObject
	Fields     []*sdkgraph.GraphObject
	HasField   []*sdkgraph.GraphRelationship
	References []*sdkgraph.GraphRelationship
}

func fetchUMLData(ctx context.Context, gc graph.GraphClient) (*UMLData, error) {
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

	fetch(func() error { items, err := graph.ListAll(ctx, gc, schema.TypeEntity); data.Entities = items; return err })
	fetch(func() error { items, err := graph.ListAll(ctx, gc, schema.TypeField); data.Fields = items; return err })
	fetch(func() error {
		items, err := graph.ListAllRels(ctx, gc, schema.RelHasField)
		data.HasField = items
		return err
	})
	fetch(func() error {
		items, err := graph.ListAllRels(ctx, gc, schema.RelReferences)
		data.References = items
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return data, nil
}

func renderPlantUML(w io.Writer, data *UMLData, domainFilter string, noFields bool) error {
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
		domain := graph.StrProp(e, "domain")
		if domainFilter != "" && domain != domainFilter {
			continue
		}
		schema := graph.StrProp(e, "db_schema")
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
		sort.Slice(ents, func(i, j int) bool { return graph.StrProp(ents[i], "name") < graph.StrProp(ents[j], "name") })
		for _, e := range ents {
			alias := strings.ReplaceAll(strings.TrimPrefix(graph.DerefKey(e.Key), "entity-"), "-", "_")
			fmt.Fprintf(w, "  class \"%s\" as entity_%s {\n", graph.StrProp(e, "name"), alias)
			if !noFields {
				fields := entityFields[e.EntityID]
				sort.Slice(fields, func(i, j int) bool { return GetPropInt(fields[i], "ordinal") < GetPropInt(fields[j], "ordinal") })
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
		if (domainFilter != "" && graph.StrProp(src, "domain") != domainFilter) || (domainFilter != "" && graph.StrProp(dst, "domain") != domainFilter) {
			continue
		}
		srcAlias := strings.ReplaceAll(strings.TrimPrefix(graph.DerefKey(src.Key), "entity-"), "-", "_")
		dstAlias := strings.ReplaceAll(strings.TrimPrefix(graph.DerefKey(dst.Key), "entity-"), "-", "_")
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
	name := graph.StrProp(f, "name")
	typ := graph.StrProp(f, "db_type")
	if typ == "" {
		typ = inferDBType(graph.StrProp(f, "go_type"))
	}
	typ = strings.ReplaceAll(typ, ",", ";")
	suffix := ""
	if GetPropBool(f, "is_pk") {
		suffix = " <<PK>>"
	}
	fmt.Fprintf(w, "    + %s : %s%s\n", name, typ, suffix)
}

func renderMermaid(w io.Writer, data *UMLData, domainFilter string, noFields bool) error {
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
		if domainFilter != "" && graph.StrProp(e, "domain") != domainFilter {
			continue
		}
		tableName := graph.StrProp(e, "table")
		if tableName == "" {
			tableName = strings.ReplaceAll(graph.DerefKey(e.Key), "-", "_")
		}
		fmt.Fprintf(w, "    %s {\n", tableName)
		if !noFields {
			fields := entityFields[e.EntityID]
			sort.Slice(fields, func(i, j int) bool { return GetPropInt(fields[i], "ordinal") < GetPropInt(fields[j], "ordinal") })
			for _, f := range fields {
				typ := graph.StrProp(f, "db_type")
				if typ == "" {
					typ = inferDBType(graph.StrProp(f, "go_type"))
				}
				fmt.Fprintf(w, "        %s %s\n", typ, graph.StrProp(f, "name"))
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
		if (domainFilter != "" && graph.StrProp(src, "domain") != domainFilter) || (domainFilter != "" && graph.StrProp(dst, "domain") != domainFilter) {
			continue
		}
		srcTable := graph.StrProp(src, "table")
		if srcTable == "" {
			srcTable = strings.ReplaceAll(graph.DerefKey(src.Key), "-", "_")
		}
		dstTable := graph.StrProp(dst, "table")
		if dstTable == "" {
			dstTable = strings.ReplaceAll(graph.DerefKey(dst.Key), "-", "_")
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

func GetPropBool(obj *sdkgraph.GraphObject, key string) bool {
	if v, ok := obj.Properties[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}


func spRel(r *sdkgraph.GraphRelationship, key string) string {
	if v, ok := r.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
