import Base
import ./nat.bend as N

type Status is Data:
  StatusNone{}
  StatusRunning{}
  StatusComplete{}
  StatusFailed{}

type Anchor is Data:
  AnchorNew{}
  AnchorOld{}

type Severity is Data:
  SeverityError{}
  SeverityWarning{}
  SeverityNote{}

type Hash is Data:
  Hash{n: Nat}

type Source is Data:
  Source{base_sha: Hash, head_sha: Hash, diff_sha: Hash}

type Cost is Data:
  Cost{amount: Nat, input: Nat, output: Nat, reasoning: Nat, cached: Nat, requests: Nat}

type Fid is Data:
  NoId{}
  Id{n: Nat}

type Finding is Data:
  Finding{id: Fid, start_pred: Nat, extra: Nat, anchor: Anchor, severity: Severity}

type Report is Data:
  Report{status: Status, source: Source, cost: Cost, summary: Bool, findings: List<&2, Finding>}

def complete(s: Status) -> Bool:
  match s:
    case StatusNone{}:
      True{}
    case StatusRunning{}:
      False{}
    case StatusComplete{}:
      True{}
    case StatusFailed{}:
      False{}

def Complete(s: Status) -> Type:
  match s:
    case StatusNone{}:
      Unit
    case StatusRunning{}:
      Empty
    case StatusComplete{}:
      Unit
    case StatusFailed{}:
      Empty

def Incomplete(s: Status) -> Type:
  match s:
    case StatusNone{}:
      Empty
    case StatusRunning{}:
      Unit
    case StatusComplete{}:
      Empty
    case StatusFailed{}:
      Unit

def startLine(pred: Nat) -> Nat:
  1n+pred

def endLine(pred: Nat, extra: Nat) -> Nat:
  Nat.add(1n+pred, extra)

type MaybeNat is Data:
  NoNat{}
  SomeNat{n: Nat}

type MaybeAnchor is Data:
  NoAnchor{}
  SomeAnchor{a: Anchor}

def normalizeEnd(+start: Nat, end: MaybeNat) -> Nat:
  match end:
    case NoNat{}:
      start
    case SomeNat{n}:
      n

def normalizeAnchor(a: MaybeAnchor) -> Anchor:
  match a:
    case NoAnchor{}:
      AnchorNew{}
    case SomeAnchor{x}:
      x

def hashEq(a: Hash, b: Hash) -> Bool:
  match a b:
    case Hash{x} Hash{y}:
      Nat.is_eq(x, y)

def and3(a: Bool, b: Bool, c: Bool) -> Bool:
  Bool.and(a, Bool.and(b, c))

def sameDiff(a: Source, b: Source) -> Bool:
  match a b:
    case Source{ba, ha, da} Source{bb, hb, db}:
      and3(hashEq(ba, bb), hashEq(ha, hb), hashEq(da, db))

def costZero() -> Cost:
  Cost{0n, 0n, 0n, 0n, 0n, 0n}

def costAdd(a: Cost, b: Cost) -> Cost:
  match a b:
    case Cost{am, i, o, r, c, q} Cost{am2, i2, o2, r2, c2, q2}:
      Cost{Nat.add(am, am2), Nat.add(i, i2), Nat.add(o, o2), Nat.add(r, r2), Nat.add(c, c2), Nat.add(q, q2)}

def or4(a: Bool, b: Bool, c: Bool, d: Bool) -> Bool:
  Bool.or(a, Bool.or(b, Bool.or(c, d)))

def nz(n: Nat) -> Bool:
  match n:
    case 0n:
      False{}
    case 1n+p:
      True{}

def recorded(c: Cost) -> Bool:
  match c:
    case Cost{am, i, o, r, cached, q}:
      or4(nz(q), nz(i), nz(o), nz(am))

def hashEqRefl(h: Hash) -> {hashEq(h, h) == True{} : Bool}:
  match h:
    case Hash{n}:
      N.eqRefl(n)

def fidEq(a: Fid, b: Fid) -> Bool:
  match a b:
    case NoId{} NoId{}:
      True{}
    case NoId{} Id{n}:
      False{}
    case Id{n} NoId{}:
      False{}
    case Id{x} Id{y}:
      Nat.is_eq(x, y)

