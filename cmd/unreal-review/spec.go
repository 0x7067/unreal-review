package main

import (
	"flag"

	"unreal-review/internal/review"
)

type specFlags struct {
	from   *string
	to     *string
	commit *string
	branch *string
}

func addSpecFlags(fs *flag.FlagSet) specFlags {
	return specFlags{
		from:   fs.String("from", "", "start of a git range: branch, tag, or SHA"),
		to:     fs.String("to", "", "end of a git range: branch, tag, or SHA (default with --from: working tree)"),
		commit: fs.String("commit", "", "single commit against its first parent"),
		branch: fs.String("branch", "", "branch since it diverged from main or master"),
	}
}

func (f specFlags) spec() review.Spec {
	return review.Spec{From: *f.from, To: *f.to, Commit: *f.commit, Branch: *f.branch}
}
