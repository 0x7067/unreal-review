import Base
import ./spec/nat.bend as N
import ./spec/findings.bend as F
import ./spec/range.bend as R
import ./spec/checkpoint.bend as C
import ./spec/github.bend as G
import ./spec/group.bend as Gr
import ./spec/focused.bend as Fo
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

def Laws.clean_summary_without_findings():
  {==}

def Laws.clean_summary_with_findings_rejected(n):
  {==}

def Laws.verdict_summary_with_findings(n):
  {==}

def Laws.verdict_summary_without_findings_rejected():
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

def Laws.pull_reviewed_narrows(base, rev):
  {==}

def Laws.pull_full_without_receipt(base):
  {==}

def Laws.duplicate_suppressed():
  {==}

def Laws.not_commentable_skipped():
  {==}

def Laws.unseen_kept():
  {==}

def Laws.duplicate_does_not_consume_cap():
  {==}

def Laws.cap_is_fifty():
  {==}

def Laws.cap_bounds_comments():
  {==}

def Laws.post_new_empty_lgtm():
  {==}

def Laws.post_new_true_comment(n, d):
  {==}

def Laws.post_new_true_dropped():
  {==}

def Laws.post_new_false_all_reported():
  {==}

def Laws.dropped_counts_unposted_off_patch():
  {==}

def Laws.dropped_already_posted_not_repeated():
  {==}

def Laws.receipt_without_drops():
  {==}

def Laws.receipt_outside_patch_ok():
  {==}

def Laws.receipt_over_cap_blocks(ds):
  {==}

def Laws.oversized_diff_refused(empty, fresh, status, same):
  {==}

def Laws.diff_within_limit_decides_as_before(empty, fresh, status, same):
  {==}

# INTERNAL adapter lemmas. These certify the hand-written mechanics abstraction,
# not new product laws, semantic bug evidence, or equivalence with Go execution.
def internal_focused_stage_error(u: Bool, s: Bool, c: Bool, r: Bool) -> {Fo.stageComplete(False{}, u, s, c, r) == False{} : Bool}:
  {==}

def internal_focused_stage_interrupted(s: Bool, c: Bool, r: Bool) -> {Fo.stageComplete(True{}, False{}, s, c, r) == False{} : Bool}:
  {==}

def internal_focused_stage_bad_summary(c: Bool, r: Bool) -> {Fo.stageComplete(True{}, True{}, False{}, c, r) == False{} : Bool}:
  {==}

def internal_focused_stage_missing_cost(r: Bool) -> {Fo.stageComplete(True{}, True{}, True{}, False{}, r) == False{} : Bool}:
  {==}

def internal_focused_stage_complete() -> {Fo.stageComplete(True{}, True{}, True{}, True{}, True{}) == True{} : Bool}:
  {==}

def internal_focused_unknown_id(u: Bool, a: Fo.Location, b: Fo.Location) -> {Fo.acceptCandidate(False{}, u, a, b) == False{} : Bool}:
  {==}

def internal_focused_duplicate_id(a: Fo.Location, b: Fo.Location) -> {Fo.acceptCandidate(True{}, False{}, a, b) == False{} : Bool}:
  {==}

def internal_focused_same_location(+p: Nat, +s: Nat, +e: Nat, +a: Nat) -> {Fo.acceptCandidate(True{}, True{}, Fo.At{p, s, e, a}, Fo.At{p, s, e, a}) == True{} : Bool}:
  %Equal.sym(Bool, Nat.is_eq(p, p), True{}, N.eqRefl(p)) : {Bool.and(_, Bool.and(Nat.is_eq(s, s), Bool.and(Nat.is_eq(e, e), Nat.is_eq(a, a)))) == True{} : Bool}
  %Equal.sym(Bool, Nat.is_eq(s, s), True{}, N.eqRefl(s)) : {Bool.and(_, Bool.and(Nat.is_eq(e, e), Nat.is_eq(a, a))) == True{} : Bool}
  %Equal.sym(Bool, Nat.is_eq(e, e), True{}, N.eqRefl(e)) : {Bool.and(_, Nat.is_eq(a, a)) == True{} : Bool}
  N.eqRefl(a)

# Overlapping [1,3] and [2,3] do not satisfy exact location acceptance.
def internal_focused_overlap_not_identity() -> {Fo.acceptCandidate(True{}, True{}, Fo.At{0n, 1n, 3n, 0n}, Fo.At{0n, 2n, 3n, 0n}) == False{} : Bool}:
  {==}

def internal_focused_dependency_barrier(+stages: List<&2, Bool>, v: Bool, d: Bool, u: Bool, missing: {Fo.discoveriesComplete(stages) == False{} : Bool}) -> {Fo.publish(stages, v, d, u) == False{} : Bool}:
  %Equal.sym(Bool, Fo.discoveriesComplete(stages), False{}, missing) : {Fo.publishReady(_, v, d, u) == False{} : Bool}
  {==}

def internal_focused_four_discoveries_ready() -> {Fo.publish([True{}, True{}, True{}, True{}], True{}, True{}, True{}) == True{} : Bool}:
  {==}

def internal_focused_unverified_not_published() -> {Fo.publish([True{}, True{}, True{}, True{}], False{}, True{}, True{}) == False{} : Bool}:
  {==}

def internal_focused_changed_dependency_not_published() -> {Fo.publish([True{}, True{}, True{}, True{}], True{}, False{}, True{}) == False{} : Bool}:
  {==}

# Exact Nat reconciliation: returning only outstanding then adding it to prior
# recovers the durable total, including attempt costs not acknowledged earlier.
def internal_focused_outstanding(prior: Nat, outstanding: Nat) -> {Fo.costDelta(Nat.add(prior, outstanding), prior) == Fo.Outstanding{outstanding} : Fo.Delta}:
  match prior:
    case 0n:
      {==}
    case 1n+p:
      internal_focused_outstanding(p, outstanding)

def internal_focused_no_double_charge(prior: Nat) -> {Fo.costDelta(Nat.add(prior, 0n), prior) == Fo.Outstanding{0n} : Fo.Delta}:
  internal_focused_outstanding(prior, 0n)

def internal_focused_prior_exceeds_empty(p: Nat) -> {Fo.costDelta(0n, 1n+p) == Fo.Invalid{} : Fo.Delta}:
  {==}

# record appends every attempt regardless of whether the stage completes.
def internal_focused_attempt_additive(amount: Nat, attempts: List<&2, Nat>) -> {Fo.ledger(amount <> attempts) == Nat.add(amount, Fo.ledger(attempts)) : Nat}:
  {==}
