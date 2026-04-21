package constitution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
	"github.com/emergent-company/codebase/schema"
)

// PropCheckSpec is the parsed form of a rule's prop_check JSON property.
type PropCheckSpec struct {
	Field    string `json:"field"`
	Nonempty bool   `json:"nonempty"` // value must be non-empty string
	Prefix   string `json:"prefix"`   // value must start with this prefix
	Bool     bool   `json:"bool"`     // value must be "true" or "false" (not absent)
}

// RuleEval is the result of evaluating one rule.
type RuleEval struct {
	Rule *sdkgraph.GraphObject
	Mode string // "auto", "prop", "scan", "review"
	// auto + prop mode
	Passes []string
	Fails  []string
	// scan mode
	ScanMatches    int
	ScanSamples    []string // up to 5 lines
	ScanViolations []string // files with 0 matches when matches expected, or vice versa
	// review mode — nothing to compute, just show how_to_verify
}

func RunCheck(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, objType, category, domain, repo, format string) error {
	// Load objects and rules in parallel
	objCh := make(chan []*sdkgraph.GraphObject, 1)
	ruleCh := make(chan []*sdkgraph.GraphObject, 1)
	errCh := make(chan error, 2)

	go func() {
		items, err := cbgraph.ListAll(ctx, gc, objType)
		if err != nil {
			errCh <- err
			objCh <- nil
			return
		}
		objCh <- items
	}()
	go func() {
		items, err := cbgraph.ListAll(ctx, gc, schema.TypeRule)
		if err != nil {
			errCh <- err
			ruleCh <- nil
			return
		}
		ruleCh <- items
	}()

	objs := <-objCh
	rules := <-ruleCh

	select {
	case err := <-errCh:
		return err
	default:
	}

	// Filter objects by domain
	if domain != "" {
		var filtered []*sdkgraph.GraphObject
		for _, o := range objs {
			if cbgraph.StrProp(o, "domain") == domain {
				filtered = append(filtered, o)
			}
		}
		objs = filtered
	}

	// Filter rules
	var relevantRules []*sdkgraph.GraphObject
	for _, r := range rules {
		if category != "" && !strings.EqualFold(cbgraph.StrProp(r, "category"), category) {
			continue
		}
		appliesTo := cbgraph.StrProp(r, "applies_to")
		if appliesTo != "" && !containsType(appliesTo, objType) {
			continue
		}
		relevantRules = append(relevantRules, r)
	}

	if len(relevantRules) == 0 {
		fmt.Fprintf(w, "No rules found for type %q", objType)
		if category != "" {
			fmt.Fprintf(w, " category %q", category)
		}
		fmt.Fprintln(w, ". Add rules with: codebase constitution add-rule")
		return nil
	}

	// Evaluate each rule
	var evals []RuleEval
	for _, rule := range relevantRules {
		ev := RuleEval{Rule: rule}
		autoCheck := cbgraph.StrProp(rule, "auto_check")
		scanPattern := cbgraph.StrProp(rule, "scan_pattern")
		scanTarget := cbgraph.StrProp(rule, "scan_target")

		propCheckRaw := cbgraph.StrProp(rule, "prop_check")

		switch {
		case autoCheck != "":
			ev.Mode = "auto"
			rx, err := regexp.Compile(autoCheck)
			if err != nil {
				ev.Mode = "review"
				break
			}
			for _, obj := range objs {
				key := cbgraph.DerefKey(obj.Key)
				if rx.MatchString(key) {
					ev.Passes = append(ev.Passes, key)
				} else {
					ev.Fails = append(ev.Fails, key)
				}
			}

		case propCheckRaw != "":
			ev.Mode = "prop"
			var spec PropCheckSpec
			if err := json.Unmarshal([]byte(propCheckRaw), &spec); err != nil {
				ev.Mode = "review"
				break
			}
			for _, obj := range objs {
				key := cbgraph.DerefKey(obj.Key)
				val := anyPropStr(obj, spec.Field)
				ok := CheckPropSpec(spec, val)
				if ok {
					ev.Passes = append(ev.Passes, key)
				} else {
					ev.Fails = append(ev.Fails, fmt.Sprintf("%s  (%s=%q)", key, spec.Field, val))
				}
			}

		case scanPattern != "":
			ev.Mode = "scan"
			matches, samples, err := runRgScan(repo, scanPattern, scanTarget)
			if err != nil {
				ev.Mode = "review" // rg not available, fall back
			} else {
				ev.ScanMatches = matches
				ev.ScanSamples = samples
			}

		default:
			ev.Mode = "review"
		}

		evals = append(evals, ev)
	}

	if format == "json" {
		return printCheckJSON(w, evals, objs)
	}

	printCheckTable(w, evals, objs, objType)
	return nil
}

