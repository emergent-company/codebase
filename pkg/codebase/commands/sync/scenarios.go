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
	"gopkg.in/yaml.v3"
)

type ScenariosDef struct {
	Scenarios []ScenarioDef `yaml:"scenarios"`
}

type ScenarioDef struct {
	Key         string    `yaml:"key"`
	Domain      string    `yaml:"domain"`
	Title       string    `yaml:"title"`
	Given       string    `yaml:"given"`
	When        string    `yaml:"when"`
	Then        string    `yaml:"then"`
	Description string    `yaml:"description"`
	Steps       []StepDef `yaml:"steps"`
}

type StepDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	ContextKey  string `yaml:"context_key"`
}

type ScenariosOptions struct {
	Repo     string
	File     string
	Discover bool
	Router   string
	Store    string
	Sync     bool
	Verbose  bool
}

func RunScenarios(ctx context.Context, g graph.GraphClient, opts *ScenariosOptions, yml *config.CodebaseYML, out io.Writer, stderr io.Writer) error {
	absRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	scenFile := opts.File
	routerFile := opts.Router
	storeGlob := opts.Store
	if yml != nil {
		if scenFile == "" {
			scenFile = yml.Sync.Scenarios.File
		}
		if routerFile == "" {
			routerFile = yml.Sync.Scenarios.RouterFile
		}
		if storeGlob == "" {
			storeGlob = yml.Sync.Scenarios.StoreGlob
		}
	}
	if scenFile == "" {
		scenFile = ".codebase/scenarios.yml"
	}

	absScenFile := filepath.Join(absRoot, scenFile)

	if opts.Discover {
		return runDiscover(absRoot, absScenFile, routerFile, storeGlob, out)
	}

	data, err := os.ReadFile(absScenFile)
	if err != nil {
		return fmt.Errorf("reading scenarios file %s: %w\n\nRun with --discover to generate a starter file.", absScenFile, err)
	}
	var def ScenariosDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return fmt.Errorf("parsing scenarios YAML: %w", err)
	}
	fmt.Fprintf(stderr, "→ Loaded %d scenarios from %s\n", len(def.Scenarios), scenFile)

	fmt.Fprintln(stderr, "→ Fetching existing Scenario objects from graph...")
	existingScenarios, err := ListAllObjects(ctx, g, "Scenario")
	if err != nil {
		return fmt.Errorf("fetching scenarios: %w", err)
	}
	scenByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range existingScenarios {
		if DerefKey(obj.Key) != "" {
			scenByKey[DerefKey(obj.Key)] = obj
		}
	}

	existingSteps, err := ListAllObjects(ctx, g, "ScenarioStep")
	if err != nil {
		return fmt.Errorf("fetching steps: %w", err)
	}
	stepByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range existingSteps {
		if DerefKey(obj.Key) != "" {
			stepByKey[DerefKey(obj.Key)] = obj
		}
	}

	var toCreateScen, toUpdateScen, upToDateScen []scenRecord
	for _, s := range def.Scenarios {
		key := s.Key
		if key == "" {
			key = scenarioKey(s.Domain, s.Title)
			s.Key = key
		}
		rec := scenRecord{def: s, key: key}
		if obj, exists := scenByKey[key]; exists {
			if needsScenarioUpdate(obj, s) {
				toUpdateScen = append(toUpdateScen, rec)
			} else {
				upToDateScen = append(upToDateScen, rec)
			}
		} else {
			toCreateScen = append(toCreateScen, rec)
		}
	}

	totalSteps := 0
	newSteps := 0
	for _, s := range def.Scenarios {
		key := s.Key
		if key == "" {
			key = scenarioKey(s.Domain, s.Title)
		}
		for i, step := range s.Steps {
			totalSteps++
			stepKey := stepKey(key, i+1, step.Name)
			if _, exists := stepByKey[stepKey]; !exists {
				newSteps++
			}
		}
	}

	fmt.Fprintf(out, "┌─ SCENARIOS SYNC STATUS\n")
	fmt.Fprintf(out, "  Defined scenarios : %d\n", len(def.Scenarios))
	fmt.Fprintf(out, "  Graph scenarios   : %d\n", len(scenByKey))
	fmt.Fprintf(out, "  To create         : %d\n", len(toCreateScen))
	fmt.Fprintf(out, "  To update         : %d\n", len(toUpdateScen))
	fmt.Fprintf(out, "  Up to date        : %d\n", len(upToDateScen))
	fmt.Fprintf(out, "  Total steps       : %d\n", totalSteps)
	fmt.Fprintf(out, "  New steps         : %d\n", newSteps)

	if opts.Verbose || len(def.Scenarios) <= 20 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ SCENARIOS\n")
		for _, s := range def.Scenarios {
			key := s.Key
			if key == "" {
				key = scenarioKey(s.Domain, s.Title)
			}
			status := "+"
			if _, exists := scenByKey[key]; exists {
				status = "~"
			}
			fmt.Fprintf(out, "  %s [%s] %s (%d steps)\n", status, s.Domain, s.Title, len(s.Steps))
			if opts.Verbose {
				for i, step := range s.Steps {
					fmt.Fprintf(out, "      %d. %s\n", i+1, step.Name)
				}
			}
		}
	}

	if !opts.Sync {
		if len(toCreateScen) > 0 || len(toUpdateScen) > 0 || newSteps > 0 {
			fmt.Fprintln(out, "\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNCING\n")

	if len(toCreateScen) > 0 {
		fmt.Fprintf(out, "Creating %d Scenario objects...\n", len(toCreateScen))
		created, failed := batchCreateScenarios(ctx, g, toCreateScen)
		fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
	}
	if len(toUpdateScen) > 0 {
		fmt.Fprintf(out, "Updating %d Scenario objects...\n", len(toUpdateScen))
		updated, failed := updateScenarios(ctx, g, toUpdateScen, scenByKey)
		fmt.Fprintf(out, "  Updated: %d  Failed: %d\n", updated, failed)
	}

	updatedScenarios, err := ListAllObjects(ctx, g, "Scenario")
	if err != nil {
		return fmt.Errorf("re-fetching scenarios: %w", err)
	}
	scenByKeyFresh := map[string]*sdkgraph.GraphObject{}
	for _, obj := range updatedScenarios {
		if DerefKey(obj.Key) != "" {
			scenByKeyFresh[DerefKey(obj.Key)] = obj
		}
	}

	if newSteps > 0 {
		fmt.Fprintf(out, "Creating %d ScenarioStep objects...\n", newSteps)
		created, failed := createSteps(ctx, g, def.Scenarios, stepByKey, scenByKeyFresh)
		fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
	}

	allSteps, err := ListAllObjects(ctx, g, "ScenarioStep")
	if err != nil {
		return fmt.Errorf("re-fetching steps: %w", err)
	}
	stepByKeyFresh := map[string]*sdkgraph.GraphObject{}
	for _, obj := range allSteps {
		if DerefKey(obj.Key) != "" {
			stepByKeyFresh[DerefKey(obj.Key)] = obj
		}
	}

	fmt.Fprintln(stderr, "→ Fetching Context objects for relationship wiring...")
	allContexts, err := ListAllObjects(ctx, g, "Context")
	if err != nil {
		return fmt.Errorf("fetching contexts: %w", err)
	}
	ctxByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range allContexts {
		if DerefKey(obj.Key) != "" {
			ctxByKey[DerefKey(obj.Key)] = obj
		}
	}

	fmt.Fprintln(stderr, "→ Wiring relationships (has_step, occurs_in)...")
	wired, skipped, missing := wireRelationships(ctx, g, def.Scenarios, scenByKeyFresh, stepByKeyFresh, ctxByKey)
	fmt.Fprintf(out, "  Wired: %d  Skipped (exists): %d  Missing context: %d\n", wired, skipped, missing)

	fmt.Fprintln(out, "\nSync complete.")
	return nil
}

func scenarioKey(domain, title string) string {
	slug := slugify(strings.ReplaceAll(title, " ", "-"))
	if domain != "" {
		return "scn-" + slugify(domain) + "-" + slug
	}
	return "scn-" + slug
}

func stepKey(scenKey string, order int, name string) string {
	slug := slugify(strings.ReplaceAll(name, " ", "-"))
	return fmt.Sprintf("%s-step-%d-%s", scenKey, order, slug)
}

func needsScenarioUpdate(obj *sdkgraph.GraphObject, def ScenarioDef) bool {
	return StrProp(obj, "title") != def.Title ||
		StrProp(obj, "given") != def.Given ||
		StrProp(obj, "when") != def.When ||
		StrProp(obj, "then") != def.Then
}

type scenRecord struct {
	def ScenarioDef
	key string
}

func batchCreateScenarios(ctx context.Context, g graph.GraphClient, recs []scenRecord) (int, int) {
	const batchSize = 50
	created, failed := 0, 0
	for i := 0; i < len(recs); i += batchSize {
		end := i + batchSize
		if end > len(recs) {
			end = len(recs)
		}
		batch := recs[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, r := range batch {
			key := r.key
			props := map[string]any{
				"name":        r.def.Title,
				"title":       r.def.Title,
				"description": r.def.Description,
			}
			if r.def.Given != "" {
				props["given"] = r.def.Given
			}
			if r.def.When != "" {
				props["when"] = r.def.When
			}
			if r.def.Then != "" {
				props["then"] = r.def.Then
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "Scenario",
				Key:        &key,
				Properties: props,
				Labels:     []string{r.def.Domain},
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

func updateScenarios(ctx context.Context, g graph.GraphClient, recs []scenRecord, byKey map[string]*sdkgraph.GraphObject) (int, int) {
	updated, failed := 0, 0
	for _, r := range recs {
		obj, ok := byKey[r.key]
		if !ok {
			failed++
			continue
		}
		props := map[string]any{
			"title":       r.def.Title,
			"description": r.def.Description,
		}
		if r.def.Given != "" {
			props["given"] = r.def.Given
		}
		if r.def.When != "" {
			props["when"] = r.def.When
		}
		if r.def.Then != "" {
			props["then"] = r.def.Then
		}
		if _, err := g.UpdateObject(ctx, obj.EntityID, &sdkgraph.UpdateObjectRequest{Properties: props}); err != nil {
			failed++
		} else {
			updated++
		}
	}
	return updated, failed
}

func createSteps(ctx context.Context, g graph.GraphClient, scenarios []ScenarioDef, existingSteps, scenByKey map[string]*sdkgraph.GraphObject) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	var items []sdkgraph.CreateObjectRequest
	for _, s := range scenarios {
		scenKey := s.Key
		if scenKey == "" {
			scenKey = scenarioKey(s.Domain, s.Title)
		}
		for i, step := range s.Steps {
			sk := stepKey(scenKey, i+1, step.Name)
			if _, exists := existingSteps[sk]; exists {
				continue
			}
			key := sk
			props := map[string]any{
				"name":        step.Name,
				"order":       i + 1,
				"description": step.Description,
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "ScenarioStep",
				Key:        &key,
				Properties: props,
			})
		}
	}
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items[i:end]})
		if err != nil {
			failed += end - i
			continue
		}
		created += resp.Success
		failed += resp.Failed
	}
	return created, failed
}

func wireRelationships(
	ctx context.Context,
	g graph.GraphClient,
	scenarios []ScenarioDef,
	scenByKey map[string]*sdkgraph.GraphObject,
	stepByKey map[string]*sdkgraph.GraphObject,
	ctxByKey map[string]*sdkgraph.GraphObject,
) (int, int, int) {
	const batchSize = 100
	var rels []sdkgraph.CreateRelationshipRequest
	missingCtx := 0
	for _, s := range scenarios {
		scenKey := s.Key
		if scenKey == "" {
			scenKey = scenarioKey(s.Domain, s.Title)
		}
		scenObj, ok := scenByKey[scenKey]
		if !ok {
			continue
		}
		for i, step := range s.Steps {
			sk := stepKey(scenKey, i+1, step.Name)
			stepObj, ok := stepByKey[sk]
			if !ok {
				continue
			}
			rels = append(rels, sdkgraph.CreateRelationshipRequest{
				Type:   "has_step",
				SrcID:  scenObj.EntityID,
				DstID:  stepObj.EntityID,
				Upsert: true,
			})
			if step.ContextKey != "" {
				ctxObj, ok := ctxByKey[step.ContextKey]
				if !ok {
					missingCtx++
				} else {
					rels = append(rels, sdkgraph.CreateRelationshipRequest{
						Type:   "occurs_in",
						SrcID:  stepObj.EntityID,
						DstID:  ctxObj.EntityID,
						Upsert: true,
					})
				}
			}
		}
	}
	wired := 0
	skipped := 0
	for i := 0; i < len(rels); i += batchSize {
		end := i + batchSize
		if end > len(rels) {
			end = len(rels)
		}
		resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: rels[i:end]})
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
	return wired, skipped, missingCtx
}

