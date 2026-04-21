package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var fallbackTypes = []string{
	"Scenario", "Context", "Action", "APIEndpoint", "ServiceMethod",
	"SQLQuery", "SourceFile", "ScenarioStep", "UIComponent", "Actor",
	"Domain", "TestSuite", "Pattern", "Rule", "Middleware",
}

type ListOptions struct {
	Type   string
	Key    string
	All    bool
	Limit  int
	Cursor string
	Filter []string
	Status string
	Count  bool
	JSON   bool
	Verbose bool
}

func List(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, opts ListOptions) error {
	var allItems []*sdkgraph.GraphObject
	cursor := opts.Cursor
	limit := opts.Limit
	if opts.All {
		limit = 500
	}

	for {
		req := &sdkgraph.ListObjectsOptions{
			Type:   opts.Type,
			Limit:  limit,
			Cursor: cursor,
			Key:    opts.Key,
		}
		res, err := gc.ListObjects(ctx, req)
		if err != nil {
			return err
		}
		allItems = append(allItems, res.Items...)
		if !opts.All || res.NextCursor == nil || *res.NextCursor == "" {
			break
		}
		cursor = *res.NextCursor
	}

	if opts.Key != "" {
		filtered := allItems[:0]
		for _, item := range allItems {
			if item.Key != nil && (strings.EqualFold(*item.Key, opts.Key) || strings.HasPrefix(*item.Key, opts.Key)) {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	for _, f := range opts.Filter {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			continue
		}
		filterKey, filterVal := parts[0], parts[1]
		filtered := allItems[:0]
		for _, item := range allItems {
			if filterKey == "key" {
				if item.Key != nil && (strings.EqualFold(*item.Key, filterVal) || strings.Contains(*item.Key, filterVal)) {
					filtered = append(filtered, item)
				}
				continue
			}
			if item.Properties != nil {
				if v, ok := item.Properties[filterKey]; ok {
					if fmt.Sprintf("%v", v) == filterVal {
						filtered = append(filtered, item)
					}
				}
			}
		}
		allItems = filtered
	}

	if opts.Status != "" {
		filtered := allItems[:0]
		for _, item := range allItems {
			if item.Status != nil && *item.Status == opts.Status {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	if opts.Count {
		fmt.Fprintln(w, len(allItems))
		return nil
	}

	if opts.JSON {
		data, err := json.MarshalIndent(map[string]interface{}{"items": allItems, "count": len(allItems)}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data))
		return nil
	}

	if opts.Verbose {
		for _, item := range allItems {
			key := ""
			if item.Key != nil {
				key = *item.Key
			}
			status := ""
			if item.Status != nil {
				status = *item.Status
			}
			fmt.Fprintf(w, "%-15s  %s", item.Type, key)
			if status != "" {
				fmt.Fprintf(w, "  [%s]", status)
			}
			fmt.Fprintln(w)

			keys := make([]string, 0, len(item.Properties))
			for k := range item.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(w, "  %-20s %v\n", k+":", item.Properties[k])
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%d objects\n", len(allItems))
		return nil
	}

	fmt.Fprintf(w, "%-15s %-40s %-12s %-15s\n", "TYPE", "KEY", "STATUS", "ID")
	fmt.Fprintln(w, strings.Repeat("─", 84))
	for _, item := range allItems {
		key := ""
		if item.Key != nil {
			key = *item.Key
		}
		status := ""
		if item.Status != nil {
			status = *item.Status
		}
		fmt.Fprintf(w, "%-15s %-40s %-12s %-15s\n", item.Type, key, status, output.ShortID(item.EntityID))
	}
	fmt.Fprintf(w, "\n%d objects\n", len(allItems))
	return nil
}

func Get(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, keyOrID string) error {
	var obj *sdkgraph.GraphObject
	var err error

	if uuidRE.MatchString(keyOrID) {
		obj, err = gc.GetObject(ctx, keyOrID)
	} else {
		obj, err = cbgraph.GetByKey(ctx, gc, keyOrID)
		if err != nil {
			for _, t := range fallbackTypes {
				if o, e2 := cbgraph.GetByKeyAndType(ctx, gc, keyOrID, t); e2 == nil {
					obj = o
					err = nil
					break
				}
			}
		}
	}

	if err != nil {
		return fmt.Errorf("object %q not found", keyOrID)
	}

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(data))
	return nil
}

type CreateOptions struct {
	Type       string
	Key        string
	Properties string
	Status     string
	Upsert     bool
	JSON       bool
}

func Create(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, opts CreateOptions) error {
	var props map[string]interface{}
	if err := json.Unmarshal([]byte(opts.Properties), &props); err != nil {
		return fmt.Errorf("invalid properties JSON: %w", err)
	}

	req := &sdkgraph.CreateObjectRequest{
		Type:       opts.Type,
		Key:        &opts.Key,
		Properties: props,
	}
	if opts.Status != "" {
		req.Status = &opts.Status
	}

	var obj *sdkgraph.GraphObject
	var err error
	if opts.Upsert {
		obj, err = gc.UpsertObject(ctx, req)
	} else {
		obj, err = gc.CreateObject(ctx, req)
	}
	if err != nil {
		return err
	}

	if opts.JSON {
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data))
		return nil
	}
	fmt.Fprintln(w, obj.EntityID)
	return nil
}

type UpdateOptions struct {
	ID         string
	Key        string
	Properties string
	Status     string
}

func Update(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, opts UpdateOptions) error {
	var props map[string]interface{}
	if opts.Properties != "{}" && opts.Properties != "" {
		if err := json.Unmarshal([]byte(opts.Properties), &props); err != nil {
			return fmt.Errorf("invalid properties JSON: %w", err)
		}
	}

	req := &sdkgraph.UpdateObjectRequest{Properties: props}
	if opts.Status != "" {
		req.Status = &opts.Status
	}

	var obj *sdkgraph.GraphObject
	var err error
	if opts.ID != "" {
		obj, err = gc.UpdateObject(ctx, opts.ID, req)
	} else if opts.Key != "" {
		target, err := cbgraph.GetByKey(ctx, gc, opts.Key)
		if err != nil {
			return err
		}
		obj, err = gc.UpdateObject(ctx, target.EntityID, req)
	} else {
		return fmt.Errorf("either --id or --key is required")
	}

	if err != nil {
		return err
	}
	fmt.Fprintln(w, obj.EntityID)
	return nil
}

type RelateOptions struct {
	Type   string
	From   string
	To     string
	Upsert bool
}

func Relate(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, opts RelateOptions) error {
	req := &sdkgraph.CreateRelationshipRequest{
		Type:  opts.Type,
		SrcID: opts.From,
		DstID: opts.To,
	}

	var rel *sdkgraph.GraphRelationship
	var err error
	if opts.Upsert {
		rel, err = gc.UpsertRelationship(ctx, req)
	} else {
		rel, err = gc.CreateRelationship(ctx, req)
	}

	if err != nil {
		return err
	}
	fmt.Fprintln(w, rel.EntityID)
	return nil
}

type UnrelateOptions struct {
	RelID   string
	From    string
	To      string
	RelType string
}

func Unrelate(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, opts UnrelateOptions) error {
	relID := opts.RelID
	if relID == "" && opts.From != "" && opts.To != "" {
		if opts.RelType == "" {
			return fmt.Errorf("--type is required for lookup")
		}
		res, err := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
			SrcID: opts.From,
			DstID: opts.To,
			Type:  opts.RelType,
		})
		if err != nil || len(res.Items) == 0 {
			return fmt.Errorf("relationship not found")
		}
		relID = res.Items[0].EntityID
	} else if relID == "" {
		return fmt.Errorf("provide rel-id or --from/--to/--type")
	}

	return gc.DeleteRelationship(ctx, relID)
}

