// lb-58x
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

func (rt *runtime) handleError(err error) int {
	code := domain.ExitRuntime
	payload := domain.ErrorPayload{
		Code:    "runtime",
		Message: err.Error(),
	}

	var usage *app.UsageError
	if errors.As(err, &usage) {
		code = domain.ExitUsage
		payload.Code = "usage"
		payload.Message = usage.Message
	} else if ce, ok := beads.AsCommandError(err); ok {
		code = ce.ExitCode2()
		payload.Code = string(ce.Kind)
		payload.Message = ce.Message()
		payload.Hint = ce.UserHint()
		if ce.Stderr != "" {
			s := ce.Stderr
			payload.UpstreamStderr = &s
		} else if rt.verbose && ce.Stdout != "" {
			s := ce.Stdout
			payload.UpstreamStderr = &s
		}
	} else if isUsageMessage(err.Error()) {
		code = domain.ExitUsage
		payload.Code = "usage"
		payload.Message = err.Error()
	}

	if rt.format == output.FormatJSON || rt.json {
		if rt.out != nil {
			_ = rt.out.EmitJSONError(commandName(err), payload)
		} else {
			fmt.Fprintf(rt.opts.Stdout, "{\"schema_version\":%d,\"error\":{\"code\":%q,\"message\":%q}}\n",
				domain.SchemaVersion, payload.Code, payload.Message)
		}
		return code
	}

	dest := rt.opts.Stderr
	fmt.Fprintln(dest, payload.Message)
	if payload.Hint != "" {
		fmt.Fprintln(dest, payload.Hint)
	}
	if rt.verbose && payload.UpstreamStderr != nil {
		fmt.Fprintln(dest, *payload.UpstreamStderr)
	}
	rt.printDebugTraces()
	return code
}

func (rt *runtime) printDebugTraces() {
	if !rt.debug || rt.svc == nil || rt.svc.Client == nil {
		return
	}
	runner := rt.svc.Client.Runner()
	if runner == nil {
		return
	}
	for _, tr := range runner.Traces() {
		fmt.Fprintf(rt.opts.Stderr, "bd %s (exit %d, %s)\n", strings.Join(tr.Args, " "), tr.ExitCode, tr.Duration)
	}
}

func commandName(err error) string {
	if ce, ok := beads.AsCommandError(err); ok && ce.Operation != "" {
		return ce.Operation
	}
	return ""
}

func isUsageMessage(msg string) bool {
	m := strings.ToLower(msg)
	for _, n := range []string{
		"arg(s)", "accepts", "unknown command", "unknown flag",
		"unknown shorthand", "required flag", "invalid",
	} {
		if strings.Contains(m, n) {
			return true
		}
	}
	return false
}
