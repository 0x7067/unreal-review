import Base
import ./findings.bend as F

type Spec is Data:
  Workspace{}
  Range{from: Nat, to: Maybe<&2, Nat>}
  Commit{rev: Nat, parent: Maybe<&2, Nat>}
  Branch{name: Nat}

type Labels is Data:
  Labels{base: Maybe<&2, Nat>, head: Maybe<&2, Nat>}

def noneNat() -> Maybe<&2, Nat>:
  None{}

def someNat(n: Nat) -> Maybe<&2, Nat>:
  Some{n}

def labels(s: Spec) -> Labels:
  match s:
    case Workspace{}:
      Labels{someNat(0n), noneNat()}
    case Range{from, to}:
      Labels{Some{from}, to}
    case Commit{rev, parent}:
      Labels{parent, Some{rev}}
    case Branch{name}:
      Labels{someNat(0n), Some{name}}

type Since is Data:
  NoSince{}
  Since{rev: Nat}

def pullBase(full: Nat, since: Since, usable: Bool) -> Nat:
  match since:
    case NoSince{}:
      full
    case Since{rev}:
      match usable:
        case True{}:
          rev
        case False{}:
          full

def isWorkspace(s: Spec) -> Bool:
  match s:
    case Workspace{}:
      True{}
    case Range{f, t}:
      False{}
    case Commit{r, p}:
      False{}
    case Branch{n}:
      False{}
