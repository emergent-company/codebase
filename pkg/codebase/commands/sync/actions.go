package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/config"
	"github.com/emergent-company/codebase/graph"
)

type ActionsOptions struct {
	Repo    string
	Glob    string
	Pattern string
	Sync    bool
	Verbose bool
	Defines bool
}

type actionInfo struct {
	name        string
	displayName string
	actionType  string
	description string
	store       string
}

type actionRecord struct {
	key  string
	info *actionInfo
}

func RunActions(ctx context.Context, g graph.GraphClient, opts *ActionsOptions, yml *config.CodebaseYML, out io.Writer, stderr io.Writer) error {
	glob := opts.Glob
	pattern := opts.Pattern
	if yml != nil {
		if glob == "" {
			glob = yml.Sync.Actions.Glob
		}
		if pattern == "" {
			pattern = yml.Sync.Actions.Pattern
		}
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.actions.glob in .codebase.yml or pass --glob")
	}
	if pattern == "" {
		pattern = "mobx"
	}

	absRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	fmt.Fprintf(stderr, "→ Scanning store files with glob: %s (pattern: %s)\n", glob, pattern)
	var diskActions []actionRecord
	actionsByFile := map[string][]actionRecord{}
	if err := walkGlob(absRoot, glob, func(rel string) {
		base := filepath.Base(rel)
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
			base == "index.ts" || base == "index.tsx" {
			return
		}
		actions := extractActions(filepath.Join(absRoot, rel), rel, pattern)
		diskActions = append(diskActions, actions...)
		if len(actions) > 0 {
			actionsByFile[rel] = append(actionsByFile[rel], actions...)
		}
	}); err != nil {
		return fmt.Errorf("walking store files: %w", err)
	}
	sort.Slice(diskActions, func(i, j int) bool { return diskActions[i].key < diskActions[j].key })
	fmt.Fprintf(stderr, "  %d actions found\n", len(diskActions))

	fmt.Fprintln(stderr, "→ Fetching existing Action objects from graph...")
	graphObjs, err := ListAllObjects(ctx, g, "Action")
	if err != nil {
		return fmt.Errorf("fetching actions: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if DerefKey(obj.Key) != "" {
			graphByKey[DerefKey(obj.Key)] = obj
		}
	}

	var toCreate, upToDate []actionRecord
	for _, a := range diskActions {
		if _, exists := graphByKey[a.key]; exists {
			upToDate = append(upToDate, a)
		} else {
			toCreate = append(toCreate, a)
		}
	}

	fmt.Fprintf(out, "┌─ ACTIONS SYNC STATUS\n")
	fmt.Fprintf(out, "  Disk actions    : %d\n", len(diskActions))
	fmt.Fprintf(out, "  Graph objects   : %d\n", len(graphByKey))
	fmt.Fprintf(out, "  To create       : %d\n", len(toCreate))
	fmt.Fprintf(out, "  Up to date      : %d\n", len(upToDate))

	if opts.Verbose {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ ALL ACTIONS\n")
		fmt.Fprintf(out, "  %-40s  %-15s  %s\n", "Key", "Type", "Store")
		fmt.Fprintf(out, "  %-40s  %-15s  %s\n", strings.Repeat("─", 40), strings.Repeat("─", 15), strings.Repeat("─", 25))
		for _, a := range diskActions {
			fmt.Fprintf(out, "  %-40s  %-15s  %s\n", truncate(a.key, 40), a.info.actionType, a.info.store)
		}
	}

	if len(toCreate) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ TO CREATE (%d)\n", len(toCreate))
		for _, a := range toCreate {
			fmt.Fprintf(out, "  + %-40s  type=%s  store=%s\n", a.key, a.info.actionType, a.info.store)
		}
	}

	if !opts.Sync && !opts.Defines {
		if len(toCreate) > 0 {
			fmt.Fprintln(out, "\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNCING\n")

	if opts.Sync {
		if len(toCreate) > 0 {
			fmt.Fprintf(out, "Creating %d Action objects...\n", len(toCreate))
			created, failed := batchCreateActions(ctx, g, toCreate)
			fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
		}
	}

	if opts.Defines {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ WIRING SOURCEFILE → ACTION DEFINES\n")
		wired, skipped, missing := wireActionDefines(ctx, g, absRoot, diskActions, actionsByFile, opts.Verbose, out)
		fmt.Fprintf(out, "  Wired: %d  Skipped (exists): %d  Unresolved: %d\n", wired, skipped, missing)
	}

	fmt.Fprintln(out, "\nSync complete.")
	return nil
}

var (
	mobxActionRe = regexp.MustCompile(`^\s{2,}((?:async\s+)?([a-zA-Z][a-zA-Z0-9_]+))\s*[=(]`)
	exportedFnRe = regexp.MustCompile(`^export\s+(?:async\s+)?function\s+([a-zA-Z][a-zA-Z0-9_]+)\s*\(`)
	exportedArrowRe = regexp.MustCompile(`^export\s+const\s+([a-zA-Z][a-zA-Z0-9_]+)\s*=\s*(?:async\s*)?\(`)
	reduxThunkRe = regexp.MustCompile(`^export\s+const\s+([a-zA-Z][a-zA-Z0-9_]+)\s*=\s*createAsyncThunk\(`)
	zustandActionRe = regexp.MustCompile(`^\s{2,}([a-zA-Z][a-zA-Z0-9_]+)\s*:\s*(?:async\s*)?\(`)
)

var skipMethods = map[string]bool{
	"constructor": true, "render": true, "get": true, "set": true,
	"toString": true, "toJSON": true, "dispose": true, "init": true,
	"makeAutoObservable": true, "makeObservable": true, "runInAction": true,
	"action": true, "observable": true, "computed": true, "reaction": true,
	"autorun": true, "when": true, "flow": true, "intercept": true, "observe": true,
	"async": true, "return": true, "if": true, "else": true, "for": true,
	"while": true, "switch": true, "case": true, "break": true, "continue": true,
	"try": true, "catch": true, "finally": true, "throw": true, "new": true,
	"typeof": true, "instanceof": true, "void": true, "delete": true,
}

var skipPrefixes = []string{"get", "is", "has", "can", "should", "compute", "select"}

func extractActions(absPath, rel, pattern string) []actionRecord {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	storeName := storeNameFromPath(rel)
	var results []actionRecord
	seen := map[string]bool{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var methodName string

		switch pattern {
		case "redux":
			if m := reduxThunkRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedFnRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedArrowRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		case "zustand":
			if m := zustandActionRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		default: // mobx
			if m := mobxActionRe.FindStringSubmatch(line); m != nil {
				methodName = m[2]
			} else if m := exportedFnRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedArrowRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		}

		if methodName == "" || seen[methodName] {
			continue
		}
		if len(methodName) < 4 {
			continue
		}
		if skipMethods[methodName] {
			continue
		}
		if isGetterMethod(methodName) {
			continue
		}
		if isNoiseName(methodName) {
			continue
		}

		seen[methodName] = true
		info := &actionInfo{
			name:        methodName,
			displayName: camelToTitle(methodName),
			actionType:  inferActionType(methodName),
			description: fmt.Sprintf("%s action in %s", camelToTitle(methodName), toTitle(storeName)),
			store:       storeName,
		}
		key := actionKey(storeName, methodName)
		results = append(results, actionRecord{key: key, info: info})
	}
	return results
}

func isNoiseName(name string) bool {
	noiseExact := map[string]bool{
		"elem": true, "item": true, "view": true, "type": true, "place": true,
		"count": true, "response": true, "endpoint": true, "hidden": true,
		"loading": true, "loader": true, "candidate": true, "reference": true,
		"take": true, "analyze": true, "cleanup": true, "parse": true,
	}
	return noiseExact[strings.ToLower(name)]
}

func isGetterMethod(name string) bool {
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			next := name[len(prefix)]
			if next >= 'A' && next <= 'Z' {
				return true
			}
		}
	}
	return false
}

