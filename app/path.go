package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// widenPath adds the user's login-shell PATH to this process's. An app started
// from Finder/Dock gets only /usr/bin:/bin:/usr/sbin:/sbin, so `claude`, node,
// pnpm and Homebrew would not be found, by us or by the agents we start (which
// inherit our environment). On failure the PATH stays as it is.
func widenPath() {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // the window waits on this
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '<<PATH>>%s<<PATH>>' "$PATH"`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // no controlling tty: an interactive shell must not grab it
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return
	}
	// The markers skip whatever the shell's rc files print.
	if _, rest, ok := strings.Cut(string(out), "<<PATH>>"); ok {
		if login, _, ok := strings.Cut(rest, "<<PATH>>"); ok {
			os.Setenv("PATH", mergePath(login, os.Getenv("PATH")))
		}
	}
}

// mergePath returns the entries of first, then those of second not already in it.
func mergePath(first, second string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(first+string(os.PathListSeparator)+second, string(os.PathListSeparator)) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}
