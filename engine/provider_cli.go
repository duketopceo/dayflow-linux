package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// A `cli` provider execve's a subscription-auth'd agent CLI (cursor-agent,
// opencode) instead of calling an HTTP endpoint. The exec boundary is
// hardened (see PRIVACY.md): argv-only invocation with no shell; untrusted
// prompt text rides on stdin — or strictly after a `--` terminator via the
// {prompt} placeholder — never where it could parse as a flag; a deny-list
// of permission-escalation flags is enforced on the final computed argv; the
// child environment is a minimal allowlist so the daemon's env (provider API
// keys included) is not inherited; cwd is a per-invocation scratch dir under
// the system temp; and a context timeout bounds every call.
//
// Residual: flag denial constrains dayflow's argv, not the CLI's own tool
// permission profile — the subprocess can still act within whatever its own
// configuration allows, and forwards prompts (and any frames it reads) to its
// configured model backend, typically cloud.

const (
	// defaultCLITimeoutSec bounds one CLI invocation; cursor-agent/opencode
	// calls run minutes-scale, so this sits above the HTTP default.
	defaultCLITimeoutSec = 180
	cliMaxOutputBytes    = 4 << 20  // cap captured stdout/stderr
	cliMaxFrameBytes     = 64 << 20 // cap a single staged frame copy
)

// cliDenyFlags are permission-escalation flags across the supported CLIs —
// cursor-agent's --force/--yolo, opencode's --auto, plus common equivalents.
// Enforced on the args template at config-write time and again on the final
// computed argv at exec time, so a hand-edited config cannot sneak them in.
var cliDenyFlags = map[string]bool{
	"--yolo": true, "--force": true, "--auto": true, "-f": true,
	"--yes": true, "-y": true, "--auto-approve": true,
	"--approve-all": true, "--allow-all": true, "--full-auto": true,
	"--no-confirm": true, "--skip-permissions": true,
	"--dangerously-skip-permissions": true,
}

// cliEnvDeny lists env vars env_passthrough may not carry: the managed
// allowlist names, plus loader hooks that could inject code into the child.
var cliEnvDeny = map[string]bool{
	"PATH": true, "HOME": true, "LANG": true, "TMPDIR": true,
	"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "LD_AUDIT": true,
	"DYLD_INSERT_LIBRARIES": true, "DYLD_FALLBACK_LIBRARY_PATH": true,
}

// cliPresetArgs returns a sensible args template for a known CLI, applied
// only when the user sets `command` and has not configured `args`. Both
// presets take the prompt on stdin: cursor-agent's --print mode reads piped
// stdin, and `opencode run` appends piped stdin to the prompt.
func cliPresetArgs(command string) []string {
	switch filepath.Base(command) {
	case "cursor-agent":
		return []string{"--print", "--output-format", "text"}
	case "opencode":
		return []string{"run"}
	}
	return nil
}

// cliArgDenied reports whether an argv token is a denied flag. Long flags are
// matched on their `--name` base so `--yolo=true` is caught; short-flag
// bundles (`-fy`) are checked per character.
func cliArgDenied(arg string) bool {
	base := arg
	if strings.HasPrefix(arg, "--") {
		if i := strings.Index(arg, "="); i >= 0 {
			base = arg[:i]
		}
		if cliDenyFlags[base] {
			return true
		}
		return false
	}
	if strings.HasPrefix(arg, "-") && len(arg) > 1 {
		for _, c := range arg[1:] {
			if c == '=' {
				break
			}
			if cliDenyFlags["-"+string(c)] {
				return true
			}
		}
	}
	return false
}

// validateCLIArgs checks an args template at config-write time. `{prompt}`
// is only valid as a standalone arg positioned after a `--` terminator —
// earlier placement would let untrusted prompt text parse as a flag.
// `{file}` may be standalone (expands to every staged path) or embedded in a
// flag (`--file={file}` repeats the flag per path).
func validateCLIArgs(tpl []string) error {
	seenSep := false
	for _, a := range tpl {
		if a == "--" {
			seenSep = true
			continue
		}
		if a == "{prompt}" {
			if !seenSep {
				return fmt.Errorf("{prompt} must appear as its own arg after a -- terminator; otherwise the prompt is delivered on stdin")
			}
			continue
		}
		if strings.Contains(a, "{prompt}") {
			return fmt.Errorf("{prompt} must be a standalone arg, not embedded in %q", a)
		}
		if cliArgDenied(a) {
			return fmt.Errorf("arg %q is on the cli provider deny-list (permission-escalation flags are not allowed)", a)
		}
	}
	return nil
}

