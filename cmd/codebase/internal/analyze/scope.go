package analyzecmd

import (
	"context"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
)

type appScope struct {
	App     *config.AppYML
	Domains map[string]bool
}

func resolveAppScope(ctx context.Context, gc *sdkgraph.Client, appName string) *appScope {
	if appName == "" {
		return nil
	}
	app := config.FindApp(appName)
	if app == nil {
		return nil
	}
	resp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
		Type:  "Domain",
		Limit: 1000,
	})
	if err != nil || resp == nil {
		return &appScope{App: app, Domains: nil}
	}
	domains := make(map[string]bool)
	for _, d := range resp.Items {
		appVal := ""
		if d.Properties != nil {
			if v, ok := d.Properties["app"]; ok {
				appVal, _ = v.(string)
			}
		}
		if appVal == "" {
			continue
		}
		if strings.EqualFold(appVal, app.RootPath) || strings.EqualFold(appVal, app.Name) {
			name := ""
			if d.Properties != nil {
				if v, ok := d.Properties["name"]; ok {
					name, _ = v.(string)
				}
			}
			if name != "" {
				domains[strings.ToLower(name)] = true
			}
		}
	}
	return &appScope{App: app, Domains: domains}
}

func (s *appScope) domainPasses(domainName string) bool {
	if s == nil || s.Domains == nil {
		return true
	}
	return s.Domains[strings.ToLower(domainName)]
}
