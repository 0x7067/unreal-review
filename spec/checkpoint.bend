import Base
import ./findings.bend as F

type Decision is Data:
  Start{}
  Resume{}
  EmptyComplete{}
  RefuseComplete{}
  RefuseMismatch{}

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

def outcomeStatus(d: Decision) -> F.Status:
  match d:
    case Start{}:
      F.StatusRunning{}
    case Resume{}:
      F.StatusRunning{}
    case EmptyComplete{}:
      F.StatusComplete{}
    case RefuseComplete{}:
      F.StatusComplete{}
    case RefuseMismatch{}:
      F.StatusRunning{}

def canComplete(summary: Bool, rec: Bool) -> Bool:
  Bool.and(summary, rec)

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
