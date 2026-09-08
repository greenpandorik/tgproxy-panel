package agent

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ui prints the step style shared by install.sh and the node install script
// (.superpowers/sdd/phase6/ui-style.md), so `tgwp-agent upgrade` reads as the same product:
// bold "==> Title" sections, "  ✔ done", "  ✘ failed" on stderr, "  ! warning", indented dim
// detail, and a waiting line that ends as a ✔ or a ✘.
//
// Colors appear only on a terminal with NO_COLOR unset; the marks fall back to [ok]/[x]/[..]
// when the locale is not UTF-8, exactly as the two shell scripts do.
type ui struct {
	out, errw io.Writer
	tty       bool

	markOK, markFail, markWait                string
	green, red, yellow, dim, bold, reset, esc string
}

func newUI(out, errw io.Writer, tty bool, getenv func(string) string) *ui {
	u := &ui{out: out, errw: errw, tty: tty, markOK: "[ok]", markFail: "[x]", markWait: "[..]"}
	locale := getenv("LC_ALL")
	if locale == "" {
		locale = getenv("LC_CTYPE")
	}
	if locale == "" {
		locale = getenv("LANG")
	}
	l := strings.ToLower(locale)
	if strings.Contains(l, "utf-8") || strings.Contains(l, "utf8") {
		u.markOK, u.markFail, u.markWait = "✔", "✘", "…"
	}
	if tty && getenv("NO_COLOR") == "" {
		u.green, u.red, u.yellow = "\033[32m", "\033[31m", "\033[33m"
		u.dim, u.bold, u.reset = "\033[2m", "\033[1m", "\033[0m"
		u.esc = "\r\033[K"
	}
	return u
}

// print is the one place output is written: an unwritable stdout is not something a node
// upgrade can do anything about, so the error is dropped here rather than at every call site.
func (u *ui) print(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func (u *ui) step(format string, a ...any) {
	u.print(u.out, "\n%s==> %s%s\n", u.bold, fmt.Sprintf(format, a...), u.reset)
}

func (u *ui) ok(format string, a ...any) {
	u.print(u.out, "  %s%s%s %s\n", u.green, u.markOK, u.reset, fmt.Sprintf(format, a...))
}

func (u *ui) fail(format string, a ...any) {
	u.print(u.errw, "  %s%s%s %s\n", u.red, u.markFail, u.reset, fmt.Sprintf(format, a...))
}

func (u *ui) warn(format string, a ...any) {
	u.print(u.out, "  %s!%s %s\n", u.yellow, u.reset, fmt.Sprintf(format, a...))
}

func (u *ui) info(format string, a ...any) {
	u.print(u.out, "    %s%s%s\n", u.dim, fmt.Sprintf(format, a...), u.reset)
}

// waitFor polls probe once a second for up to budget. On a terminal the line is redrawn with
// the elapsed time every 5s; in a log it is one line at the start and one at the end, so a
// journal never fills with carriage returns. It is the Go twin of the scripts' wait_for.
func (u *ui) waitFor(label string, budget time.Duration, sleep func(time.Duration), probe func() bool) bool {
	secs := int(budget / time.Second)
	if u.tty {
		u.print(u.out, "  %s %s", u.markWait, label)
	} else {
		u.print(u.out, "  %s %s (up to %ds)\n", u.markWait, label, secs)
	}
	for i := 0; ; i++ {
		if probe() {
			u.print(u.out, "%s", u.esc)
			u.ok("%s (%ds)", label, i)
			return true
		}
		if i >= secs {
			break
		}
		sleep(time.Second)
		if u.tty && (i+1)%5 == 0 {
			u.print(u.out, "%s  %s %s (%ds)", u.esc, u.markWait, label, i+1)
		}
	}
	u.print(u.out, "%s", u.esc)
	u.fail("%s: still not ready after %ds", label, secs)
	return false
}

// isTTY reports whether f is a terminal, which decides colors and whether there is anybody to
// prompt. A file, a pipe or a systemd journal is not.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
