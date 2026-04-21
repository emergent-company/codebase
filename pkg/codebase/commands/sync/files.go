package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
)

var relTypes = []string{"belongs_to", "defines", "handles", "defined_in", "tested_by", "imports", "depends_on"}

type FilesOptions struct {
	Repo        string
	Ext         string
	Sync        bool
	Verbose     bool
	OrphansOnly bool
}

func RunFiles(ctx context.Context, g graph.GraphClient, opts *FilesOptions, out io.Writer, stderr io.Writer) error {
	absRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}
	exts := parseExts(opts.Ext)

	fmt.Fprintf(stderr, "→ Scanning %s\n", absRoot)
	gitignore, _ := loadGitignore(absRoot)

	diskFiles := make(map[string]struct{})
	if err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(absRoot, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || isIgnored(gitignore, rel+"/")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isIgnored(gitignore, rel) && hasExt(rel, exts) {
			diskFiles[rel] = struct{}{}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walking filesystem: %w", err)
	}
	fmt.Fprintf(stderr, "  %d tracked files on disk\n", len(diskFiles))

	fmt.Fprintln(stderr, "→ Fetching graph data...")
	var wg sync.WaitGroup
	objCh := make(chan []*sdkgraph.GraphObject, 1)
	relCh := make(chan map[string][]*sdkgraph.GraphRelationship, 1)
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		objs, err := ListAllObjects(ctx, g, "SourceFile")
		if err != nil {
			errCh <- err
			return
		}
		objCh <- objs
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		allRels := make(map[string][]*sdkgraph.GraphRelationship)
		var mu sync.Mutex
		var relWg sync.WaitGroup
		for _, rt := range relTypes {
			rt := rt
			relWg.Add(1)
			go func() {
				defer relWg.Done()
				rels, err := ListAllRelationships(ctx, g, rt)
				if err != nil {
					return
				}
				mu.Lock()
				allRels[rt] = rels
				mu.Unlock()
			}()
		}
		relWg.Wait()
		relCh <- allRels
	}()

	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			return e
		}
	}

	graphObjects := <-objCh
	allRels := <-relCh

	graphFiles := make(map[string]*sdkgraph.GraphObject, len(graphObjects))
	idToPath := make(map[string]string, len(graphObjects))
	for _, obj := range graphObjects {
		path, _ := obj.Properties["path"].(string)
		if path == "" {
			continue
		}
		graphFiles[path] = obj
		idToPath[obj.EntityID] = path
		if obj.ID != obj.EntityID {
			idToPath[obj.ID] = path
		}
	}

	type relCounts struct {
		byType map[string]int
		total  int
	}
	relCountByID := make(map[string]*relCounts)
	ensureCount := func(id string) *relCounts {
		if _, ok := relCountByID[id]; !ok {
			relCountByID[id] = &relCounts{byType: make(map[string]int)}
		}
		return relCountByID[id]
	}

	totalRelsByType := make(map[string]int)
	for rt, rels := range allRels {
		totalRelsByType[rt] = len(rels)
		for _, r := range rels {
			for _, id := range []string{r.SrcID, r.DstID} {
				if _, ok := idToPath[id]; ok {
					rc := ensureCount(id)
					rc.byType[rt]++
					rc.total++
				}
			}
		}
	}

	var missing, stale []string
	for path := range diskFiles {
		if _, ok := graphFiles[path]; !ok {
			missing = append(missing, path)
		}
	}
	for path := range graphFiles {
		if _, ok := diskFiles[path]; !ok {
			stale = append(stale, path)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	var orphans []string
	for path, obj := range graphFiles {
		if _, onDisk := diskFiles[path]; !onDisk {
			continue
		}
		rc := relCountByID[obj.EntityID]
		if rc == nil || rc.total == 0 {
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNC STATUS\n")
	inSync := len(missing) == 0 && len(stale) == 0
	if inSync {
		fmt.Fprintln(out, "  ✓ Graph is up to date")
	} else {
		fmt.Fprintf(out, "  ✗ Out of sync\n")
	}
	fmt.Fprintf(out, "  Disk files      : %d\n", len(diskFiles))
	fmt.Fprintf(out, "  Graph objects   : %d\n", len(graphFiles))
	fmt.Fprintf(out, "  Missing in graph: %d\n", len(missing))
	fmt.Fprintf(out, "  Stale in graph  : %d\n", len(stale))

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ RELATIONSHIP COVERAGE\n")
	fmt.Fprintf(out, "  %-20s %6s\n", "Type", "Count")
	fmt.Fprintf(out, "  %-20s %6s\n", strings.Repeat("─", 20), strings.Repeat("─", 6))
	for _, rt := range relTypes {
		fmt.Fprintf(out, "  %-20s %6d\n", rt, totalRelsByType[rt])
	}
	fmt.Fprintf(out, "  %-20s %6s\n", strings.Repeat("─", 20), strings.Repeat("─", 6))

	tracked := 0
	wired := 0
	for path, obj := range graphFiles {
		if _, onDisk := diskFiles[path]; !onDisk {
			continue
		}
		tracked++
		rc := relCountByID[obj.EntityID]
		if rc != nil && rc.total > 0 {
			wired++
		}
	}
	pct := 0.0
	if tracked > 0 {
		pct = float64(wired) / float64(tracked) * 100
	}
	fmt.Fprintf(out, "\n  Wired files : %d / %d (%.1f%%)\n", wired, tracked, pct)
	fmt.Fprintf(out, "  Orphan files: %d\n", len(orphans))

	if opts.Verbose || opts.OrphansOnly || len(orphans) > 0 {
		fmt.Fprintln(out)
		if opts.OrphansOnly || (!opts.Verbose && len(orphans) > 0) {
			fmt.Fprintf(out, "┌─ ORPHAN FILES (%d)\n", len(orphans))
			if len(orphans) == 0 {
				fmt.Fprintln(out, "  (none)")
			} else {
				fmt.Fprintf(out, "  %-60s  %s\n", "File", "Relationships")
				fmt.Fprintf(out, "  %-60s  %s\n", strings.Repeat("─", 60), strings.Repeat("─", 13))
				for _, path := range orphans {
					fmt.Fprintf(out, "  %-60s  %s\n", truncate(path, 60), "0  ← orphan")
				}
			}
		}
		if opts.Verbose {
			fmt.Fprintf(out, "┌─ ALL TRACKED FILES\n")
			var allPaths []string
			for path := range graphFiles {
				if _, onDisk := diskFiles[path]; onDisk {
					allPaths = append(allPaths, path)
				}
			}
			sort.Strings(allPaths)
			fmt.Fprintf(out, "  %-60s  %8s  %s\n", "File", "Rels", "Breakdown")
			fmt.Fprintf(out, "  %-60s  %8s  %s\n", strings.Repeat("─", 60), strings.Repeat("─", 8), strings.Repeat("─", 30))
			for _, path := range allPaths {
				obj := graphFiles[path]
				rc := relCountByID[obj.EntityID]
				total := 0
				breakdown := ""
				if rc != nil {
					total = rc.total
					parts := make([]string, 0, len(rc.byType))
					for _, rt := range relTypes {
						if n := rc.byType[rt]; n > 0 {
							parts = append(parts, fmt.Sprintf("%s:%d", rt, n))
						}
					}
					breakdown = strings.Join(parts, "  ")
				}
				orphanMark := ""
				if total == 0 {
					orphanMark = "  ← orphan"
				}
				fmt.Fprintf(out, "  %-60s  %8d  %s%s\n", truncate(path, 60), total, breakdown, orphanMark)
			}
		}
	}

	if len(missing) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ MISSING IN GRAPH (%d)\n", len(missing))
		for _, p := range missing {
			fmt.Fprintf(out, "  + %s\n", p)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ STALE IN GRAPH (%d)\n", len(stale))
		for _, p := range stale {
			fmt.Fprintf(out, "  - %s\n", p)
		}
	}

	if !inSync && !opts.Sync {
		fmt.Fprintln(out, "\nRun with --sync to apply changes.")
		return nil
	}
	if inSync {
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNCING\n")
	if len(missing) > 0 {
		fmt.Fprintf(out, "Creating %d missing SourceFile objects...\n", len(missing))
		created, failed := batchCreateSourceFiles(ctx, g, missing)
		fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
	}
	if len(stale) > 0 {
		fmt.Fprintf(out, "Deleting %d stale SourceFile objects...\n", len(stale))
		deleted, failed := concurrentDelete(ctx, g, stale, graphFiles)
		fmt.Fprintf(out, "  Deleted: %d  Failed: %d\n", deleted, failed)
	}
	fmt.Fprintln(out, "\nSync complete.")
	return nil
}

func batchCreateSourceFiles(ctx context.Context, g graph.GraphClient, paths []string) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(paths); i += batchSize {
		end := i + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, path := range batch {
			key := pathToKey(path)
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: "SourceFile",
				Key:  &key,
				Properties: map[string]any{
					"path":        path,
					"name":        filepath.Base(path),
					"language":    detectLanguage(path),
					"description": "Source file: " + filepath.Base(path),
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

func concurrentDelete(ctx context.Context, g graph.GraphClient, paths []string, graphFiles map[string]*sdkgraph.GraphObject) (int, int) {
	const workers = 16
	type job struct{ path string }
	jobs := make(chan job, len(paths))
	for _, p := range paths {
		jobs <- job{p}
	}
	close(jobs)
	var mu sync.Mutex
	deleted, failed := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				obj := graphFiles[j.path]
				if err := g.DeleteObject(ctx, obj.EntityID, nil); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
				} else {
					mu.Lock()
					deleted++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return deleted, failed
}

func loadGitignore(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns, scanner.Err()
}

func isIgnored(patterns []string, rel string) bool {
	for _, pat := range patterns {
		if matchGitignore(pat, rel) {
			return true
		}
	}
	return false
}

func matchGitignore(pattern, rel string) bool {
	if strings.HasPrefix(pattern, "!") {
		return false
	}
	pat := pattern
	dirOnly := strings.HasSuffix(pat, "/")
	if dirOnly {
		pat = strings.TrimSuffix(pat, "/")
	}
	anchored := strings.HasPrefix(pat, "/")
	if anchored {
		pat = strings.TrimPrefix(pat, "/")
	}
	if m, _ := filepath.Match(pat, rel); m {
		return true
	}
	if !anchored {
		if m, _ := filepath.Match(pat, filepath.Base(rel)); m {
			return true
		}
	}
	if strings.HasPrefix(rel, pat+"/") || rel == pat {
		return true
	}
	if strings.HasPrefix(pat, "**/") {
		suffix := strings.TrimPrefix(pat, "**/")
		if strings.HasSuffix(rel, "/"+suffix) || rel == suffix || filepath.Base(rel) == suffix {
			return true
		}
	}
	return false
}

func hasExt(path string, exts map[string]struct{}) bool {
	_, ok := exts[strings.ToLower(filepath.Ext(path))]
	return ok
}

func parseExts(s string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, e := range strings.Split(s, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m[strings.ToLower(e)] = struct{}{}
	}
	return m
}

func detectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".swift":
		return "swift"
	case ".java":
		return "java"
	case ".kt":
		return "kotlin"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	default:
		return "unknown"
	}
}

func pathToKey(path string) string {
	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(path)
	return "sf-" + strings.ToLower(key)
}
