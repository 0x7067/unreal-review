import Base
import ./spec/findings.bend as F
import ./spec/range.bend as R
import ./spec/checkpoint.bend as C
import ./spec/github.bend as G
import ./spec/group.bend as Gr

law omitted_status_complete:
  {F.complete(F.StatusNone{}) == True{} : Bool}

law running_not_complete:
  {F.complete(F.StatusRunning{}) == False{} : Bool}

law complete_status_complete:
  {F.complete(F.StatusComplete{}) == True{} : Bool}

law failed_not_complete:
  {F.complete(F.StatusFailed{}) == False{} : Bool}

law normalize_end_omitted:
  for +start: Nat
  {F.normalizeEnd(start, F.NoNat{}) == start : Nat}

law normalize_anchor_omitted:
  {F.normalizeAnchor(F.NoAnchor{}) == F.AnchorNew{} : F.Anchor}

law same_diff_refl:
  for +s: F.Source
  {F.sameDiff(s, s) == True{} : Bool}

law recorded_zero:
  {F.recorded(F.costZero()) == False{} : Bool}

law cost_add_zero_l:
  for +c: F.Cost
  {F.costAdd(F.costZero(), c) == c : F.Cost}

law cost_add_zero_r:
  for +c: F.Cost
  {F.costAdd(c, F.costZero()) == c : F.Cost}

law empty_diff_complete:
  for fresh: Bool
  for status: F.Status
  for same: Bool
  {C.decideEmpty(True{}, fresh, status, same) == C.EmptyComplete{} : C.Decision}

law complete_none_refuses:
  for same: Bool
  {C.decideEmpty(False{}, False{}, F.StatusNone{}, same) == C.RefuseComplete{} : C.Decision}

law complete_done_refuses:
  for same: Bool
  {C.decideEmpty(False{}, False{}, F.StatusComplete{}, same) == C.RefuseComplete{} : C.Decision}

law mismatch_refuses:
  {C.decideEmpty(False{}, False{}, F.StatusRunning{}, False{}) == C.RefuseMismatch{} : C.Decision}

law failed_mismatch_refuses:
  {C.decideEmpty(False{}, False{}, F.StatusFailed{}, False{}) == C.RefuseMismatch{} : C.Decision}

law resume_when_same:
  {C.decideEmpty(False{}, False{}, F.StatusRunning{}, True{}) == C.Resume{} : C.Decision}

law fresh_starts:
  for status: F.Status
  for same: Bool
  {C.decideEmpty(False{}, True{}, status, same) == C.Start{} : C.Decision}

law interrupt_stays_running:
  {C.finishStatus(False{}, True{}) == F.StatusRunning{} : F.Status}

law other_failure_failed:
  {C.finishStatus(False{}, False{}) == F.StatusFailed{} : F.Status}

law finish_ok_complete:
  for interrupted: Bool
  {C.finishStatus(True{}, interrupted) == F.StatusComplete{} : F.Status}

law complete_needs_summary:
  {C.canComplete(False{}, True{}) == False{} : Bool}

law complete_needs_cost:
  {C.canComplete(True{}, False{}) == False{} : Bool}

law complete_when_both:
  {C.canComplete(True{}, True{}) == True{} : Bool}

law empty_ids_keep_both:
  for +sp: Nat
  for +ex: Nat
  for +an: F.Anchor
  for +sv: F.Severity
  for +sp2: Nat
  for +ex2: Nat
  for +an2: F.Anchor
  for +sv2: F.Severity
  {F.mergePair(F.Finding{F.NoId{}, sp, ex, an, sv}, F.Finding{F.NoId{}, sp2, ex2, an2, sv2}) == [F.Finding{F.NoId{}, sp, ex, an, sv}, F.Finding{F.NoId{}, sp2, ex2, an2, sv2}] : List<&2, F.Finding>}

law workspace_labels:
  {R.labels(R.Workspace{}) == R.Labels{R.someNat(0n), R.noneNat()} : R.Labels}

law range_omit_to:
  for +from: Nat
  {R.labels(R.Range{from, None{}}) == R.Labels{Some{from}, None{}} : R.Labels}

law commit_root:
  for +rev: Nat
  {R.labels(R.Commit{rev, None{}}) == R.Labels{None{}, Some{rev}} : R.Labels}

law commit_parent:
  for +rev: Nat
  for +p: Nat
  {R.labels(R.Commit{rev, Some{p}}) == R.Labels{Some{p}, Some{rev}} : R.Labels}

law new_is_right:
  {G.side(F.AnchorNew{}) == G.Right{} : G.Side}

law old_is_left:
  {G.side(F.AnchorOld{}) == G.Left{} : G.Side}

law post_dry_ok:
  for complete: Bool
  {G.postOk(complete, True{}) == True{} : Bool}

law post_requires_complete:
  {G.postOk(False{}, False{}) == False{} : Bool}