func inferActionType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "navigate") || strings.HasPrefix(lower, "goto") || strings.HasPrefix(lower, "redirect"):
		return "navigation"
	case strings.HasPrefix(lower, "fetch") || strings.HasPrefix(lower, "load") || strings.HasPrefix(lower, "refresh"):
		return "trigger"
	case strings.HasPrefix(lower, "create") || strings.HasPrefix(lower, "add") || strings.HasPrefix(lower, "save") ||
		strings.HasPrefix(lower, "update") || strings.HasPrefix(lower, "delete") || strings.HasPrefix(lower, "remove") ||
		strings.HasPrefix(lower, "submit") || strings.HasPrefix(lower, "send") || strings.HasPrefix(lower, "upload"):
		return "mutation"
	case strings.HasPrefix(lower, "toggle") || strings.HasPrefix(lower, "open") || strings.HasPrefix(lower, "close") ||
		strings.HasPrefix(lower, "show") || strings.HasPrefix(lower, "hide") || strings.HasPrefix(lower, "expand") ||
		strings.HasPrefix(lower, "collapse"):
		return "toggle"
	case strings.HasPrefix(lower, "handle"):
		return "trigger"
	default:
		return "trigger"
	}
}

func storeNameFromPath(rel string) string {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSuffix(base, "-store")
	base = strings.TrimSuffix(base, ".store")
	return base
}

