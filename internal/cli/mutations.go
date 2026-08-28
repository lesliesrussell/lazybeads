// lb-rc1
package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

func (rt *runtime) createCmd() *cobra.Command {
	in := beads.CreateIssueInput{}
	var labels []string
	var metadata []string
	var descFile string
	var prio int
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a Beads issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Title = args[0]
			in.Labels = labels
			in.Priority = &prio
			if descFile != "" {
				body, err := os.ReadFile(descFile)
				if err != nil {
					return &app.UsageError{Message: "reading --description-file: " + err.Error()}
				}
				in.Description = string(body)
			}
			in.Metadata = parseMetadataFlags(metadata)
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Create(cmd.Context(), in, opts)
			})
		},
	}
	cmd.Flags().StringVar(&in.Type, "type", "task", "issue type")
	cmd.Flags().IntVar(&prio, "priority", 2, "priority 0-4")
	cmd.Flags().StringVar(&in.Description, "description", "", "issue description")
	cmd.Flags().StringVar(&descFile, "description-file", "", "read description from a file")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label (repeatable)")
	cmd.Flags().StringVar(&in.Assignee, "assignee", "", "assignee")
	cmd.Flags().StringVar(&in.Parent, "parent", "", "parent issue")
	cmd.Flags().StringArrayVar(&metadata, "metadata", nil, "key=value metadata")
	return cmd
}

func (rt *runtime) editCmd() *cobra.Command {
	var (
		title, desc, descFile, typ string
		priority                   int
		labels                     []string
		openEditor                 bool
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Update fields on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := beads.UpdateIssueInput{}
			if cmd.Flags().Changed("title") {
				in.Title = &title
			}
			if cmd.Flags().Changed("description") {
				in.Description = &desc
			}
			if descFile != "" {
				body, err := os.ReadFile(descFile)
				if err != nil {
					return &app.UsageError{Message: "reading --description-file: " + err.Error()}
				}
				s := string(body)
				in.Description = &s
			}
			if cmd.Flags().Changed("type") {
				in.Type = &typ
			}
			if cmd.Flags().Changed("priority") {
				in.Priority = &priority
			}
			for _, l := range labels {
				switch {
				case strings.HasPrefix(l, "add:"):
					in.AddLabels = append(in.AddLabels, strings.TrimPrefix(l, "add:"))
				case strings.HasPrefix(l, "remove:"):
					in.RemoveLabels = append(in.RemoveLabels, strings.TrimPrefix(l, "remove:"))
				default:
					in.AddLabels = append(in.AddLabels, l)
				}
			}
			if openEditor {
				updated, err := rt.editInEditor(cmd, args[0])
				if err != nil {
					return err
				}
				in = updated
			}
			if in.Title == nil && in.Description == nil && in.Type == nil && in.Priority == nil &&
				len(in.AddLabels) == 0 && len(in.RemoveLabels) == 0 && !openEditor {
				return &app.UsageError{Message: "edit requires a field flag or --open-in-editor"}
			}
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Edit(cmd.Context(), args[0], in, opts)
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&desc, "description", "", "new description")
	cmd.Flags().StringVar(&descFile, "description-file", "", "read description from a file")
	cmd.Flags().StringVar(&typ, "type", "", "new type")
	cmd.Flags().IntVar(&priority, "priority", 2, "new priority")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "add:NAME or remove:NAME")
	cmd.Flags().BoolVar(&openEditor, "open-in-editor", false, "edit YAML frontmatter in $VISUAL/$EDITOR")
	return cmd
}

func (rt *runtime) claimCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claim <id>",
		Short: "Atomically claim an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Claim(cmd.Context(), args[0], opts)
			})
		},
	}
}

func (rt *runtime) unclaimCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unclaim <id>",
		Short: "Release a claim when Beads supports it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Unclaim(cmd.Context(), args[0], opts)
			})
		},
	}
}

func (rt *runtime) closeCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "close <id> [reason]",
		Short: "Close an issue; reason is required",
		Args:  cobra.RangeArgs(1, 20),
		RunE: func(cmd *cobra.Command, args []string) error {
			if reason == "" && len(args) > 1 {
				reason = strings.Join(args[1:], " ")
			}
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Close(cmd.Context(), args[0], reason, opts)
			})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "close reason")
	return cmd
}

func (rt *runtime) reopenCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "reopen <id>",
		Short: "Reopen a closed issue; reason is required",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Reopen(cmd.Context(), args[0], reason, opts)
			})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reopen reason")
	return cmd
}

func (rt *runtime) assignCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "assign <id> [actor]",
		Short: "Record responsibility without claiming",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			actor := ""
			if len(args) > 1 {
				actor = args[1]
			}
			if clear {
				actor = ""
			} else if actor == "" {
				return &app.UsageError{Message: "assign requires an actor or --clear"}
			}
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.Assign(cmd.Context(), args[0], actor, opts)
			})
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the assignee")
	return cmd
}

func (rt *runtime) depCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Add, remove, list, or validate dependencies",
	}
	var rel string
	add := &cobra.Command{
		Use:   "add <child> <parent>",
		Short: "Make child wait on parent. Arguments are never reordered.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := domain.NormalizeRelation(rel)
			if rel == "" {
				t = domain.RelBlocks
			}
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.DepAdd(cmd.Context(), args[0], args[1], t, opts)
			})
		},
	}
	add.Flags().StringVar(&rel, "type", "blocks", "relation type (default blocks; shown explicitly)")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{
		Use:   "remove <child> <parent>",
		Short: "Remove a dependency edge",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return rt.runMutation(func(dry bool) (*app.MutationResult, error) {
				opts := rt.optsMutate()
				opts.DryRun = dry
				return rt.svc.DepRemove(cmd.Context(), args[0], args[1], opts)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list <id>",
		Short: "List blockers and dependents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			down, up, err := rt.svc.DepList(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data := map[string]any{"blockers": down, "dependents": up}
			if rt.format != output.FormatHuman {
				return rt.emit("dep list", data, nil)
			}
			renderDepList(rt.out, args[0], down, up)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Report cycles without mutating anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := rt.svc.DepValidate(cmd.Context())
			if err != nil {
				return err
			}
			if rt.format != output.FormatHuman {
				return rt.emit("dep validate", report, report.Warnings)
			}
			renderDepValidate(rt.out, report)
			return nil
		},
	})
	return cmd
}

func parseMetadataFlags(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			out[p] = ""
			continue
		}
		out[k] = v
	}
	return out
}