// expandCLIArgs renders the args template into a final argv. Substituted
// values (prompt, staged file paths) are data, not flags — the returned set
// marks their positions so the exec-time deny check skips them.
func expandCLIArgs(tpl []string, prompt string, files []string) ([]string, map[int]bool, error) {
	var argv []string
	data := map[int]bool{}
	mark := func() { data[len(argv)-1] = true }
	for _, a := range tpl {
		switch {
		case a == "{prompt}":
			argv = append(argv, prompt)
			mark()
		case strings.Contains(a, "{prompt}"):
			return nil, nil, fmt.Errorf("{prompt} must be a standalone arg, not embedded in %q", a)
		case a == "{file}":
			for _, f := range files {
				argv = append(argv, f)
				mark()
			}
		case strings.Contains(a, "{file}"):
			for _, f := range files {
				argv = append(argv, strings.ReplaceAll(a, "{file}", f))
				mark()
			}
		default:
			argv = append(argv, a)
		}
	}
	return argv, data, nil
}

// checkCLIArgv enforces the deny-list on the final computed argv at exec
// time. Substituted positions are skipped — they are data by construction.
func checkCLIArgv(argv []string, data map[int]bool) error {
	for i, a := range argv {
		if data[i] {
			continue
		}
		if cliArgDenied(a) {
			return fmt.Errorf("refusing to exec: arg %q is on the cli deny-list", a)
		}
	}
	return nil
}