def mergePairBoth(a: Finding, b: Finding, eq: Bool) -> List<&2, Finding>:
  match eq:
    case True{}:
      [b]
    case False{}:
      [a, b]

def mergePairId(n: Nat, a: Finding, b: Finding) -> List<&2, Finding>:
  match b:
    case Finding{NoId{}, sp, ex, an, sv}:
      [a, Finding{NoId{}, sp, ex, an, sv}]
    case Finding{Id{+m}, sp, ex, an, sv}:
      mergePairBoth(a, Finding{Id{m}, sp, ex, an, sv}, Nat.is_eq(n, m))

def mergePair(a: Finding, b: Finding) -> List<&2, Finding>:
  match a:
    case Finding{NoId{}, sp, ex, an, sv}:
      [Finding{NoId{}, sp, ex, an, sv}, b]
    case Finding{Id{+n}, sp, ex, an, sv}:
      mergePairId(n, Finding{Id{n}, sp, ex, an, sv}, b)

def sameDiffReflH(+ba: Hash, +ha: Hash, +da: Hash) -> {sameDiff(Source{ba, ha, da}, Source{ba, ha, da}) == True{} : Bool}:
  %Equal.sym(Bool, hashEq(ba, ba), True{}, hashEqRefl(ba)) : {Bool.and(_, Bool.and(hashEq(ha, ha), hashEq(da, da))) == True{} : Bool}
  %Equal.sym(Bool, hashEq(ha, ha), True{}, hashEqRefl(ha)) : {Bool.and(_, hashEq(da, da)) == True{} : Bool}
  hashEqRefl(da)

def sameDiffRefl(s: Source) -> {sameDiff(s, s) == True{} : Bool}:
  match s:
    case Source{+ba, +ha, +da}:
      sameDiffReflH(ba, ha, da)

def costAddZeroL(c: Cost) -> {costAdd(costZero(), c) == c : Cost}:
  match c:
    case Cost{am, i, o, r, cached, q}:
      {==}

def costEq6(am: Nat, i: Nat, o: Nat, r: Nat, cached: Nat, q: Nat, am2: Nat, i2: Nat, o2: Nat, r2: Nat, c2: Nat, q2: Nat, eam: {Nat.add(am, 0n) == am2 : Nat}, ei: {Nat.add(i, 0n) == i2 : Nat}, eo: {Nat.add(o, 0n) == o2 : Nat}, er: {Nat.add(r, 0n) == r2 : Nat}, ec: {Nat.add(cached, 0n) == c2 : Nat}, eq: {Nat.add(q, 0n) == q2 : Nat}) -> {Cost{Nat.add(am, 0n), Nat.add(i, 0n), Nat.add(o, 0n), Nat.add(r, 0n), Nat.add(cached, 0n), Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}:
  %Equal.sym(Nat, Nat.add(am, 0n), am2, eam) : {Cost{_, Nat.add(i, 0n), Nat.add(o, 0n), Nat.add(r, 0n), Nat.add(cached, 0n), Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  %Equal.sym(Nat, Nat.add(i, 0n), i2, ei) : {Cost{am2, _, Nat.add(o, 0n), Nat.add(r, 0n), Nat.add(cached, 0n), Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  %Equal.sym(Nat, Nat.add(o, 0n), o2, eo) : {Cost{am2, i2, _, Nat.add(r, 0n), Nat.add(cached, 0n), Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  %Equal.sym(Nat, Nat.add(r, 0n), r2, er) : {Cost{am2, i2, o2, _, Nat.add(cached, 0n), Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  %Equal.sym(Nat, Nat.add(cached, 0n), c2, ec) : {Cost{am2, i2, o2, r2, _, Nat.add(q, 0n)} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  %Equal.sym(Nat, Nat.add(q, 0n), q2, eq) : {Cost{am2, i2, o2, r2, c2, _} == Cost{am2, i2, o2, r2, c2, q2} : Cost}
  {==}

def costAddZeroR(c: Cost) -> {costAdd(c, costZero()) == c : Cost}:
  match c:
    case Cost{+am, +i, +o, +r, +cached, +q}:
      costEq6(am, i, o, r, cached, q, am, i, o, r, cached, q, N.addZero(am), N.addZero(i), N.addZero(o), N.addZero(r), N.addZero(cached), N.addZero(q))
