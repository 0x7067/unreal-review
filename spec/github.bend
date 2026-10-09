import Base
import ./findings.bend as F

type Side is Data:
  Right{}
  Left{}

def side(a: F.Anchor) -> Side:
  match a:
    case F.AnchorNew{}:
      Right{}
    case F.AnchorOld{}:
      Left{}

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

def dropped(flags: List<&2, Flags>) -> Nat:
  match flags:
    case Nil{}:
      0n
    case f <> t:
      match f:
        case Flags{posted, commentable}:
          match posted:
            case True{}:
              dropped(t)
            case False{}:
              match commentable:
                case True{}:
                  dropped(t)
                case False{}:
                  Nat.add(1n, dropped(t))

type Drop is Data:
  OutsidePatch{}
  OverCap{}

def receipt(ds: List<&2, Drop>) -> Bool:
  match ds:
    case Nil{}:
      True{}
    case d <> t:
      match d:
        case OutsidePatch{}:
          receipt(t)
        case OverCap{}:
          False{}