func cliEnvNameOK(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// cliChildEnv builds the child's minimal environment: PATH, HOME (real — the
// CLI's subscription auth lives under it — or scratch when scratch_home is
// set), LANG, TMPDIR pointing at the scratch dir, plus any explicitly
// configured env_passthrough names. The daemon's environment, including
// OPENROUTER_API_KEY and per-provider keys, is never inherited wholesale.
func cliChildEnv(p Provider, scratch string) []string {
	home := os.Getenv("HOME")
	if p.ScratchHome {
		home = scratch
	}
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = "C.UTF-8"
	}
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"LANG=" + lang,
		"TMPDIR=" + scratch,
	}
	for _, name := range p.EnvPassthrough {
		if !cliEnvNameOK(name) || cliEnvDeny[name] {
			continue
		}
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// stageCLIFiles copies frame files into the scratch dir so the CLI can read
// them regardless of whether it tolerates paths outside its cwd. Unreadable
// files are skipped, mirroring callOpenRouter's per-frame tolerance.
func stageCLIFiles(scratch string, files []string) []string {
	var staged []string
	for i, f := range files {
		ext := filepath.Ext(f)
		if ext == "" {
			ext = ".jpg"
		}
		dst := filepath.Join(scratch, fmt.Sprintf("frame-%d%s", i, ext))
		src, err := os.Open(f)
		if err != nil {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			src.Close()
			continue
		}
		_, err = io.Copy(out, io.LimitReader(src, cliMaxFrameBytes))
		src.Close()
		out.Close()
		if err != nil {
			continue
		}
		staged = append(staged, dst)
	}
	return staged
}

// cappedBuffer bounds captured child output so a runaway CLI cannot exhaust
// daemon memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if remain := b.limit - b.buf.Len(); remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// stripCtl removes control characters (keeping \n and \t) so CLI output is
// stored as plain text — no ANSI escapes or terminal control sequences make
// it into blocks, chat replies, or logs.
func stripCtl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// cliPromptFromMessages flattens a chat message list into a single
// role-labeled prompt for the subprocess.
func cliPromptFromMessages(messages []orMessage) string {
	var b strings.Builder
	for _, m := range messages {
		var parts []string
		for _, c := range m.Content {
			switch {
			case c.Type == "text" && c.Text != "":
				parts = append(parts, c.Text)
			case c.Type == "image_url":
				parts = append(parts, "[image not passed to cli text call]")
			}
		}
		if len(parts) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", m.Role, strings.Join(parts, "\n"))
	}
	return strings.TrimSpace(b.String())
}

// callProviderCLIText serves a text task through a cli provider.
func callProviderCLIText(cfg Config, p Provider, messages []orMessage) (string, int, int, error) {
	text, err := runCLIProvider(cfg, p, cliPromptFromMessages(messages), nil)
	if err != nil {
		return "", 0, 0, err
	}
	return text, 0, 0, nil
}

// callProviderCLIVision serves the frame-summarize call through a cli
// provider. Sampled frames are staged into the scratch dir and referenced by
// path (in the prompt and via {file} args) instead of being base64-encoded.
func callProviderCLIVision(cfg Config, p Provider, prompt string, frames []string) (string, int, int, error) {
	text, err := runCLIProvider(cfg, p, prompt, frames)
	if err != nil {
		return "", 0, 0, err
	}
	return text, 0, 0, nil
}

// runCLIProvider executes the configured command with the hardened boundary
// and returns the child's stdout as stripped plain text.
func runCLIProvider(cfg Config, p Provider, prompt string, files []string) (string, error) {
	if strings.TrimSpace(p.Command) == "" {
		return "", fmt.Errorf("cli provider %q has no command configured (`dayflow provider set %s command <name>`)", p.ID, p.ID)
	}
	if err := validateCLIArgs(p.Args); err != nil {
		return "", fmt.Errorf("cli provider %q: %w", p.ID, err)
	}
	scratch, err := os.MkdirTemp("", "dayflow-cli-*")
	if err != nil {
		return "", fmt.Errorf("cli provider %q: scratch dir: %w", p.ID, err)
	}
	defer os.RemoveAll(scratch)

	staged := stageCLIFiles(scratch, files)
	if len(files) > 0 && len(staged) == 0 {
		return "", fmt.Errorf("cli provider %q: no readable frames", p.ID)
	}
	if len(staged) > 0 {
		// Reference the staged paths in the prompt too — this covers CLIs
		// like cursor-agent that read files named in the prompt and have no
		// attach flag.
		var sb strings.Builder
		sb.WriteString(prompt)
		sb.WriteString("\n\nFrame files to analyze (local paths):\n")
		for _, f := range staged {
			sb.WriteString("- " + f + "\n")
		}
		prompt = sb.String()
	}

	argv, data, err := expandCLIArgs(p.Args, prompt, staged)
	if err != nil {
		return "", fmt.Errorf("cli provider %q: %w", p.ID, err)
	}
	if err := checkCLIArgv(argv, data); err != nil {
		return "", fmt.Errorf("cli provider %q: %w", p.ID, err)
	}

	timeout := time.Duration(p.CLITimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultCLITimeoutSec * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// execve-style: os/exec never involves a shell. When {prompt} is carried
	// in argv (post-`--`), stdin gets an empty immediate-EOF reader so CLIs
	// that append piped stdin don't see the prompt twice.
	cmd := exec.CommandContext(ctx, p.Command, argv...)
	cmd.Dir = scratch
	cmd.Env = cliChildEnv(p, scratch)
	// Kill the whole process group on timeout — a CLI's own children (sleep,
	// tool calls) must not survive the deadline — and bound Wait so a
	// grandchild still holding the stdout/stderr pipes cannot stall us.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	if promptInArgv(p.Args) {
		cmd.Stdin = strings.NewReader("")
	} else {
		cmd.Stdin = strings.NewReader(prompt)
	}
	stdout := &cappedBuffer{limit: cliMaxOutputBytes}
	stderr := &cappedBuffer{limit: cliMaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	debugf(cfg, "cli provider %s: exec %s (%d args, %d files, timeout %s)",
		p.ID, p.Command, len(argv), len(staged), timeout)
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("cli provider %q timed out after %s", p.ID, timeout)
	}
	if runErr != nil {
		detail := strings.TrimSpace(stripCtl(stderr.String()))
		if detail == "" {
			return "", fmt.Errorf("cli provider %q failed: %v", p.ID, runErr)
		}
		return "", fmt.Errorf("cli provider %q failed: %v: %s", p.ID, runErr, truncate(detail, 200))
	}
	out := strings.TrimSpace(stripCtl(stdout.String()))
	if out == "" {
		return "", fmt.Errorf("cli provider %q produced no output", p.ID)
	}
	return out, nil
}

// promptInArgv reports whether the args template delivers the prompt via a
// `{prompt}` arg (rather than stdin).
func promptInArgv(tpl []string) bool {
	for _, a := range tpl {
		if strings.Contains(a, "{prompt}") {
			return true
		}
	}
	return false
}