func runDiscover(absRoot, outFile, routerFile, storeGlob string, out io.Writer) error {
	fmt.Fprintln(out, "→ Discovering scenarios from codebase...")
	var scenarios []ScenarioDef
	if routerFile != "" {
		absRouter := filepath.Join(absRoot, routerFile)
		routeScenarios := discoverFromRouter(absRouter)
		scenarios = append(scenarios, routeScenarios...)
		fmt.Fprintf(out, "  Found %d route-based scenarios from %s\n", len(routeScenarios), routerFile)
	}
	if storeGlob != "" {
		storeScenarios := discoverFromStores(absRoot, storeGlob)
		scenarios = append(scenarios, storeScenarios...)
		fmt.Fprintf(out, "  Found %d store-based scenarios\n", len(storeScenarios))
	}
	if len(scenarios) == 0 {
		fmt.Fprintln(out, "  No scenarios discovered. Add router_file and store_glob to .codebase.yml sync.scenarios config.")
		return nil
	}
	seen := map[string]bool{}
	var unique []ScenarioDef
	for _, s := range scenarios {
		if !seen[s.Key] {
			seen[s.Key] = true
			unique = append(unique, s)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].Domain != unique[j].Domain {
			return unique[i].Domain < unique[j].Domain
		}
		return unique[i].Title < unique[j].Title
	})
	def := ScenariosDef{Scenarios: unique}
	data, err := yaml.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	header := "# Scenarios definition — generated by `codebase sync scenarios --discover`\n" +
		"# Edit this file to refine scenarios and steps, then run:\n" +
		"#   codebase sync scenarios --sync\n\n"
	if err := os.WriteFile(outFile, append([]byte(header), data...), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outFile, err)
	}
	fmt.Fprintf(out, "\n✓ Generated %d scenarios → %s\n", len(unique), outFile)
	fmt.Fprintln(out, "  Review and edit the file, then run: codebase sync scenarios --sync")
	return nil
}

