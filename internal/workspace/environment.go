// lb-1td
package workspace

import (
	"os"
	"os/exec"
	"strings"
)

// ResolveActor determines the effective operator identity in the documented
// order: explicit flag, LB_ACTOR, configuration, then git identity only when
// the user has intentionally enabled it. No identity is ever assumed.
func ResolveActor(flagActor, configActor string, useGitIdentity bool, projectDir string) (string, string) {
	if v := strings.TrimSpace(flagActor); v != "" {
		return v, "--actor"
	}
	if v := strings.TrimSpace(os.Getenv("LB_ACTOR")); v != "" {
		return v, "LB_ACTOR"
	}
	if v := strings.TrimSpace(configActor); v != "" {
		return v, "config"
	}
	if useGitIdentity {
		if v := gitConfig(projectDir, "user.name"); v != "" {
			return v, "git user.name"
		}
		if v := gitConfig(projectDir, "user.email"); v != "" {
			return v, "git user.email"
		}
	}
	return "", ""
}

func gitConfig(dir, key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// EditorCommand resolves the editor to use for --open-in-editor, honouring
// $VISUAL before $EDITOR.
func EditorCommand() []string {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			// The value may carry arguments (e.g. "code -w"); split on spaces
			// and execute via argv, never through a shell.
			return strings.Fields(v)
		}
	}
	return nil
}
