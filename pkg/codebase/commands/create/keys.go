package create

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ContextKey(name string) string {
	return "ctx-" + Slugify(name)
}

func UIComponentKey(name string) string {
	return "ui-" + Slugify(name)
}

func HelperKey(name string) string {
	slug := Slugify(name)
	slug = strings.TrimPrefix(slug, "use-")
	return "hook-" + slug
}

func ActionKey(domain, name string) string {
	domainSlug := Slugify(domain)
	nameSlug := CamelToSlug(name)
	nameParts := strings.Split(nameSlug, "-")
	if len(nameParts) > 3 {
		nameParts = nameParts[:3]
	}
	return "act-" + domainSlug + "-" + strings.Join(nameParts, "-")
}

func APIEndpointKey(domain, handler string) string {
	handlerSlug := Slugify(CamelToSlug(handler))
	if domain != "" {
		domainPrefix := Slugify(domain) + "-"
		if strings.HasPrefix(handlerSlug, domainPrefix) {
			handlerSlug = strings.TrimPrefix(handlerSlug, domainPrefix)
		}
	}
	return "ep-" + handlerSlug
}

func SourceFileKey(path string) string {
	p := filepath.ToSlash(path)
	p = strings.TrimPrefix(p, "apps/")
	p = strings.TrimPrefix(p, "libs/")
	p = strings.ReplaceAll(p, "/src/", "/")
	ext := filepath.Ext(p)
	p = strings.TrimSuffix(p, ext)

	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(p)
	key = strings.ToLower(key)
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	return "sf-" + strings.Trim(key, "-")
}

func DomainKey(slug string) string {
	return "dom-" + Slugify(slug)
}

func ScenarioKey(name string) string {
	return "scn-" + Slugify(name)
}

func ScenarioStepKey(scenarioKey string, order int) string {
	prefix := strings.TrimPrefix(scenarioKey, "scn-")
	parts := strings.Split(prefix, "-")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return fmt.Sprintf("step-%s-%d", strings.Join(parts, "-"), order)
}

func Slugify(s string) string {
	s = CamelToSlug(s)
	s = strings.ToLower(s)
	s = strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

func CamelToSlug(s string) string {
	var result []rune
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := runes[i-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
				result = append(result, '-')
			}
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}