law post_complete_ok:
  {G.postOk(True{}, False{}) == True{} : Bool}

law inline_cap:
  {G.underCap(G.cap50()) == True{} : Bool}

law unique_none:
  {Gr.uniqueHits(Nil{}) == None{} : Maybe<&2, Nat>}

law unique_one:
  for +x: Nat
  {Gr.uniqueHits([x]) == Some{x} : Maybe<&2, Nat>}

law unique_two:
  for +x: Nat
  for +y: Nat
  {Gr.uniqueHits([x, y]) == None{} : Maybe<&2, Nat>}

law same_dir_test_stays:
  for +d: Nat
  for b: Bool
  {Gr.shouldAttach(d, d, b) == False{} : Bool}

law attach_from_test_dir:
  {Gr.shouldAttach(0n, 1n, True{}) == True{} : Bool}

law attach_not_test_dir:
  {Gr.shouldAttach(0n, 1n, False{}) == False{} : Bool}

law no_flush_empty_chunk:
  for nl: Nat
  for add: Nat
  {Gr.needFlush(0n, nl, add) == False{} : Bool}

law flush_at_file_cap:
  {Gr.needFlush(25n, 0n, 0n) == True{} : Bool}

law family_needs_two:
  {Gr.familyOk(1n) == False{} : Bool}

law family_two_ok:
  {Gr.familyOk(2n) == True{} : Bool}

law header_over_cap:
  {Gr.headerOk(5n) == False{} : Bool}

law header_cap_ok:
  {Gr.headerOk(4n) == True{} : Bool}

law pathspec_multi_dir:
  for excl: Bool
  {Gr.pathspec(False{}, excl) == Gr.SpecFiles{} : Gr.Pathspec}

law pathspec_exclusive_dir:
  {Gr.pathspec(True{}, True{}) == Gr.SpecDir{} : Gr.Pathspec}

law pathspec_not_exclusive:
  {Gr.pathspec(True{}, False{}) == Gr.SpecFiles{} : Gr.Pathspec}

law family_skips_existing:
  for n: Nat
  {Gr.takeFamily(True{}, n) == False{} : Bool}

law init_root_is_file:
  for +id: Nat
  for +stem: Nat
  for +ext: Nat
  for kind: Gr.FileKind
  for lines: Nat
  {Gr.initKey(Gr.File{id, 0n, stem, ext, kind, lines}) == Gr.KFile{id} : Gr.GKey}

law init_dir_key:
  for +id: Nat
  for +stem: Nat
  for +ext: Nat
  for kind: Gr.FileKind
  for lines: Nat
  {Gr.initKey(Gr.File{id, 1n, stem, ext, kind, lines}) == Gr.KDir{1n} : Gr.GKey}

law cluster_perm:
  for +id: Nat
  for +fs: List<&2, Gr.ChangedFile>
  for +es: List<&2, Gr.Edge>
  {Gr.countId(id, fs) == Gr.countIdGroups(id, Gr.cluster(fs, es)) : Nat}

law cluster_caps:
  for +fs: List<&2, Gr.ChangedFile>
  for +es: List<&2, Gr.Edge>
  Gr.AllCapped(Gr.cluster(fs, es))

law pull_narrows_when_usable:
  for +full: Nat
  for +rev: Nat
  {R.pullBase(full, R.Since{rev}, True{}) == rev : Nat}

law pull_full_when_unusable:
  for +full: Nat
  for +rev: Nat
  {R.pullBase(full, R.Since{rev}, False{}) == full : Nat}

law pull_full_without_since_true:
  for +full: Nat
  {R.pullBase(full, R.NoSince{}, True{}) == full : Nat}

law pull_full_without_since_false:
  for +full: Nat
  {R.pullBase(full, R.NoSince{}, False{}) == full : Nat}

law duplicate_suppressed:
  {G.comments([G.Flags{True{}, True{}}, G.Flags{False{}, True{}}]) == 1n : Nat}

law not_commentable_skipped:
  {G.comments([G.Flags{False{}, False{}}, G.Flags{False{}, True{}}]) == 1n : Nat}

law unseen_kept:
  {G.comments([G.Flags{False{}, True{}}]) == 1n : Nat}

law duplicate_does_not_consume_cap:
  {G.comments([G.Flags{True{}, True{}}, G.Flags{False{}, True{}}]) == G.comments([G.Flags{False{}, True{}}]) : Nat}

law cap_is_fifty:
  {G.cap50() == 50n : Nat}

law cap_bounds_comments:
  {G.underCap(G.cap50()) == True{} : Bool}

law post_new_false_empty:
  {G.postNew(0n, 0n) == False{} : Bool}

law post_new_true_comment:
  for +n: Nat
  for +d: Nat
  {G.postNew(1n+n, d) == True{} : Bool}

law post_new_true_dropped:
  {G.postNew(0n, 1n+0n) == True{} : Bool}
