// lb-58x
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

func (rt *runtime) statusCmd() *cobra.Command {
	var watch string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Workspace health and work-state overview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch == "" {
				return rt.runStatus(cmd)
			}
			d, err := config.ParseDuration(watch)
			if err != nil {
				return &app.UsageError{Message: "invalid --watch duration: " + err.Error()}
			}
			return rt.watch(cmd, d, rt.runStatus)
		},
	}
	cmd.Flags().StringVar(&watch, "watch", "", "refresh every duration, for example 5s")
	return cmd
}

func (rt *runtime) runStatus(cmd *cobra.Command) error {
	ctx, cancel := rt.ctx(cmd)
	defer cancel()
	report, err := rt.svc.Status(ctx)
	if err != nil {
		return err
	}
	if rt.format != output.FormatHuman {
		return rt.emit("status", report, nil)
	}
	renderStatus(rt.out, report)
	return nil
}

func (rt *runtime) readyCmd() *cobra.Command {
	var (
		priorityMax int
		labels      []string
		parent      string
		sortName    string
		limit       int
	)
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Show unblocked work, ranked for an operator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := app.ReadyRequest{Labels: labels, Parent: parent, Sort: sortName, Limit: limit}
			if cmd.Flags().Changed("priority-max") {
				req.PriorityMax = &priorityMax
			}
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			result, err := rt.svc.Ready(ctx, req)
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("ready", result, nil)
			}
			renderReady(rt.out, result, app.Now())
			return nil
		},
	}
	cmd.Flags().IntVar(&priorityMax, "priority-max", 4, "keep issues at this priority or more urgent")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "require this label (AND when repeated)")
	cmd.Flags().StringVar(&parent, "parent", "", "restrict to children of this issue")
	cmd.Flags().StringVar(&sortName, "sort", "priority", "sort: priority, leverage, age")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum issues to print")
	return cmd
}

// lb-rd7
func (rt *runtime) nextCmd() *cobra.Command {
	var (
		strategy string
		limit    int
		parent   string
	)
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Recommend the highest-leverage ready task and explain why",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			result, err := rt.svc.Next(ctx, app.NextRequest{
				Strategy: strategy,
				Limit:    limit,
				Parent:   parent,
			})
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("next", result, nil)
			}
			renderNext(rt.out, result, app.Now(), rt.verbose)
			return nil
		},
	}
	cmd.Flags().StringVar(&strategy, "strategy", "", "ranking strategy: priority, leverage, age, balanced, random")
	cmd.Flags().IntVar(&limit, "limit", 0, "how many ranked tasks to show (default 1)")
	cmd.Flags().StringVar(&parent, "parent", "", "restrict recommendations to this epic or parent")
	return cmd
}

func (rt *runtime) showCmd() *cobra.Command {
	var events, raw bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Render one issue in an action-oriented format",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			result, err := rt.svc.Show(ctx, args[0], app.ShowRequest{Events: events})
			if err != nil {
				return err
			}
			if raw {
				if len(result.Detail.Raw) > 0 {
					rt.out.Print(string(result.Detail.Raw))
					return nil
				}
				return rt.emit("show", result.Detail, nil)
			}
			if rt.format != output.FormatHuman {
				return rt.emit("show", result, nil)
			}
			renderShow(rt.out, result, app.Now(), events)
			return nil
		},
	}
	cmd.Flags().BoolVar(&events, "events", false, "include recent history")
	cmd.Flags().BoolVar(&raw, "raw", false, "print the upstream JSON payload")
	return cmd
}

func (rt *runtime) listCmd() *cobra.Command {
	var req struct {
		status, typ, assignee, query string
		labels, labelsAny            []string
		createdAfter, updatedBefore  string
		all                          bool
		limit                        int
	}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Filter issues with Beads-compatible constraints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			result, err := rt.svc.List(ctx, app.ListRequest{
				Status:        req.status,
				Type:          req.typ,
				Labels:        req.labels,
				LabelsAny:     req.labelsAny,
				Assignee:      req.assignee,
				Query:         req.query,
				CreatedAfter:  req.createdAfter,
				UpdatedBefore: req.updatedBefore,
				All:           req.all,
				Limit:         req.limit,
			})
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("list", result, nil)
			}
			renderIssueTable(rt.out, "List", result.Issues, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&req.status, "status", "", "filter by status")
	cmd.Flags().StringVar(&req.typ, "type", "", "filter by type")
	cmd.Flags().StringArrayVar(&req.labels, "label", nil, "require this label (AND when repeated)")
	cmd.Flags().StringArrayVar(&req.labelsAny, "label-any", nil, "match any of these labels (OR)")
	cmd.Flags().StringVar(&req.assignee, "assignee", "", "filter by assignee (`me` resolves the current actor)")
	cmd.Flags().StringVar(&req.query, "query", "", "local full-text filter after Beads returns candidates")
	cmd.Flags().StringVar(&req.createdAfter, "created-after", "", "keep issues created after this time")
	cmd.Flags().StringVar(&req.updatedBefore, "updated-before", "", "keep issues updated before this time")
	cmd.Flags().BoolVar(&req.all, "all", false, "disable the default safety limit and include closed work")
	cmd.Flags().IntVar(&req.limit, "limit", 0, "maximum issues to print")
	return cmd
}

func (rt *runtime) searchCmd() *cobra.Command {
	var all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Fuzzy discovery across issue fields",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			ctx, cancel := rt.ctx(cmd)
			defer cancel()
			result, err := rt.svc.Search(ctx, app.SearchRequest{Query: query, All: all, Limit: limit})
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("search", result, nil)
			}
			renderIssueTable(rt.out, "Search", result.Issues, fmt.Sprintf("%d total for %q", result.Total, result.Query))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include closed issues")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results (default 20)")
	return cmd
}

func (rt *runtime) watch(cmd *cobra.Command, interval time.Duration, fn func(*cobra.Command) error) error {
	if interval <= 0 {
		return &app.UsageError{Message: "--watch duration must be positive"}
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := fn(cmd); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := fn(cmd); err != nil {
				return err
			}
		}
	}
}
