package config

// SyncRoutesConfig configures the route extractor for `codebase sync routes`.
type SyncRoutesConfig struct {
	Framework     string `yaml:"framework"`
	Command       string `yaml:"command"`
	Runtime       string `yaml:"runtime"`
	Glob          string `yaml:"glob"`
	DomainSegment int    `yaml:"domain_segment"`
}

// SyncViewsConfig configures `codebase sync views`.
type SyncViewsConfig struct {
	Glob       string   `yaml:"glob"`
	RoutesFile string   `yaml:"routes_file"`
	Platform   []string `yaml:"platform"`
}

// SyncComponentsConfig configures `codebase sync components`.
type SyncComponentsConfig struct {
	Glob string `yaml:"glob"`
}

// SyncActionsConfig configures `codebase sync actions`.
type SyncActionsConfig struct {
	Glob    string `yaml:"glob"`
	Pattern string `yaml:"pattern"`
}

// SyncScenariosConfig configures `codebase sync scenarios`.
type SyncScenariosConfig struct {
	File       string `yaml:"file"`
	RouterFile string `yaml:"router_file"`
	StoreGlob  string `yaml:"store_glob"`
}

// SyncConfig groups all sync sub-command configuration.
type SyncConfig struct {
	Routes     SyncRoutesConfig     `yaml:"routes"`
	Views      SyncViewsConfig      `yaml:"views"`
	Components SyncComponentsConfig `yaml:"components"`
	Actions    SyncActionsConfig    `yaml:"actions"`
	Scenarios  SyncScenariosConfig  `yaml:"scenarios"`
}

// CodebaseYML is the structure of .codebase.yml.
type CodebaseYML struct {
	Project   string     `yaml:"project"`
	ProjectID string     `yaml:"project_id"`
	Server    string     `yaml:"server"`
	Sync      SyncConfig `yaml:"sync"`
}
