package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"unreal-review/internal/review"
)

func cmdGroup(args []string) error {
	fs := flag.NewFlagSet("group", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", ".", "git repository to inspect")
	spec := addSpecFlags(fs)
	var exclude stringList
	fs.Var(&exclude, "exclude", "git glob to omit from the diff; repeatable")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := review.Groups(ctx, *workspace, spec.spec(), fs.Args(), exclude)
	if err != nil {
		return err
	}
	return printGroups(os.Stdout, result, *workspace, exclude)
}

func printGroups(w io.Writer, result review.GroupResult, workspace string, exclude []string) error {
	if len(result.Groups) == 0 {
		_, err := fmt.Fprintln(w, "no changes to group")
		return err
	}
	files, lines := 0, 0
	for _, group := range result.Groups {
		files += len(group.Files)
		lines += group.Lines()
	}
	if _, err := fmt.Fprintf(w, "%s\n", groupHeader(result, files, lines)); err != nil {
		return err
	}
	used := map[string]int{}
	for i, group := range result.Groups {
		if _, err := fmt.Fprintf(w, "\n%d  %s  (%s, %s)\n", i+1, group.Title, countWord(len(group.Files), "file"), countWord(group.Lines(), "line")); err != nil {
			return err
		}
		for _, file := range group.Files {
			detail := fmt.Sprintf("+%d -%d", file.Added, file.Deleted)
			if file.Binary {
				detail = "binary"
			}
			if _, err := fmt.Fprintf(w, "   %s  %s\n", file.Path, detail); err != nil {
				return err
			}
		}
		out := "findings-" + groupSlug(group.Title, used) + ".jsonl"
		if _, err := fmt.Fprintf(w, "   %s\n", formatRunCommand(workspace, result.Spec, out, exclude, group.Pathspecs)); err != nil {
			return err
		}
	}
	return nil
}

func groupHeader(result review.GroupResult, files, lines int) string {
	n := countWord(len(result.Groups), "group")
	counts := countWord(files, "file") + ", " + countWord(lines, "line")
	switch {
	case result.Spec.Commit != "":
		return fmt.Sprintf("%s in commit %s (%s)", n, result.Spec.Commit, counts)
	case result.Spec.From == "" && result.Spec.To == "" && result.Spec.Branch == "":
		return fmt.Sprintf("%s in the working tree (%s)", n, counts)
	default:
		to := result.To
		if to == "" {
			to = "working tree"
		}
		return fmt.Sprintf("%s from %s to %s (%s)", n, result.From, to, counts)
	}
}

func formatRunCommand(workspace string, spec review.Spec, out string, exclude, pathspecs []string) string {
	var b strings.Builder
	b.WriteString("unreal-review run")
	if workspace != "" && workspace != "." {
		fmt.Fprintf(&b, " --workspace %s", workspace)
	}
	b.WriteString(spec.FlagArgs())
	for _, pattern := range exclude {
		fmt.Fprintf(&b, " --exclude %s", pattern)
	}
	fmt.Fprintf(&b, " --out %s --", out)
	for _, pathspec := range pathspecs {
		fmt.Fprintf(&b, " %s", pathspec)
	}
	return b.String()
}

func groupSlug(title string, used map[string]int) string {
	s := slugify(title)
	n := used[s]
	used[s] = n + 1
	if n == 0 {
		return s
	}
	return fmt.Sprintf("%s-%d", s, n+1)
}

func slugify(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "group"
	}
	return s
}

func countWord(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
