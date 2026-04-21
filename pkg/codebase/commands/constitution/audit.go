package constitution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
	"github.com/emergent-company/codebase/schema"
)

// AuditRuleDef is a security/performance rule seeded by `constitution audit --seed`.
type AuditRuleDef struct {
	Key         string
	Name        string
	Statement   string
	Category    string
	AppliesTo   string
	AuditType   string // "security", "performance"
	PropCheck   string
	AutoCheck   string
	HowToVerify string
}

// SecurityPerformanceRules is the canonical set of security + performance audit rules.
var SecurityPerformanceRules = []AuditRuleDef{
	// ── Security ──────────────────────────────────────────────────────────────
	{
		Key:       "rule-security-auth-required-set",
		Name:      "All endpoints must declare auth_required",
		Statement: "Every APIEndpoint must have auth_required set to true or false — never absent. Absence means unknown security posture.",
		Category:  "security",
		AppliesTo: "APIEndpoint",
		AuditType: "security",
		PropCheck: `{"field":"auth_required","bool":true}`,
	},
	{
		Key:         "rule-security-public-endpoints-documented",
		Name:        "Public endpoints must be intentional",
		Statement:   "Every APIEndpoint with auth_required=false must have a non-empty 'summary' explaining why it is public.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "List all APIEndpoints where auth_required=false and check that summary is non-empty. Flag any that lack justification.",
	},
	{
		Key:         "rule-security-no-wildcard-scopes",
		Name:        "No wildcard permission scopes",
		Statement:   "No APIEndpoint should have scopes containing '*' — use explicit minimal scopes.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "Check all APIEndpoints where scopes contains '*'. Each is a potential over-permission.",
	},
	{
		Key:         "rule-security-sensitive-domains-auth",
		Name:        "Sensitive domains must require auth",
		Statement:   "Domains classified as sensitive (auth, user, admin, billing, payment) must have 100% of their endpoints with auth_required=true.",
		Category:    "security",
		AppliesTo:   "Domain",
		AuditType:   "security",
		HowToVerify: "For each domain named auth, user, admin, billing, or payment: list its APIEndpoints and verify all have auth_required=true.",
	},
	{
		Key:         "rule-security-no-debug-handlers",
		Name:        "No debug/test handlers in production",
		Statement:   "No APIEndpoint handler name should contain 'debug', 'seed', or 'mock' unless the domain is explicitly a test domain.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "List APIEndpoints whose handler contains 'debug', 'seed', or 'mock' and are not in a test-e2e domain.",
	},
	// ── Performance ───────────────────────────────────────────────────────────
	{
		Key:         "rule-performance-list-endpoints-paginated",
		Name:        "List endpoints must support pagination",
		Statement:   "Every GET APIEndpoint whose path ends in a collection (no trailing :id) should document pagination via limit/cursor or page/size query params.",
		Category:    "performance",
		AppliesTo:   "APIEndpoint",
		AuditType:   "performance",
		HowToVerify: "List GET APIEndpoints whose path does not end in a path param (/:id, /:uuid, etc.). Check that their summary or handler name implies pagination support.",
	},
	{
		Key:         "rule-performance-high-cardinality-domains",
		Name:        "High-cardinality domains must document DB indexes",
		Statement:   "Domains with 20+ APIEndpoints are high-cardinality and must have at least one SourceFile documenting DB indexes or migrations.",
		Category:    "performance",
		AppliesTo:   "Domain",
		AuditType:   "performance",
		HowToVerify: "For each Domain with 20+ APIEndpoints: verify at least one SourceFile in that domain contains a migration or index definition.",
	},
	{
		Key:         "rule-performance-no-n-plus-one",
		Name:        "No N+1 query patterns in service methods",
		Statement:   "ServiceMethod implementations must not call database queries inside loops. Use batch queries or joins instead.",
		Category:    "performance",
		AppliesTo:   "Service",
		AuditType:   "performance",
		HowToVerify: "Scan service files for patterns like 'for ... { await ... find' or 'forEach ... query'. Flag any that appear to loop over DB calls.",
	},
	{
		Key:         "rule-performance-unbounded-queries",
		Name:        "Large responses must be paginated or streamed",
		Statement:   "Any endpoint that could return more than 1000 records must implement pagination (limit/cursor) or streaming. Unbounded queries are a performance risk.",
		Category:    "performance",
		AppliesTo:   "APIEndpoint",
		AuditType:   "performance",
		HowToVerify: "Review GET list endpoints and their corresponding service methods. Check that all list queries have a LIMIT clause or equivalent pagination guard.",
	},
}

