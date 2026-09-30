import Base
import ./nat.bend as N

# Hand-written model of review plan validation, not Go equivalence or semantic
# bug truth. Nat IDs/digests stand for exact interned strings. Bytes are serialized
# prompt lengths, not a tokenizer estimate. Nat token/cost analogues are exact
# nonnegative units and do not model USD floats or provider accounting.
type Span is Data:
  Own{start: Nat, end: Nat}

# Context spans (including boundary context) are deliberately absent from this
# ownership list: only local task Spans own bytes, flattened and sorted by Start
# as in ValidatePlan. The model does not prove the Go sort implementation.
def ownsFrom(+spans: List<&2, Span>, cursor: Nat, total: Nat) -> Bool:
  match spans:
    case Nil{}:
      Nat.is_eq(cursor, total)
    case Own{s, e} <> rest:
      Bool.and(Nat.is_eq(cursor, s), Bool.and(Nat.is_lt(s, e), ownsFrom(rest, e, total)))

def fullOwnership(total: Nat, spans: List<&2, Span>) -> Bool:
  ownsFrom(spans, 0n, total)

# Boundary tasks may include overlapping context but may own no diff bytes.
def boundaryOwnership(spans: List<&2, Span>, context: List<&2, Span>) -> Bool:
  match spans:
    case Nil{}:
      True{}
    case h <> t:
      False{}

def maxPromptBytes() -> Nat:
  120000n

def directThreshold() -> Nat:
  200000n

def promptCap() -> Nat:
  maxPromptBytes()

def unitsBounded(+cap: Nat, sizes: List<&2, Nat>) -> Bool:
  match sizes:
    case Nil{}:
      True{}
    case n <> rest:
      Bool.and(Nat.is_le(n, cap), unitsBounded(cap, rest))

def contains(+id: Nat, ids: List<&2, Nat>) -> Bool:
  match ids:
    case Nil{}:
      False{}
    case h <> t:
      Bool.or(Nat.is_eq(id, h), contains(id, t))

def unique(+ids: List<&2, Nat>) -> Bool:
  match ids:
    case Nil{}:
      True{}
    case h <> t:
      Bool.and(Bool.not(contains(h, t)), unique(t))

def allIn(ids: List<&2, Nat>, +allowed: List<&2, Nat>) -> Bool:
  match ids:
    case Nil{}:
      True{}
    case h <> t:
      Bool.and(contains(h, allowed), allIn(t, allowed))

# Required includes every local and boundary task. Completed and Verified are
# independently exact sets. Verified is acknowledged only after successful
# bounded candidate verification and cross-output consolidation.
def exactCoverage(+required: List<&2, Nat>, +completed: List<&2, Nat>) -> Bool:
  Bool.and(unique(required), Bool.and(unique(completed), Bool.and(allIn(required, completed), allIn(completed, required))))

def coverage(digest: Nat, returned: Nat, +required: List<&2, Nat>, completed: List<&2, Nat>, verified: List<&2, Nat>) -> Bool:
  Bool.and(Nat.is_eq(digest, returned), Bool.and(exactCoverage(required, completed), exactCoverage(required, verified)))

# supported abstracts successful construction, including rejection of an
# indivisible line/header/context that cannot fit a serialized prompt. It also
# abstracts version/hash/content-ID/path checks, nonempty tasks/prompts and
# mandatory local/boundary counts. Those string checks are not modeled here.
# boundariesValid is the conjunction of boundaryOwnership for boundary tasks.
def validPlan(supported: Bool, boundariesValid: Bool, total: Nat, spans: List<&2, Span>, sizes: List<&2, Nat>, ids: List<&2, Nat>) -> Bool:
  Bool.and(supported, Bool.and(boundariesValid, Bool.and(fullOwnership(total, spans), Bool.and(unitsBounded(promptCap(), sizes), unique(ids)))))

# Stage dependency and Nat per-attempt accounting analogue are in focused.bend.
# These lemmas are reusable mechanics, not a durability/equivalence certificate
# for planned.go's filesystem receipts, locks, crash recovery, or float costs.
# Here sourceSame is the root's final source re-read, after agent return.
def complete(summary: Bool, cost: Bool, planValid: Bool, covered: Bool, sourceSame: Bool) -> Bool:
  Bool.and(summary, Bool.and(cost, Bool.and(planValid, Bool.and(covered, sourceSame))))

