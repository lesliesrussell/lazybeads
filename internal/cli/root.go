// lb-58x
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/output"
	"github.com/lesliesrussell/lazybeads/internal/version"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// Options wires IO and optional test doubles into the CLI.
type Options struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Args    []string
	Service *app.Service
	Client  beads.Client
}

type runtime struct {
	opts Options

	format  output.Format
	json    bool
	color   string
	ascii   bool
	quiet   bool
	verbose bool
	debug   bool

	project  string
	beadsDir string
	rig      string
	actor    string
	timeout  string

	cfg config.Config
	svc *app.Service
	out *output.Writer
}

// Execute runs the CLI and returns a specification exit code.
func Execute(opts Options) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	rt := &runtime{opts: opts}
	cmd := rt.root()
	if opts.Args != nil {
		cmd.SetArgs(opts.Args)
	}
	cmd.SetIn(opts.Stdin)
	cmd.SetOut(opts.Stdout)
	cmd.SetErr(opts.Stderr)
	if err := cmd.Execute(); err != nil {
		return rt.handleError(err)
	}
	return 0
}

func (rt *runtime) root() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "lb",
		Short:         "The terminal operator console for Beads.",
		Long:          "LazyBeads helps an operator discover the highest-leverage Beads task, understand why, and act on it safely.",
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "version" || cmd.Name() == "help" {
				return nil
			}
			return rt.setup(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return &app.UsageError{Message: "a command is required; try `lb status` or `lb ready`"}
		},
	}
	cmd.SetVersionTemplate("lazybeads {{.Version}}\n")

	f := cmd.PersistentFlags()
	f.BoolVar(&rt.json, "json", false, "emit the stable JSON envelope")
	f.String("format", "human", "output format: human, json, jsonl")
	f.StringVar(&rt.color, "color", "", "color mode: auto, always, never")
	f.Bool("no-color", false, "disable color (same as --color never)")
	f.BoolVar(&rt.ascii, "ascii", false, "use ASCII glyphs instead of Unicode")
	f.BoolVar(&rt.quiet, "quiet", false, "suppress secondary commentary")
	f.BoolVar(&rt.verbose, "verbose", false, "include upstream diagnostic details")
	f.BoolVar(&rt.debug, "debug", false, "trace bd invocations with secrets redacted")
	f.StringVar(&rt.project, "project", "", "repository path to resolve the Beads workspace from")
	f.StringVar(&rt.beadsDir, "beads-dir", "", "explicit Beads directory")
	f.StringVar(&rt.rig, "rig", "", "cross-rig selection")
	f.StringVar(&rt.actor, "actor", "", "operator identity")
	f.StringVar(&rt.timeout, "timeout", "", "bd command timeout (for example 30s)")

	cmd.AddCommand(rt.statusCmd())
	cmd.AddCommand(rt.readyCmd())
	cmd.AddCommand(rt.showCmd())
	cmd.AddCommand(rt.listCmd())
	cmd.AddCommand(rt.searchCmd())
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the LazyBeads version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(rt.opts.Stdout, "lazybeads %s\n", version.Version)
		},
	})
	return cmd
}

func (rt *runtime) setup(cmd *cobra.Command) error {
	formatFlag, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	if rt.json {
		rt.format = output.FormatJSON
	} else {
		format, err := output.ParseFormat(formatFlag)
		if err != nil {
			return &app.UsageError{Message: err.Error()}
		}
		rt.format = format
	}
	color := rt.color
	if noColor {
		color = "never"
	}

	if rt.opts.Service != nil {
		rt.svc = rt.opts.Service
		rt.cfg = rt.svc.Config
	} else if err := rt.resolveWorkspace(cmd.Context()); err != nil {
		rt.out = output.NewWriter(rt.opts.Stdout, rt.opts.Stderr, output.Options{
			Format: rt.format, Color: firstNonEmpty(color, "auto"), ASCII: rt.ascii, Quiet: rt.quiet, Verbose: rt.verbose, Debug: rt.debug,
		})
		return err
	}

	if color == "" {
		color = rt.cfg.General.Color
	}
	rt.out = output.NewWriter(rt.opts.Stdout, rt.opts.Stderr, output.Options{
		Format:  rt.format,
		Color:   firstNonEmpty(color, "auto"),
		ASCII:   rt.ascii || rt.cfg.TUI.ASCII,
		Quiet:   rt.quiet,
		Verbose: rt.verbose,
		Debug:   rt.debug,
	})
	return nil
}

func (rt *runtime) resolveWorkspace(ctx context.Context) error {
	startDir := rt.project
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	cfg, err := config.Load(startDir)
	if err != nil {
		return err
	}
	if rt.timeout != "" {
		d, err := config.ParseDuration(rt.timeout)
		if err != nil {
			return &app.UsageError{Message: "invalid --timeout: " + err.Error()}
		}
		cfg.General.Timeout = config.Duration(d)
	}
	rt.cfg = cfg

	actor, _ := workspace.ResolveActor(rt.actor, cfg.General.Actor, cfg.General.UseGitIdentity, startDir)
	runner := beads.NewRunner(cfg.General.BDBinary)
	runner.Debug = rt.debug
	client := rt.opts.Client
	if client == nil {
		client = beads.NewCLI(runner, actor)
	}

	sel := workspace.Selection{
		BeadsDir:       rt.beadsDir,
		Project:        rt.project,
		Rig:            rt.rig,
		WorkingDir:     startDir,
		ConfigProject:  cfg.Workspace.Project,
		ConfigBeadsDir: cfg.Workspace.BeadsDir,
		ConfigRig:      cfg.Workspace.Rig,
	}
	ws, err := workspace.Resolve(ctx, client, sel, cfg.General.BDBinary)
	if err != nil {
		return err
	}
	rt.svc = app.NewService(client, cfg, ws, actor, workspace.NewInProcessLocks())
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (rt *runtime) ctx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	timeout := rt.cfg.General.Timeout.Duration()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(cmd.Context(), timeout)
}
