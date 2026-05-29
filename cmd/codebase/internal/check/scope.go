package checkcmd

import (
	"context"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
)

// appScope holds the resolved app context for a --app filter.
type appScope struct {
	App     *config.AppYML
	Domains map[string]bool // set of domain names (lowercase) belonging to this app
}

// resolveAppScope loads the app config and resolves which domains belong to it.
// If no app name is given, returns nil (no filtering).
func resolveAppScope(ctx context.Context, gc *sdkgraph.Client, appName string) *appScope {
	if appName == "" {
		return nil
	}
	app := config.FindApp(appName)
	if app == nil {
		return nil
	}
	// Find domains belonging to this app via Domain.app property
	resp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
		Type:  "Domain",
		Limit: 1000,
	})
	if err != nil || resp == nil {
		return &appScope{App: app, Domains: nil}
	}
	domains := make(map[string]bool)
	for _, d := range resp.Items {
		appVal := strProp(d, "app")
		if appVal == "" {
			continue
		}
		if strings.EqualFold(appVal, app.RootPath) || strings.EqualFold(appVal, app.Name) {
			name := strings.ToLower(strProp(d, "name"))
			if name != "" {
				domains[name] = true
			}
		}
	}
	return &appScope{App: app, Domains: domains}
}

// isAppDomain checks if a domain name belongs to the app scope.
// If scope is nil (no --app flag), all domains pass.
func (s *appScope) domainPasses(domainName string) bool {
	if s == nil || s.Domains == nil {
		return true
	}
	return s.Domains[strings.ToLower(domainName)]
}
