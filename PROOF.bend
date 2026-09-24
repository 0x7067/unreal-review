import Base
import ./spec/nat.bend as N
import ./spec/findings.bend as F
import ./spec/range.bend as R
import ./spec/checkpoint.bend as C
import ./spec/github.bend as G
import ./spec/group.bend as Gr
import ./LAWS.bend as Laws

def Laws.omitted_status_complete():
  {==}

def Laws.running_not_complete():
  {==}

def Laws.complete_status_complete():
  {==}

def Laws.failed_not_complete():
  {==}

def Laws.normalize_end_omitted(start):
  {==}

def Laws.normalize_anchor_omitted():
  {==}

def Laws.same_diff_refl(s):
  F.sameDiffRefl(s)

def Laws.recorded_zero():
  {==}

def Laws.cost_add_zero_l(c):
  F.costAddZeroL(c)

def Laws.cost_add_zero_r(c):
  F.costAddZeroR(c)

def Laws.empty_diff_complete(fresh, status, same):
  {==}

def Laws.complete_none_refuses(same):
  {==}

def Laws.complete_done_refuses(same):
  {==}

def Laws.mismatch_refuses():
  {==}

def Laws.failed_mismatch_refuses():
  {==}

def Laws.resume_when_same():
  {==}

def Laws.fresh_starts(status, same):
  {==}

def Laws.interrupt_stays_running():
  {==}

def Laws.other_failure_failed():
  {==}

def Laws.finish_ok_complete(interrupted):
  {==}

def Laws.complete_needs_summary():
  {==}

def Laws.complete_needs_cost():
  {==}

def Laws.complete_when_both():
  {==}

def Laws.empty_ids_keep_both(sp, ex, an, sv, sp2, ex2, an2, sv2):
  {==}

def Laws.workspace_labels():
  {==}

def Laws.range_omit_to(from):
  {==}

def Laws.commit_root(rev):
  {==}

def Laws.commit_parent(rev, p):
  {==}

def Laws.new_is_right():
  {==}

def Laws.old_is_left():
  {==}

def Laws.post_dry_ok(complete):
  {==}

def Laws.post_requires_complete():
  {==}

def Laws.post_complete_ok():
  {==}

def Laws.inline_cap():
  {==}

def Laws.unique_none():
  {==}

def Laws.unique_one(x):
  {==}

def Laws.unique_two(x, y):
  {==}

def Laws.same_dir_test_stays(d, b):
  match b:
    case False{}:
      {==}
    case True{}:
      N.neRefl(d)

def Laws.attach_from_test_dir():
  {==}

def Laws.attach_not_test_dir():
  {==}

def Laws.no_flush_empty_chunk(nl, add):
  {==}

def Laws.flush_at_file_cap():
  {==}

def Laws.family_needs_two():
  {==}

def Laws.family_two_ok():
  {==}

def Laws.header_over_cap():
  {==}

def Laws.header_cap_ok():
  {==}

def Laws.pathspec_multi_dir(excl):
  {==}

def Laws.pathspec_exclusive_dir():
  {==}

def Laws.pathspec_not_exclusive():
  {==}

def Laws.family_skips_existing(n):
  {==}

def Laws.init_root_is_file(id, stem, ext, kind, lines):
  {==}

def Laws.init_dir_key(id, stem, ext, kind, lines):
  {==}

def Laws.cluster_perm(id, fs, es):
  Gr.clusterPerm(id, fs, es)

def Laws.cluster_caps(fs, es):
  Gr.clusterCaps(fs, es)