// RunAuditSeed upserts all built-in audit rules and wires them to constitution-v1.
func RunAuditSeed(ctx context.Context, gc cbgraph.GraphClient, w io.Writer) error {
	constResp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: "constitution-v1", Type: schema.TypeConstitution, Limit: 1})
	if err != nil {
		return fmt.Errorf("listing constitution: %w", err)
	}
	var constID string
	for _, obj := range constResp.Items {
		if cbgraph.DerefKey(obj.Key) == "constitution-v1" {
			constID = obj.EntityID
			break
		}
	}
	if constID == "" {
		return fmt.Errorf("constitution-v1 not found — run 'codebase constitution create' first")
	}

	seeded := 0
	for _, rule := range SecurityPerformanceRules {
		key := rule.Key
		props := map[string]any{
			"name":       rule.Name,
			"statement":  rule.Statement,
			"category":   rule.Category,
			"audit_type": rule.AuditType,
		}
		if rule.AppliesTo != "" {
			props["applies_to"] = rule.AppliesTo
		}
		if rule.PropCheck != "" {
			props["prop_check"] = rule.PropCheck
		}
		if rule.AutoCheck != "" {
			props["auto_check"] = rule.AutoCheck
		}
		if rule.HowToVerify != "" {
			props["how_to_verify"] = rule.HowToVerify
		}

		ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type:       schema.TypeRule,
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(w, "warn: rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  schema.RelIncludes,
			SrcID: constID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(w, "warn: wiring %s: %v\n", rule.Key, err)
		}
		fmt.Fprintf(w, "  seeded  %-52s  [%s]\n", rule.Key, rule.AuditType)
		seeded++
	}

	fmt.Fprintf(w, "\n%d audit rules seeded into constitution-v1\n", seeded)
	return nil
}

// AuditResult is the evaluation of one audit rule against graph objects.
type AuditResult struct {
	Rule       *sdkgraph.GraphObject
	AuditType  string
	Mode       string // "prop", "auto", "review"
	Passes     []string
	Fails      []string
	ReviewHint string
}

func RunAudit(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, auditTypeFilter, domain, format string) error {
	rules, err := cbgraph.ListAll(ctx, gc, schema.TypeRule)
	if err != nil {
		return fmt.Errorf("listing rules: %w", err)
	}

	// Filter to audit rules matching the type filter
	var auditRules []*sdkgraph.GraphObject
	for _, r := range rules {
		at := anyPropStr(r, "audit_type")
		if at == "" {
			continue
		}
		if auditTypeFilter != "" && !strings.Contains(strings.ToLower(at), strings.ToLower(auditTypeFilter)) {
			continue
		}
		auditRules = append(auditRules, r)
	}

	if len(auditRules) == 0 {
		fmt.Fprintln(w, "No audit rules found. Run: codebase constitution audit --seed")
		return nil
	}

	// Collect all object types needed
	neededTypes := map[string]bool{}
	for _, r := range auditRules {
		for _, t := range strings.Split(cbgraph.StrProp(r, "applies_to"), ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				neededTypes[t] = true
			}
		}
	}

	// Fetch objects per type
	objsByType := map[string][]*sdkgraph.GraphObject{}
	for t := range neededTypes {
		items, err := cbgraph.ListAll(ctx, gc, t)
		if err != nil {
			fmt.Fprintf(w, "warn: listing %s: %v\n", t, err)
			continue
		}
		if domain != "" && t == schema.TypeAPIEndpoint {
			var filtered []*sdkgraph.GraphObject
			for _, o := range items {
				if cbgraph.StrProp(o, "domain") == domain {
					filtered = append(filtered, o)
				}
			}
			items = filtered
		}
		objsByType[t] = items
	}

	// Evaluate each audit rule
	var results []AuditResult
	for _, rule := range auditRules {
		at := anyPropStr(rule, "audit_type")
		appliesTo := strings.TrimSpace(strings.Split(cbgraph.StrProp(rule, "applies_to"), ",")[0])
		objs := objsByType[appliesTo]

		res := AuditResult{
			Rule:      rule,
			AuditType: at,
		}

		propCheckRaw := cbgraph.StrProp(rule, "prop_check")
		autoCheck := cbgraph.StrProp(rule, "auto_check")
		howTo := cbgraph.StrProp(rule, "how_to_verify")

		switch {
		case propCheckRaw != "":
			res.Mode = "prop"
			var spec PropCheckSpec
			if err := json.Unmarshal([]byte(propCheckRaw), &spec); err != nil {
				res.Mode = "review"
				res.ReviewHint = howTo
				break
			}
			for _, obj := range objs {
				key := cbgraph.DerefKey(obj.Key)
				val := anyPropStr(obj, spec.Field)
				if CheckPropSpec(spec, val) {
					res.Passes = append(res.Passes, key)
				} else {
					res.Fails = append(res.Fails, fmt.Sprintf("%s  (%s=%q)", key, spec.Field, val))
				}
			}

		case autoCheck != "":
			res.Mode = "auto"
			rx, err := regexp.Compile(autoCheck)
			if err != nil {
				res.Mode = "review"
				res.ReviewHint = howTo
				break
			}
			for _, obj := range objs {
				key := cbgraph.DerefKey(obj.Key)
				if rx.MatchString(key) {
					res.Passes = append(res.Passes, key)
				} else {
					res.Fails = append(res.Fails, key)
				}
			}

		default:
			res.Mode = "review"
			res.ReviewHint = howTo
		}

		results = append(results, res)
	}

	// Sort: fails first, then review, then passes
	sort.Slice(results, func(i, j int) bool {
		pri := func(r AuditResult) int {
			if (r.Mode == "prop" || r.Mode == "auto") && len(r.Fails) > 0 {
				return 0
			}
			if r.Mode == "review" {
				return 1
			}
			return 2
		}
		return pri(results[i]) < pri(results[j])
	})

	if format == "json" {
		return printAuditJSON(w, results)
	}
	printAuditReport(w, results, auditTypeFilter, domain)
	return nil
}

