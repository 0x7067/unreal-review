package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestSubcommandHelp(t *testing.T) {
	if args := os.Getenv("UNREAL_REVIEW_SUBCOMMAND_HELP"); args != "" {
		os.Args = append([]string{"unreal-review"}, strings.Fields(args)...)
		main()
		os.Exit(0)
	}

	cases := []struct {
		args  string
		onOut bool
		once  string
		need  []string
	}{
		{args: "run -h", once: "Usage of run:\n", need: []string{"-workspace"}},
		{args: "group -h", once: "Usage of group:\n", need: []string{"-exclude"}},
		{args: "eval -h", once: "Usage of eval:\n", need: []string{"-corpus"}},
		{args: "render markdown -h", once: "Usage of render markdown:\n", need: []string{"-out"}},
		{args: "render github -h", once: "Usage of render github:\n", need: []string{"-dry-run"}},
		{
			args:  "render -h",
			onOut: true,
			once:  "Usage:\n",
			need:  []string{"unreal-review render github", "unreal-review render markdown", "unreal-review group"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			stdout, stderr, err := runSubcommandHelp(t, tc.args)
			if err != nil {
				t.Fatalf("exit: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			got := stderr
			other := stdout
			if tc.onOut {
				got = stdout
				other = stderr
			}
			if other != "" {
				t.Fatalf("extra output:\n%s", other)
			}
			if strings.Count(got, tc.once) != 1 {
				t.Fatalf("%q count = %d\n%s", tc.once, strings.Count(got, tc.once), got)
			}
			for _, line := range tc.need {
				if !strings.Contains(got, line) {
					t.Fatalf("missing %q\n%s", line, got)
				}
			}
			if strings.Contains(got, "flag: help requested") || strings.Contains(got, "unreal-review:") {
				t.Fatalf("help error\n%s", got)
			}
		})
	}
}

func TestRegisteredFlagsAppearInHelp(t *testing.T) {
	stdout, stderr, err := runSubcommandHelp(t, "help")
	if err != nil {
		t.Fatalf("help: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	usage := usageSection(stdout)
	if usage == "" {
		t.Fatalf("main help has no usage section:\n%s", stdout)
	}

	commands := []struct {
		name     string
		args     string
		register func(*flag.FlagSet)
	}{
		{name: "run", args: "run -h", register: func(fs *flag.FlagSet) { registerRunFlags(fs) }},
		{name: "group", args: "group -h", register: func(fs *flag.FlagSet) { registerGroupFlags(fs) }},
		{name: "eval", args: "eval -h", register: func(fs *flag.FlagSet) { registerEvalFlags(fs) }},
		{name: "render github", args: "render github -h", register: func(fs *flag.FlagSet) { registerGitHubFlags(fs) }},
		{name: "render markdown", args: "render markdown -h", register: func(fs *flag.FlagSet) { registerMarkdownFlags(fs) }},
	}
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			_, helpText, err := runSubcommandHelp(t, cmd.args)
			if err != nil {
				t.Fatalf("exit: %v\n%s", err, helpText)
			}
			block := commandSynopsis(usage, cmd.name)
			if block == "" {
				t.Fatalf("synopsis missing %s\n%s", cmd.name, usage)
			}
			fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
			cmd.register(fs)
			fs.VisitAll(func(fl *flag.Flag) {
				if !mentionsFlag(helpText, fl.Name, 1) {
					t.Errorf("-%s missing from %s help\n%s", fl.Name, cmd.args, helpText)
				}
				if !mentionsFlag(block, fl.Name, 2) {
					t.Errorf("--%s missing from synopsis\n%s", fl.Name, block)
				}
			})
		})
	}
}

func usageSection(text string) string {
	const start = "Usage:\n"
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	rest := text[i+len(start):]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func commandSynopsis(usage, command string) string {
	var b strings.Builder
	on := false
	for _, line := range strings.Split(usage, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "unreal-review ") {
			rest := strings.TrimPrefix(trim, "unreal-review ")
			on = rest == command || strings.HasPrefix(rest, command+" ")
		}
		if on {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func mentionsFlag(text, name string, dashes int) bool {
	prefix := strings.Repeat("-", dashes) + name
	pattern := `(?:^|[^A-Za-z0-9-])` + regexp.QuoteMeta(prefix) + `(?:[^A-Za-z0-9-]|$)`
	return regexp.MustCompile(pattern).MatchString(text)
}

func runSubcommandHelp(t *testing.T, args string) (string, string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSubcommandHelp$")
	cmd.Env = subcommandHelpEnv(args)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func subcommandHelpEnv(args string) []string {
	drop := map[string]struct{}{
		"OPENROUTER_API_KEY":            {},
		"GH_TOKEN":                      {},
		"GITHUB_TOKEN":                  {},
		"UNREAL_REVIEW_SECRETS_FD":      {},
		"UNREAL_REVIEW_SUBCOMMAND_HELP": {},
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, ok := drop[name]; ok {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "UNREAL_REVIEW_SUBCOMMAND_HELP="+args)
}