func Delete(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, keyOrID string, objType string) error {
	var obj *sdkgraph.GraphObject
	var err error

	if uuidRE.MatchString(keyOrID) {
		obj, err = gc.GetObject(ctx, keyOrID)
	} else if objType != "" {
		obj, err = cbgraph.GetByKeyAndType(ctx, gc, keyOrID, objType)
	} else {
		obj, err = cbgraph.GetByKey(ctx, gc, keyOrID)
		if err != nil {
			for _, t := range fallbackTypes {
				if o, e2 := cbgraph.GetByKeyAndType(ctx, gc, keyOrID, t); e2 == nil {
					obj = o
					err = nil
					break
				}
			}
		}
	}

	if err != nil {
		return fmt.Errorf("object %q not found", keyOrID)
	}

	return gc.DeleteObject(ctx, obj.EntityID, nil)
}

func Rename(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, oldKey, newKey string, dryRun bool) error {
	obj, err := cbgraph.GetByKey(ctx, gc, oldKey)
	if err != nil {
		for _, t := range fallbackTypes {
			if o, e2 := cbgraph.GetByKeyAndType(ctx, gc, oldKey, t); e2 == nil {
				obj = o
				err = nil
				break
			}
		}
	}
	if err != nil {
		return fmt.Errorf("object %q not found", oldKey)
	}

	outRels, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 1000})
	inRels, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{DstID: obj.EntityID, Limit: 1000})

	if dryRun {
		fmt.Fprintf(w, "[DRY RUN] Rename %s -> %s (%s)\n", oldKey, newKey, obj.Type)
		return nil
	}

	newObj, err := gc.CreateObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       obj.Type,
		Key:        &newKey,
		Properties: obj.Properties,
		Status:     obj.Status,
	})
	if err != nil {
		return err
	}

	for _, r := range outRels.Items {
		gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: r.Type, SrcID: newObj.EntityID, DstID: r.DstID})
	}
	for _, r := range inRels.Items {
		gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: r.Type, SrcID: r.SrcID, DstID: newObj.EntityID})
	}

	return gc.DeleteObject(ctx, obj.EntityID, nil)
}