func printAuditReport(w io.Writer, results []AuditResult, typeFilter, domain string) {
	now := time.Now().Format("2006-01-02")
	title := "SECURITY + PERFORMANCE AUDIT"
	if typeFilter != "" {
		title = strings.ToUpper(typeFilter) + " AUDIT"
	}
	fmt.Fprintf(w, "┌─ %s\n", title)
	fmt.Fprintf(w, "  Generated: %s", now)
	if domain != "" {
		fmt.Fprintf(w, "  · domain: %s", domain)
	}
	fmt.Fprintf(w, "\n\n")

	secFails, perfFails, reviewCount, passCount := 0, 0, 0, 0

	for _, res := range results {
		rKey := cbgraph.DerefKey(res.Rule.Key)
		name := cbgraph.StrProp(res.Rule, "name")
		at := res.AuditType
		atTag := fmt.Sprintf("[%s]", strings.ToUpper(at))

		switch res.Mode {
		case "prop", "auto":
			if len(res.Fails) == 0 {
				passCount++
				fmt.Fprintf(w, "✓  %-14s  %-44s  %d/%d pass\n", atTag, rKey, len(res.Passes), len(res.Passes))
			} else {
				if strings.Contains(at, "security") {
					secFails += len(res.Fails)
				} else {
					perfFails += len(res.Fails)
				}
				fmt.Fprintf(w, "✗  %-14s  %-44s  %d fail / %d pass\n", atTag, rKey, len(res.Fails), len(res.Passes))
				fmt.Fprintf(w, "   %s\n", name)
				shown := res.Fails
				if len(shown) > 8 {
					shown = shown[:8]
				}
				for _, f := range shown {
					fmt.Fprintf(w, "     • %s\n", f)
				}
				if len(res.Fails) > 8 {
					fmt.Fprintf(w, "     … and %d more\n", len(res.Fails)-8)
				}
			}

		case "review":
			reviewCount++
			fmt.Fprintf(w, "?  %-14s  %-44s\n", atTag, rKey)
			fmt.Fprintf(w, "   %s\n", name)
			if res.ReviewHint != "" {
				hint := res.ReviewHint
				if len(hint) > 200 {
					hint = hint[:197] + "..."
				}
				fmt.Fprintf(w, "   → %s\n", hint)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, strings.Repeat("─", 72))
	fmt.Fprintf(w, "Summary:  ✓ %d pass  ✗ %d security violations  ✗ %d performance violations  ? %d manual review\n",
		passCount, secFails, perfFails, reviewCount)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Legend: ✓ verified  ✗ violation  ? AI must review manually")
}

func printAuditJSON(w io.Writer, results []AuditResult) error {
	type jsonResult struct {
		RuleKey    string   `json:"rule_key"`
		Name       string   `json:"name"`
		AuditType  string   `json:"audit_type"`
		Mode       string   `json:"mode"`
		PassCount  int      `json:"pass_count"`
		FailCount  int      `json:"fail_count"`
		Fails      []string `json:"fails,omitempty"`
		ReviewHint string   `json:"review_hint,omitempty"`
	}
	var out []jsonResult
	for _, r := range results {
		jr := jsonResult{
			RuleKey:    cbgraph.DerefKey(r.Rule.Key),
			Name:       cbgraph.StrProp(r.Rule, "name"),
			AuditType:  r.AuditType,
			Mode:       r.Mode,
			PassCount:  len(r.Passes),
			FailCount:  len(r.Fails),
			ReviewHint: r.ReviewHint,
		}
		if len(r.Fails) > 0 {
			jr.Fails = r.Fails
		}
		out = append(out, jr)
	}
	return output.JSONTo(w, out)
}
