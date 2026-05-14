package shellintegration

// Plan describes how to spawn a shell with OSC 133 hooks injected.
// ExtraEnv is appended to the child's environment; ExtraArgs is appended
// to argv after the shell binary. Cleanup is non-nil when temporary files
// need removal at session close; callers MUST nil-check before invoking.
// Shell is purely informational (used in logs).
type Plan struct {
	ExtraEnv  []string
	ExtraArgs []string
	Cleanup   func()
	Shell     string
}

// Prepare returns a Plan for the given shell. If enabled is false, the path
// is empty, or the shell is unsupported, Prepare returns a zero Plan. The
// sessionID is used to scope temporary files so concurrent sessions do not
// collide. Prepare never returns an error: internal failures (mkdir, write)
// yield a zero Plan plus a one-time log line.
func Prepare(shellPath string, enabled bool, sessionID string) Plan {
	if !enabled {
		return Plan{}
	}
	switch DetectShell(shellPath) {
	case ShellZsh:
		return prepareZsh(sessionID)
	case ShellBash:
		return prepareBash(sessionID)
	case ShellFish:
		return prepareFish()
	case ShellPwsh:
		return preparePwsh(sessionID)
	default:
		return Plan{}
	}
}