func Prune(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, dryRun bool) error {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Limit: 1000})
	if err != nil {
		return err
	}

	for _, obj := range res.Items {
		if obj.Type == "Scenario" {
			continue
		}
		out, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 1})
		in, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{DstID: obj.EntityID, Limit: 1})

		if len(out.Items) == 0 && len(in.Items) == 0 {
			if dryRun {
				key := obj.Type + ":" + output.ShortID(obj.EntityID)
				if obj.Key != nil && *obj.Key != "" {
					key = *obj.Key
				}
				fmt.Fprintf(w, "[DRY RUN] Prune %s (%s)\n", key, obj.Type)
			} else {
				gc.DeleteObject(ctx, obj.EntityID, nil)
			}
		}
	}
	return nil
}

type TreeOptions struct {
	Type  string
	Depth int
}

func Tree(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, keyOrID string, opts TreeOptions) error {
	if opts.Type != "" {
		res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: opts.Type, Limit: 500})
		if err != nil {
			return err
		}
		for _, obj := range res.Items {
			key := obj.Type + ":" + output.ShortID(obj.EntityID)
			if obj.Key != nil && *obj.Key != "" {
				key = *obj.Key
			}
			fmt.Fprintf(w, "%s (%s)\n", key, obj.Type)
			rels, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 20})
			for _, r := range rels.Items {
				dst, _ := gc.GetObject(ctx, r.DstID)
				dstKey := r.DstID
				if dst != nil && dst.Key != nil {
					dstKey = *dst.Key
				}
				fmt.Fprintf(w, "  └── %s -> %s\n", r.Type, dstKey)
			}
		}
		return nil
	}

	if keyOrID == "" {
		return fmt.Errorf("key or ID required")
	}
	var root *sdkgraph.GraphObject
	if uuidRE.MatchString(keyOrID) {
		root, _ = gc.GetObject(ctx, keyOrID)
	} else {
		root, _ = cbgraph.GetByKey(ctx, gc, keyOrID)
	}
	if root == nil {
		return fmt.Errorf("object not found")
	}

	type node struct {
		obj   *sdkgraph.GraphObject
		depth int
	}
	queue := []node{{root, 0}}
	seen := map[string]bool{root.EntityID: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		indent := strings.Repeat("  ", curr.depth)
		key := curr.obj.Type + ":" + output.ShortID(curr.obj.EntityID)
		if curr.obj.Key != nil && *curr.obj.Key != "" {
			key = *curr.obj.Key
		}
		fmt.Fprintf(w, "%s%s (%s)\n", indent, key, curr.obj.Type)

		if curr.depth >= opts.Depth {
			continue
		}

		rels, _ := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: curr.obj.EntityID, Limit: 50})
		for _, r := range rels.Items {
			if seen[r.DstID] {
				continue
			}
			seen[r.DstID] = true
			dst, _ := gc.GetObject(ctx, r.DstID)
			if dst != nil {
				queue = append(queue, node{dst, curr.depth + 1})
			}
		}
	}

	return nil
}

type batchOp struct {
	Op     string                 `json:"op"`
	Type   string                 `json:"type"`
	Key    string                 `json:"key"`
	Status string                 `json:"status"`
	Props  map[string]interface{} `json:"props"`
	From   string                 `json:"from"`
	To     string                 `json:"to"`
}

func CreateBatch(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, werr io.Writer, data []byte, failFast bool) error {
	var ops []batchOp
	if err := json.Unmarshal(data, &ops); err != nil {
		return err
	}

	cache := map[string]string{}

	for i, op := range ops {
		if op.Op == "create" {
			req := &sdkgraph.CreateObjectRequest{Type: op.Type, Key: &op.Key, Properties: op.Props}
			if op.Status != "" {
				req.Status = &op.Status
			}
			obj, err := gc.UpsertObject(ctx, req)
			if err != nil {
				if failFast {
					return err
				}
				fmt.Fprintf(werr, "Error [%d]: %v\n", i, err)
				continue
			}
			cache[op.Key] = obj.EntityID
			fmt.Fprintf(w, "created %s %s\n", op.Key, obj.EntityID)
		} else if op.Op == "relate" {
			srcID := op.From
			if id, ok := cache[op.From]; ok {
				srcID = id
			} else if !uuidRE.MatchString(op.From) {
				if o, _ := cbgraph.GetByKey(ctx, gc, op.From); o != nil {
					srcID = o.EntityID
				}
			}
			dstID := op.To
			if id, ok := cache[op.To]; ok {
				dstID = id
			} else if !uuidRE.MatchString(op.To) {
				if o, _ := cbgraph.GetByKey(ctx, gc, op.To); o != nil {
					dstID = o.EntityID
				}
			}
			rel, err := gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: op.Type, SrcID: srcID, DstID: dstID})
			if err != nil {
				if failFast {
					return err
				}
				fmt.Fprintf(werr, "Error [%d]: %v\n", i, err)
				continue
			}
			fmt.Fprintf(w, "related %s -> %s (%s) %s\n", op.From, op.To, op.Type, rel.EntityID)
		}
	}
	return nil
}