func actionKey(store, method string) string {
	storeSlug := slugify(store)
	methodSlug := slugify(camelToSlug(method))
	return "act-" + storeSlug + "-" + methodSlug
}

func camelToTitle(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, ' ')
		}
		result = append(result, r)
	}
	title := string(result)
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return title
}

func camelToSlug(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '-')
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}

func slugify(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(s)
}

func batchCreateActions(ctx context.Context, g graph.GraphClient, actions []actionRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(actions); i += batchSize {
		end := i + batchSize
		if end > len(actions) {
			end = len(actions)
		}
		batch := actions[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, a := range batch {
			key := a.key
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: "Action",
				Key:  &key,
				Properties: map[string]any{
					"name":          a.info.displayName,
					"type":          a.info.actionType,
					"display_label": a.info.displayName,
					"description":   a.info.description,
				},
			})
		}
		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
		if err != nil {
			failed += len(batch)
			continue
		}
		created += resp.Success
		failed += resp.Failed
	}
	return created, failed
}

func wireActionDefines(
	ctx context.Context,
	g graph.GraphClient,
	absRoot string,
	diskActions []actionRecord,
	actionsByFile map[string][]actionRecord,
	verbose bool,
	out io.Writer,
) (int, int, int) {
	sfObjs, err := ListAllObjects(ctx, g, "SourceFile")
	if err != nil {
		fmt.Fprintf(out, "  error fetching SourceFile objects: %v\n", err)
		return 0, 0, 0
	}
	sfByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range sfObjs {
		if DerefKey(obj.Key) != "" {
			sfByKey[DerefKey(obj.Key)] = obj
		}
	}

	actObjs, err := ListAllObjects(ctx, g, "Action")
	if err != nil {
		fmt.Fprintf(out, "  error fetching Action objects: %v\n", err)
		return 0, 0, 0
	}
	actByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range actObjs {
		if DerefKey(obj.Key) != "" {
			actByKey[DerefKey(obj.Key)] = obj
		}
	}

	existingRels := map[string]bool{}
	rels, _ := ListAllRelationships(ctx, g, "defines")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for rel, actions := range actionsByFile {
		sfKey := relToSourceFileKey(rel)
		sfObj, ok := sfByKey[sfKey]
		if !ok {
			unresolved++
			continue
		}

		for _, a := range actions {
			actObj, ok := actByKey[a.key]
			if !ok {
				unresolved++
				continue
			}
			relKey := sfObj.EntityID + ":" + actObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true
			if verbose {
				fmt.Fprintf(out, "  %s → %s\n", sfKey, a.key)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "defines",
				SrcID:  sfObj.EntityID,
				DstID:  actObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Fprintf(out, "  Found %d SourceFile→Action defines edges to wire\n", len(toWire))

	const batchSize = 100
	wired := 0
	skipped := 0
	for i := 0; i < len(toWire); i += batchSize {
		end := i + batchSize
		if end > len(toWire) {
			end = len(toWire)
		}
		resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: toWire[i:end]})
		if err != nil {
			skipped += end - i
			continue
		}
		for _, r := range resp.Results {
			if r.Error != nil {
				skipped++
			} else {
				wired++
			}
		}
	}
	return wired, skipped, unresolved
}
