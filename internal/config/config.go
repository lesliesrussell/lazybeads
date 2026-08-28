// lb-1td
package config

import "time"

// Config is the fully resolved LazyBeads configuration.
type Config struct {
	General   General           `toml:"general"`
	Workspace WorkspaceSection  `toml:"workspace"`
	Ranking   Ranking           `toml:"ranking"`
	TUI       TUI               `toml:"tui"`
	Keys      Keys              `toml:"keys"`
	Aliases   map[string]string `toml:"aliases"`

	// Sources records which files contributed, for `lb doctor`.
	Sources []string `toml:"-"`
}

// General holds process-wide operator preferences.
type General struct {
	BDBinary         string   `toml:"bd_binary"`
	Actor            string   `toml:"actor"`
	Color            string   `toml:"color"`
	DefaultView      string   `toml:"default_view"`
	ConfirmMutations *bool    `toml:"confirm_mutations"`
	StaleAfter       Duration `toml:"stale_after"`
	RefreshInterval  Duration `toml:"refresh_interval"`
	Timeout          Duration `toml:"timeout"`
	UseGitIdentity   bool     `toml:"use_git_identity"`
}

// WorkspaceSection scopes and filters the work LazyBeads considers.
type WorkspaceSection struct {
	Project       string   `toml:"project"`
	BeadsDir      string   `toml:"beads_dir"`
	Rig           string   `toml:"rig"`
	FocusParent   string   `toml:"focus_parent"`
	FocusLabels   []string `toml:"focus_labels"`
	ExcludeLabels []string `toml:"exclude_labels"`
	IgnoreIssues  []string `toml:"ignore_issues"`
}

// Ranking configures the recommendation engine. Every weight is visible here so
// no factor can influence a recommendation invisibly.
type Ranking struct {
	Strategy                string  `toml:"strategy"`
	PriorityWeight          float64 `toml:"priority_weight"`
	LeverageWeight          float64 `toml:"leverage_weight"`
	AgeWeight               float64 `toml:"age_weight"`
	FocusWeight             float64 `toml:"focus_weight"`
	RecentlyUnblockedWeight float64 `toml:"recently_unblocked_weight"`
	ComplexityPenaltyWeight float64 `toml:"complexity_penalty_weight"`
	MaxGraphDepth           int     `toml:"max_graph_depth"`
	MaxGraphNodes           int     `toml:"max_graph_nodes"`
}

// TUI holds presentation preferences for the terminal client.
type TUI struct {
	Mouse            bool `toml:"mouse"`
	ASCII            bool `toml:"ascii"`
	Compact          bool `toml:"compact"`
	ShowDescriptions bool `toml:"show_descriptions"`
	ReducedMotion    bool `toml:"reduced_motion"`
}

// Keys carries user keybinding overrides keyed by scope then action.
type Keys struct {
	Layout string              `toml:"layout"`
	Global map[string][]string `toml:"global"`
	List   map[string][]string `toml:"list"`
	Detail map[string][]string `toml:"detail"`
}

// Default returns the built-in configuration, which is the lowest-precedence
// layer in the resolution chain.
func Default() Config {
	confirm := true
	return Config{
		General: General{
			BDBinary:         "bd",
			Color:            "auto",
			DefaultView:      "ready",
			ConfirmMutations: &confirm,
			StaleAfter:       Duration(8 * time.Hour),
			RefreshInterval:  Duration(30 * time.Second),
			Timeout:          Duration(30 * time.Second),
		},
		Ranking: Ranking{
			Strategy:                "balanced",
			PriorityWeight:          100,
			LeverageWeight:          15,
			AgeWeight:               1,
			FocusWeight:             25,
			RecentlyUnblockedWeight: 10,
			ComplexityPenaltyWeight: 0,
			MaxGraphDepth:           8,
			MaxGraphNodes:           500,
		},
		TUI: TUI{
			ShowDescriptions: true,
		},
		Keys:    Keys{Layout: "logical"},
		Aliases: map[string]string{},
	}
}

// ShouldConfirm reports whether mutations require interactive confirmation.
func (c Config) ShouldConfirm() bool {
	return c.General.ConfirmMutations == nil || *c.General.ConfirmMutations
}