// CheckPropSpec returns true if val satisfies the PropCheckSpec.
func CheckPropSpec(spec PropCheckSpec, val string) bool {
	if spec.Bool {
		// must be exactly "true" or "false"
		return val == "true" || val == "false"
	}
	if spec.Prefix != "" {
		return strings.HasPrefix(val, spec.Prefix)
	}
	if spec.Nonempty {
		return strings.TrimSpace(val) != ""
	}
	// default: just non-empty
	return strings.TrimSpace(val) != ""
}

// runRgScan runs ripgrep and returns match count + up to 5 sample lines.
func runRgScan(repo, pattern, target string) (int, []string, error) {
	// Expand glob target relative to repo
	var paths []string
	if target != "" {
		matches, err := filepath.Glob(filepath.Join(repo, target))
		if err != nil || len(matches) == 0 {
			// Try without repo prefix (absolute or already resolved)
			matches, _ = filepath.Glob(target)
		}
		paths = matches
	}

	args := []string{"--no-heading", "-n", pattern}
	if len(paths) > 0 {
		args = append(args, paths...)
	} else {
		args = append(args, repo)
	}

	cmd := exec.Command("rg", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	_ = cmd.Run() // rg exits 1 when no matches — not an error for us

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var nonEmpty []string
	for _, l := range lines {
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	samples := nonEmpty
	if len(samples) > 5 {
		samples = samples[:5]
	}
	return len(nonEmpty), samples, nil
}

func printCheckTable(w io.Writer, evals []RuleEval, objs []*sdkgraph.GraphObject, objType string) {
	// Sort: fails first (auto+prop), then scan, then review, then passes
	sort.Slice(evals, func(i, j int) bool {
		priority := func(e RuleEval) int {
			if (e.Mode == "auto" || e.Mode == "prop") && len(e.Fails) > 0 {
				return 0
			}
			if e.Mode == "scan" {
				return 1
			}
			if e.Mode == "review" {
				return 2
			}
			return 3 // auto/prop pass
		}
		return priority(evals[i]) < priority(evals[j])
	})

	fmt.Fprintf(w, "Constitution check: %s  (%d objects · %d rules)\n", objType, len(objs), len(evals))
	fmt.Fprintln(w, strings.Repeat("─", 72))

	autoFails := 0
	autoPass := 0
	scanRules := 0
	reviewRules := 0

	for _, ev := range evals {
		rKey := cbgraph.DerefKey(ev.Rule.Key)
		stmt := cbgraph.StrProp(ev.Rule, "statement")

		switch ev.Mode {
		case "auto", "prop":
			if len(ev.Fails) == 0 {
				autoPass++
				fmt.Fprintf(w, "✓  %-42s  %d/%d pass\n", rKey, len(ev.Passes), len(ev.Passes))
			} else {
				autoFails += len(ev.Fails)
				fmt.Fprintf(w, "✗  %-42s  %d fail / %d pass\n", rKey, len(ev.Fails), len(ev.Passes))
				shown := ev.Fails
				if len(shown) > 5 {
					shown = shown[:5]
				}
				for _, f := range shown {
					fmt.Fprintf(w, "     • %s\n", f)
				}
				if len(ev.Fails) > 5 {
					fmt.Fprintf(w, "     … and %d more\n", len(ev.Fails)-5)
				}
			}

		case "scan":
			scanRules++
			scanPattern := cbgraph.StrProp(ev.Rule, "scan_pattern")
			howTo := cbgraph.StrProp(ev.Rule, "how_to_verify")
			fmt.Fprintf(w, "~  %-42s  scan: %d matches\n", rKey, ev.ScanMatches)
			fmt.Fprintf(w, "   pattern: %s\n", scanPattern)
			if ev.ScanMatches > 0 {
				fmt.Fprintf(w, "   samples:\n")
				for _, s := range ev.ScanSamples {
					// trim long lines
					if len(s) > 120 {
						s = s[:117] + "..."
					}
					fmt.Fprintf(w, "     %s\n", s)
				}
				if ev.ScanMatches > 5 {
					fmt.Fprintf(w, "     … %d more matches\n", ev.ScanMatches-5)
				}
			} else {
				fmt.Fprintf(w, "   no matches found\n")
			}
			if howTo != "" {
				fmt.Fprintf(w, "   verify: %s\n", howTo)
			}

		case "review":
			reviewRules++
			howTo := cbgraph.StrProp(ev.Rule, "how_to_verify")
			fmt.Fprintf(w, "?  %-42s\n", rKey)
			if howTo != "" {
				fmt.Fprintf(w, "   verify: %s\n", howTo)
			} else {
				if len(stmt) > 100 {
					stmt = stmt[:97] + "..."
				}
				fmt.Fprintf(w, "   rule:   %s\n", stmt)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, strings.Repeat("─", 72))
	fmt.Fprintf(w, "Summary:  ✓ %d auto-pass  ✗ %d auto-fail  ~ %d scan  ? %d review\n",
		autoPass, autoFails, scanRules, reviewRules)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Legend: ✓ auto-verified  ✗ violation found  ~ scan evidence (AI interprets)  ? AI must verify")
}

func printCheckJSON(w io.Writer, evals []RuleEval, objs []*sdkgraph.GraphObject) error {
	type jsonEval struct {
		RuleKey     string   `json:"rule_key"`
		Mode        string   `json:"mode"`
		Passes      []string `json:"passes,omitempty"`
		Fails       []string `json:"fails,omitempty"`
		PassCount   int      `json:"pass_count,omitempty"`
		FailCount   int      `json:"fail_count,omitempty"`
		ScanMatches int      `json:"scan_matches,omitempty"`
		ScanSamples []string `json:"scan_samples,omitempty"`
		HowToVerify string   `json:"how_to_verify,omitempty"`
		Statement   string   `json:"statement,omitempty"`
	}
	var out []jsonEval
	for _, ev := range evals {
		e := jsonEval{
			RuleKey:     cbgraph.DerefKey(ev.Rule.Key),
			Mode:        ev.Mode,
			ScanMatches: ev.ScanMatches,
			ScanSamples: ev.ScanSamples,
			HowToVerify: cbgraph.StrProp(ev.Rule, "how_to_verify"),
			Statement:   cbgraph.StrProp(ev.Rule, "statement"),
			PassCount:   len(ev.Passes),
			FailCount:   len(ev.Fails),
		}
		// For auto/prop: include fails (violations), omit full passes list to keep output compact
		if ev.Mode == "auto" || ev.Mode == "prop" {
			e.Fails = ev.Fails
		}
		out = append(out, e)
	}
	return output.JSONTo(w, out)
}

func containsType(appliesTo, typ string) bool {
	for _, t := range strings.Split(appliesTo, ",") {
		if strings.EqualFold(strings.TrimSpace(t), typ) {
			return true
		}
	}
	return false
}
