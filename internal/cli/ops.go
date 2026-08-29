// lb-lh5
package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

// parseSince is shared by focus/stale/activity.

func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.EqualFold(s, "today") {
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		d := now.Sub(midnight)
		if d < time.Minute {
			d = time.Minute
		}
		return d, nil
	}
	return config.ParseDuration(s)
}

func (rt *runtime) focusCmd() *cobra.Command {
	var all bool
	var recent string
	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Work most relevant to the current operator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseSince(recent)
			if err != nil {
				return &app.UsageError{Message: "invalid --recent: " + err.Error()}
			}
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			got, err := rt.svc.Focus(ctx, app.FocusRequest{Actor: rt.actor, AllActors: all, Recent: d})
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("focus", got, nil)
			}
			renderFocus(rt.out, got, app.Now())
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all-actors", false, "include every actor's claimed work")
	cmd.Flags().StringVar(&recent, "recent", "24h", "recently-touched window")
	return cmd
}

func (rt *runtime) staleCmd() *cobra.Command {
	var since string
	var prio int
	var claimed bool
	cmd := &cobra.Command{
		Use:   "stale",
		Short: "Surface quiet in-progress claims",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseSince(since)
			if err != nil {
				return &app.UsageError{Message: "invalid --since: " + err.Error()}
			}
			req := app.StaleRequest{Since: d, ClaimedOnly: claimed}
			if cmd.Flags().Changed("priority-max") {
				req.PriorityMax = &prio
			}
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			got, err := rt.svc.Stale(ctx, req)
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("stale", got, nil)
			}
			renderIssueTable(rt.out, "Stale", got.Issues, "older than "+got.Since)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "idle threshold (default config stale_after)")
	cmd.Flags().IntVar(&prio, "priority-max", 4, "keep this priority or more urgent")
	cmd.Flags().BoolVar(&claimed, "claimed-only", false, "only issues that have an assignee")
	return cmd
}

func (rt *runtime) activityCmd() *cobra.Command {
	var since, issue, actor, kind string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Operational event feed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseSince(since)
			if err != nil {
				return &app.UsageError{Message: "invalid --since: " + err.Error()}
			}
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			got, err := rt.svc.Activity(ctx, app.ActivityRequest{Since: d, IssueID: issue, Actor: actor, Type: kind})
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("activity", got, nil)
			}
			renderActivity(rt.out, got)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "how far back to look (duration or today)")
	cmd.Flags().StringVar(&issue, "issue", "", "restrict to one issue")
	cmd.Flags().StringVar(&actor, "actor", "", "restrict to one actor")
	cmd.Flags().StringVar(&kind, "type", "", "restrict to an event kind")
	return cmd
}

func (rt *runtime) memoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Project memory: list, add, search, prime, retire"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List memories", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { return rt.runMemoryList(c, "") },
	})
	cmd.AddCommand(&cobra.Command{
		Use: "search <query>", Short: "Search memories", Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return rt.runMemoryList(c, strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "add <text>", Short: "Remember a durable note", Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.MemoryAdd(c.Context(), text, opts)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "retire <id>", Short: "Forget a memory", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.MemoryRetire(c.Context(), args[0], opts)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "prime", Short: "Show Beads workflow context", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(c)
			defer cancel()
			got, err := rt.svc.MemoryPrime(ctx)
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("memory prime", got, nil)
			}
			rt.out.Print(output.Sanitize(got.Text))
			return nil
		},
	})
	return cmd
}

func (rt *runtime) runMemoryList(cmd *cobra.Command, query string) error {
	ctx, cancel := rt.ctx(cmd)
	defer cancel()
	got, err := rt.svc.MemoryList(ctx, query)
	if err != nil {
		return err
	}
	if rt.format != output.FormatHuman {
		return rt.emit("memory list", got, nil)
	}
	if len(got) == 0 {
		rt.out.Print("No memories.")
		return nil
	}
	for _, m := range got {
		id := m.ID
		if id == "" {
			id = "-"
		}
		rt.out.Print(rt.out.Style(output.StyleID, id) + "  " + output.SanitizeLine(m.Content))
	}
	return nil
}

func (rt *runtime) syncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Inspect Beads/Dolt sync status (mutating push/pull is deferred)",
		RunE:  func(c *cobra.Command, args []string) error { return rt.runSyncStatus(c) },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Read-only sync visibility", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { return rt.runSyncStatus(c) },
	})
	cmd.AddCommand(&cobra.Command{
		Use: "pull", Short: "Deferred: would run bd dolt pull", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return &app.UsageError{Message: "mutating sync is deferred from v1; use `bd dolt pull` after `lb sync status`"}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "push", Short: "Deferred: would run bd dolt push", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return &app.UsageError{Message: "mutating sync is deferred from v1; use `bd dolt push` after `lb sync status`"}
		},
	})
	return cmd
}

