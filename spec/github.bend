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
  50n

def underCap(n: Nat) -> Bool:
  Nat.is_le(n, cap50())

type Flags is Data:
  Flags{posted: Bool, commentable: Bool}

def comments(flags: List<&2, Flags>) -> Nat:
  match flags:
    case Nil{}:
      0n
    case f <> t:
      match f:
        case Flags{posted, commentable}:
          match posted:
            case True{}:
              comments(t)
            case False{}:
              match commentable:
                case False{}:
                  comments(t)
                case True{}:
                  Nat.add(1n, comments(t))

def postNew(total: Nat, comments: Nat, dropped: Nat) -> Bool:
  match total:
    case 0n:
      True{}
    case 1n+t:
      match comments:
        case 0n:
          match dropped:
            case 0n:
              False{}
            case 1n+d:
              True{}
        case 1n+c:
          True{}

def postOk(complete: Bool, dry: Bool) -> Bool:
  match dry:
    case True{}:
      True{}
    case False{}:
      complete