# Independent propositions extracted from executable validation. These are
# structural certificates for every unit/span, not example-only tests.
def Bounds(sizes: List<&2, Nat>, cap: Nat) -> Type:
  match sizes:
    case Nil{}:
      Unit
    case n <> rest:
      (N.LE(n, cap) & Bounds(rest, cap))

def Partition(spans: List<&2, Span>, cursor: Nat, total: Nat) -> Type:
  match spans:
    case Nil{}:
      {cursor == total : Nat}
    case Own{s, e} <> rest:
      ({cursor == s : Nat} & (N.LE(1n+s, e) & Partition(rest, e, total)))

def andLeft(a: Bool, b: Bool, ok: {Bool.and(a, b) == True{} : Bool}) -> {a == True{} : Bool}:
  match a:
    case True{}:
      {==}
    case False{}:
      N.absurd({False{} == True{} : Bool}, N.fromFalseTrue(ok))

def andRight(a: Bool, b: Bool, ok: {Bool.and(a, b) == True{} : Bool}) -> {b == True{} : Bool}:
  match a:
    case True{}:
      ok
    case False{}:
      N.absurd({b == True{} : Bool}, N.fromFalseTrue(ok))

def eqOf(a: Nat, b: Nat, ok: {Nat.is_eq(a, b) == True{} : Bool}) -> {a == b : Nat}:
  match a b:
    case 0n 0n:
      {==}

    case 0n 1n+q:
      N.absurd({0n == 1n+q : Nat}, N.fromFalseTrue(ok))
    case 1n+p 0n:
      N.absurd({1n+p == 0n : Nat}, N.fromFalseTrue(ok))
    case 1n+p 1n+q:
      %eqOf(p, q, ok) : {1n+p == 1n+_ : Nat}
      {==}

def digestOf(+digest: Nat, +returned: Nat, +required: List<&2, Nat>, +completed: List<&2, Nat>, +verified: List<&2, Nat>, ok: {coverage(digest, returned, required, completed, verified) == True{} : Bool}) -> {digest == returned : Nat}:
  eqOf(digest, returned, andLeft(Nat.is_eq(digest, returned), Bool.and(exactCoverage(required, completed), exactCoverage(required, verified)), ok))

def boundsOf(+sizes: List<&2, Nat>, +cap: Nat, +ok: {unitsBounded(cap, sizes) == True{} : Bool}) -> Bounds(sizes, cap):
  match sizes:
    case Nil{}:
      Unit{}
    case n <> rest:
      (N.leOf(n, cap, andLeft(Nat.is_le(n, cap), unitsBounded(cap, rest), ok)), boundsOf(rest, cap, andRight(Nat.is_le(n, cap), unitsBounded(cap, rest), ok)))

def ltOf(c: Cmp, ok: {Cmp.is_lt(c) == True{} : Bool}) -> {c == LT{} : Cmp}:
  match c:
    case LT{}:
      {==}
    case EQ{}:
      N.absurd({EQ{} == LT{} : Cmp}, N.fromFalseTrue(ok))
    case GT{}:
      N.absurd({GT{} == LT{} : Cmp}, N.fromFalseTrue(ok))

def partitionOf(+spans: List<&2, Span>, +cursor: Nat, +total: Nat, +ok: {ownsFrom(spans, cursor, total) == True{} : Bool}) -> Partition(spans, cursor, total):
  match spans:
    case Nil{}:
      eqOf(cursor, total, ok)
    case Own{s, e} <> rest:
      (eqOf(cursor, s, andLeft(Nat.is_eq(cursor, s), Bool.and(Nat.is_lt(s, e), ownsFrom(rest, e, total)), ok)), (N.leSuccOfLt(s, e, ltOf(Nat.cmp(s, e), andLeft(Nat.is_lt(s, e), ownsFrom(rest, e, total), andRight(Nat.is_eq(cursor, s), Bool.and(Nat.is_lt(s, e), ownsFrom(rest, e, total)), ok)))), partitionOf(rest, e, total, andRight(Nat.is_lt(s, e), ownsFrom(rest, e, total), andRight(Nat.is_eq(cursor, s), Bool.and(Nat.is_lt(s, e), ownsFrom(rest, e, total)), ok)))))
