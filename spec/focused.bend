import Base

# Hand-written abstraction of internal/agent/focused.go, not an equivalence
# certificate for Go, filesystem durability, or the model's semantic judgment.
# Focused.Run's run closure rejects agent errors and ctx.Err(), then calls
# focusedComplete. summaryOk abstracts findings.CheckSummary (syntax AND verdict).
# costOk abstracts Recorded plus focusedCostDelta(cost, zero) validation.
# readableOk includes successful reads/coalescing/write/digest before record.
def stageComplete(success: Bool, uninterrupted: Bool, summaryOk: Bool, costOk: Bool, readableOk: Bool) -> Bool:
  match success:
    case False{}:
      False{}
    case True{}:
      match uninterrupted:
        case False{}:
          False{}
        case True{}:
          match summaryOk:
            case False{}:
              False{}
            case True{}:
              match costOk:
                case False{}:
                  False{}
                case True{}:
                  readableOk

# focusedValidateVerification: Nat values stand for exact interned strings or
# line numbers. Anchor is likewise interned, NOT interval overlap. Body and
# severity are intentionally absent: the verifier may improve them.
type Location is Data:
  At{path: Nat, start: Nat, end: Nat, anchor: Nat}

def sameLocation(a: Location, b: Location) -> Bool:
  match a b:
    case At{p, s, e, an} At{q, t, f, bn}:
      Bool.and(Nat.is_eq(p, q), Bool.and(Nat.is_eq(s, t), Bool.and(Nat.is_eq(e, f), Nat.is_eq(an, bn))))

# known means candidate-map lookup succeeded for this exact ID; unseen means
# the ID has not already appeared in verifier output. This models per-item
# acceptance only. The separate prior-ID preservation loop is not modeled.
def acceptCandidate(known: Bool, unseen: Bool, original: Location, output: Location) -> Bool:
  match known:
    case False{}:
      False{}
    case True{}:
      match unseen:
        case False{}:
          False{}
        case True{}:
          sameLocation(original, output)

# Focused.Run joins discovery outcomes and refuses stageErr before verification.
# The saved-verifier path also requires all four saved discovery stages.
# Each flag stands for an individually validated durable stage, NOT bug truth.
def discoveriesComplete(stages: List<&2, Bool>) -> Bool:
  match stages:
    case Nil{}:
      True{}
    case h <> t:
      match h:
        case False{}:
          False{}
        case True{}:
          discoveriesComplete(t)

# Publication abstracts the final focusedValidateVerification, dependency
# digest check and cancellation check before focusedWriteReport(root).
# verificationOk includes ID/location/prior-ID validation, not semantic truth.
def publishReady(discoveryOk: Bool, verificationOk: Bool, dependencyOk: Bool, uninterrupted: Bool) -> Bool:
  match discoveryOk:
    case False{}:
      False{}
    case True{}:
      Bool.and(verificationOk, Bool.and(dependencyOk, uninterrupted))

def publish(stages: List<&2, Bool>, verificationOk: Bool, dependencyOk: Bool, uninterrupted: Bool) -> Bool:
  publishReady(discoveriesComplete(stages), verificationOk, dependencyOk, uninterrupted)

# focusedCostDelta(total, prior) component abstraction. Nat models exact
# nonnegative integral requests/tokens (or idealized money units), NOT USD
# floats, currency validation, NaN/infinity, epsilon clamping, or overflow.
# total = BaseCost + Ledger; Ledger accumulates EVERY attempt, including failures.
# Returned outstanding cost is reconciled against the caller's PriorCost.
type Delta is Data:
  Invalid{}
  Outstanding{amount: Nat}

def subtractPrior(prior: Nat, total: Nat) -> Delta:
  match prior total:
    case 0n t:
      Outstanding{t}
    case 1n+p 0n:
      Invalid{}
    case 1n+p 1n+t:
      subtractPrior(p, t)

def costDelta(total: Nat, prior: Nat) -> Delta:
  subtractPrior(prior, total)

def ledger(attempts: List<&2, Nat>) -> Nat:
  match attempts:
    case Nil{}:
      0n
    case h <> t:
      Nat.add(h, ledger(t))