func (rt *runtime) runSyncStatus(cmd *cobra.Command) error {
	ctx, cancel := rt.ctx(cmd)
	defer cancel()
	got, err := rt.svc.SyncInspect(ctx)
	if err != nil {
		return err
	}
	if rt.format != output.FormatHuman {
		return rt.emit("sync status", got, nil)
	}
	if !got.Available {
		rt.out.Print("Sync: unknown (LazyBeads could not determine remote status)")
		if got.Detail != "" {
			rt.out.Note(got.Detail)
		}
		return nil
	}
	rt.out.Print("Sync")
	rt.out.Print(got.Detail)
	return nil
}

func (rt *runtime) doctorCmd() *cobra.Command {
	var fix, verbose bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose tool, workspace, and operator health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			req := app.DoctorRequest{Fix: fix, Verbose: verbose || rt.verbose}
			var report *app.DoctorReport
			var err error
			if rt.svc != nil {
				report, err = rt.svc.Doctor(ctx, req)
			} else {
				report, err = (&app.Service{Config: rt.cfg}).Doctor(ctx, req)
			}
			if err != nil {
				return err
			}
			if rt.setupErr != nil {
				report.SetupErr = rt.setupErr.Error()
				report.Health.Checks = append([]domain.HealthCheck{{
					Name: "setup", Level: domain.HealthError,
					Summary: rt.setupErr.Error(),
				}}, report.Health.Checks...)
				report.Health.Reduce()
			}
			if fix {
				// lb-uvj
				extra, ferr := writeUserCompletions(cmd.Root())
				report.Fixes = append(report.Fixes, extra...)
				if ferr != nil {
					report.Fixes = append(report.Fixes, "completion: "+ferr.Error())
				}
			}
			if rt.format != output.FormatHuman {
				return rt.emit("doctor", report, nil)
			}
			renderDoctor(rt.out, report, req.Verbose)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "write local config, cache dir, and shell completions; never mutates Beads")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "include check details")
	return cmd
}

func (rt *runtime) whyCmd() *cobra.Command {
	var depth int
	var all bool
	cmd := &cobra.Command{
		Use:   "why <id>",
		Short: "Explain why an issue is ready or blocked",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			got, err := rt.svc.Why(ctx, args[0], depth, all)
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("why", got, got.Warnings)
			}
			renderWhy(rt.out, got)
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 0, "traversal depth (0 = config default)")
	cmd.Flags().BoolVar(&all, "all-paths", false, "keep every path through a cycle")
	return cmd
}

func (rt *runtime) graphCmd() *cobra.Command {
	var dir, format string
	var depth int
	cmd := &cobra.Command{
		Use:   "graph <id>",
		Short: "Render a terminal-friendly dependency graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			gdir := app.GraphDirection(dir)
			got, err := rt.svc.BuildGraph(ctx, app.GraphRequest{RootID: args[0], Direction: gdir, MaxDepth: depth})
			if err != nil {
				return err
			}
			switch strings.ToLower(format) {
			case "json":
				return rt.emit("graph", got, nil)
			case "dot":
				rt.out.Print(graphDOT(got))
				return nil
			default:
				if rt.format == output.FormatJSON {
					return rt.emit("graph", got, nil)
				}
				renderGraphTree(rt.out, got)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&dir, "direction", "both", "blockers, dependents, or both")
	cmd.Flags().IntVar(&depth, "depth", 0, "traversal depth")
	cmd.Flags().StringVar(&format, "format", "tree", "tree, dot, or json")
	return cmd
}

func (rt *runtime) blockedCmd() *cobra.Command {
	var prio int
	var group string
	cmd := &cobra.Command{
		Use:   "blocked",
		Short: "Blocked work grouped by closest unresolved blocker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := app.BlockedRequest{GroupBy: group}
			if cmd.Flags().Changed("priority-max") {
				req.PriorityMax = &prio
			}
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			got, err := rt.svc.Blocked(ctx, req)
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("blocked", got, nil)
			}
			renderBlocked(rt.out, got)
			return nil
		},
	}
	cmd.Flags().IntVar(&prio, "priority-max", 4, "keep this priority or more urgent")
	cmd.Flags().StringVar(&group, "group-by", "blocker", "blocker or parent")
	return cmd
}
