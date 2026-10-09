import Base
import ./findings.bend as F
import ./plan.bend as P

type Decision is Data:
  Start{}
  Resume{}
  EmptyComplete{}
  RefuseComplete{}
  RefuseMismatch{}
  RefuseOversized{}

def decideRunning(same: Bool) -> Decision:
  match same:
    case True{}:
      Resume{}
    case False{}:
      RefuseMismatch{}

def decideStale(status: F.Status, same: Bool) -> Decision:
  match status:
    case F.StatusNone{}:
      RefuseComplete{}
    case F.StatusComplete{}:
      RefuseComplete{}
    case F.StatusRunning{}:
      decideRunning(same)
    case F.StatusFailed{}:
      decideRunning(same)

def decideFresh(fresh: Bool, status: F.Status, same: Bool) -> Decision:
  match fresh:
    case True{}:
      Start{}
    case False{}:
      decideStale(status, same)

def decideEmpty(empty: Bool, fresh: Bool, status: F.Status, same: Bool) -> Decision:
  match empty:
    case True{}:
      EmptyComplete{}
    case False{}:
      decideFresh(fresh, status, same)

def decideReview(oversized: Bool, empty: Bool, fresh: Bool, status: F.Status, same: Bool) -> Decision:
  match oversized:
    case True{}:
      RefuseOversized{}
    case False{}:
      decideEmpty(empty, fresh, status, same)

# Planned path is selected for oversized diffs OR explicit decomposition.
# valid abstracts source planner validation, not an agent assertion.
def decidePlanned(valid: Bool, empty: Bool, fresh: Bool, status: F.Status, same: Bool) -> Decision:
  match valid:
    case False{}:
      RefuseOversized{}
    case True{}:
      decideEmpty(empty, fresh, status, same)

def canComplete(summary: Bool, rec: Bool) -> Bool:
  Bool.and(summary, rec)

def canCompletePlanned(summary: Bool, rec: Bool, valid: Bool, digest: Nat, returned: Nat, required: List<&2, Nat>, completed: List<&2, Nat>, verified: List<&2, Nat>, sourceSame: Bool) -> Bool:
  P.complete(summary, rec, valid, P.coverage(digest, returned, required, completed, verified), sourceSame)

def summaryFits(clean: Bool, findings: Nat) -> Bool:
  match findings:
    case 0n:
      clean
    case 1n+p:
      Bool.not(clean)

def finishFail(interrupted: Bool) -> F.Status:
  match interrupted:
    case True{}:
      F.StatusRunning{}
    case False{}:
      F.StatusFailed{}

def finishStatus(ok: Bool, interrupted: Bool) -> F.Status:
  match ok:
    case True{}:
      F.StatusComplete{}
    case False{}:
      finishFail(interrupted)
