import Base
import ./findings.bend as F

type Side is Data:
  Right{}
  Left{}

type Hunk is Data:
  Hunk{lo: Nat, hi: Nat}

def side(a: F.Anchor) -> Side:
  match a:
    case F.AnchorNew{}:
      Right{}
    case F.AnchorOld{}:
      Left{}

def covers(h: Hunk, +line: Nat) -> Bool:
  match h:
    case Hunk{lo, hi}:
      Bool.and(Nat.is_le(lo, line), Nat.is_lt(line, hi))

def anyHunk(hs: List<&2, Hunk>, +line: Nat) -> Bool:
  match hs:
    case Nil{}:
      False{}
    case h <> t:
      Bool.or(covers(h, line), anyHunk(t, line))

def rangeOk(extra: Nat, +start: Nat, +hs: List<&2, Hunk>) -> Bool:
  match extra:
    case 0n:
      anyHunk(hs, start)
    case 1n+p:
      Bool.and(anyHunk(hs, start), rangeOk(p, 1n+start, hs))

def commentable(f: F.Finding, hs: List<&2, Hunk>) -> Bool:
  match f:
    case F.Finding{id, sp, ex, an, sv}:
      rangeOk(ex, F.startLine(sp), hs)

def cap50() -> Nat:
  25n+25n

def underCap(n: Nat) -> Bool:
  Nat.is_le(n, cap50())

def postOk(complete: Bool, dry: Bool) -> Bool:
  match dry:
    case True{}:
      True{}
    case False{}:
      complete