func discoverFromRouter(routerFile string) []ScenarioDef {
	f, err := os.Open(routerFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	pathRe := regexp.MustCompile(`path:\s*['"]([^'"]+)['"]`)
	elemRe := regexp.MustCompile(`(?:element|component):\s*(?:<)?([A-Z][a-zA-Z]+)`)
	type routeEntry struct {
		path    string
		element string
	}
	var routes []routeEntry
	scanner := bufio.NewScanner(f)
	var currentPath string
	for scanner.Scan() {
		line := scanner.Text()
		if m := pathRe.FindStringSubmatch(line); m != nil {
			currentPath = m[1]
		}
		if m := elemRe.FindStringSubmatch(line); m != nil && currentPath != "" {
			routes = append(routes, routeEntry{path: currentPath, element: m[1]})
			currentPath = ""
		}
	}
	domainRoutes := map[string][]routeEntry{}
	for _, r := range routes {
		parts := strings.Split(strings.TrimPrefix(r.path, "/"), "/")
		domain := parts[0]
		if domain == "" || strings.HasPrefix(domain, ":") {
			domain = "general"
		}
		domainRoutes[domain] = append(domainRoutes[domain], r)
	}
	var scenarios []ScenarioDef
	for domain, routes := range domainRoutes {
		if len(routes) == 0 {
			continue
		}
		title := toTitle(domain) + " Navigation"
		key := scenarioKey(domain, title)
		var steps []StepDef
		for _, r := range routes {
			steps = append(steps, StepDef{
				Name:        "View " + toTitle(strings.ReplaceAll(r.element, "View", "")),
				Description: "Navigate to " + r.path,
				ContextKey:  "",
			})
		}
		scenarios = append(scenarios, ScenarioDef{
			Key:         key,
			Domain:      domain,
			Title:       title,
			Given:       "user is authenticated",
			When:        "user navigates to " + domain + " section",
			Then:        "user can access " + domain + " functionality",
			Description: "Navigation flow for " + domain + " domain",
			Steps:       steps,
		})
	}
	return scenarios
}

func discoverFromStores(absRoot, glob string) []ScenarioDef {
	statusRe := regexp.MustCompile(`(?i)(status|state|phase)\s*[=:]\s*['"]([A-Z_]+)['"]`)
	transitionRe := regexp.MustCompile(`(?i)(start|finish|complete|submit|cancel|approve|reject|sign|create|update|delete)\w*\s*[=(]`)
	type storeFlow struct {
		store    string
		statuses []string
		actions  []string
	}
	var flows []storeFlow
	_ = walkGlob(absRoot, glob, func(rel string) {
		base := filepath.Base(rel)
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			return
		}
		f, err := os.Open(filepath.Join(absRoot, rel))
		if err != nil {
			return
		}
		defer f.Close()
		storeName := storeNameFromPath(rel)
		var statuses, actions []string
		seenStatus := map[string]bool{}
		seenAction := map[string]bool{}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if m := statusRe.FindStringSubmatch(line); m != nil {
				s := m[2]
				if !seenStatus[s] {
					seenStatus[s] = true
					statuses = append(statuses, s)
				}
			}
			if m := transitionRe.FindStringSubmatch(line); m != nil {
				a := strings.TrimRight(m[0], " =(")
				a = strings.TrimSpace(a)
				if !seenAction[a] && len(a) > 3 {
					seenAction[a] = true
					actions = append(actions, a)
				}
			}
		}
		if len(statuses) >= 2 || len(actions) >= 2 {
			flows = append(flows, storeFlow{store: storeName, statuses: statuses, actions: actions})
		}
	})
	var scenarios []ScenarioDef
	for _, flow := range flows {
		domain := flow.store
		var steps []StepDef
		if len(flow.statuses) >= 2 {
			for i, status := range flow.statuses {
				if i >= 8 {
					break
				}
				steps = append(steps, StepDef{
					Name:        toTitle(strings.ToLower(strings.ReplaceAll(status, "_", " "))),
					Description: "System is in " + status + " state",
				})
			}
		} else {
			for i, action := range flow.actions {
				if i >= 6 {
					break
				}
				steps = append(steps, StepDef{
					Name:        camelToTitle(action),
					Description: "User performs " + camelToTitle(action),
				})
			}
		}
		if len(steps) < 2 {
			continue
		}
		title := toTitle(domain) + " Lifecycle"
		key := scenarioKey(domain, title)
		scenarios = append(scenarios, ScenarioDef{
			Key:         key,
			Domain:      domain,
			Title:       title,
			Given:       "user has access to " + domain,
			When:        "user initiates " + domain + " workflow",
			Then:        domain + " progresses through its lifecycle",
			Description: "Lifecycle flow for " + domain + " domain",
			Steps:       steps,
		})
	}
	return scenarios
}
