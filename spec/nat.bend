import Base

def eqRefl(n: Nat) -> {Nat.is_eq(n, n) == True{} : Bool}:
  match n:
    case 0n:
      {==}
    case 1n+p:
      eqRefl(p)

def neRefl(n: Nat) -> {Nat.is_ne(n, n) == False{} : Bool}:
  match n:
    case 0n:
      {==}
    case 1n+p:
      neRefl(p)

def addZero(n: Nat) -> {Nat.add(n, 0n) == n : Nat}:
  match n:
    case 0n:
      {==}
    case 1n+p:
      %addZero(p) : {1n+Nat.add(p, 0n) == 1n+_ : Nat}
      {==}

def addAssoc(a: Nat, b: Nat, c: Nat) -> {Nat.add(Nat.add(a, b), c) == Nat.add(a, Nat.add(b, c)) : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %addAssoc(p, b, c) : {1n+Nat.add(Nat.add(p, b), c) == 1n+_ : Nat}
      {==}

def addSuccR(a: Nat, b: Nat) -> {Nat.add(a, 1n+b) == 1n+Nat.add(a, b) : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %addSuccR(p, b) : {1n+Nat.add(p, 1n+b) == 1n+_ : Nat}
      {==}

def LE(a: Nat, b: Nat) -> Data:
  match a b:
    case 0n b0:
      Unit
    case 1n+a1 0n:
      Empty
    case 1n+a1 1n+b1:
      LE(a1, b1)

def FT(b: Bool) -> Type:
  match b:
    case True{}:
      Empty
    case False{}:
      Unit

def fromFalseTrue(e: {False{} == True{} : Bool}) -> Empty:
  %e : FT(_)
  Unit{}

def absurd(A: Type, e: Empty) -> A:
  match e:

# A Cmp motive that refutes any equality claiming to equal LT. Pair it with
# Equal.sym to turn {X == LT} into {LT == X}, then the goal becomes NotLt(X).
def NotLt(c: Cmp) -> Type:
  match c:
    case LT{}:
      Unit
    case EQ{}:
      Empty
    case GT{}:
      Empty

# Nat.cmp(a, b) = LT means a < b, so a + 1 <= b: exactly what needFlush leaves us
# to prove about a chunk that just grew by one file.
def leSuccOfLt(+a: Nat, +b: Nat, e: {Nat.cmp(a, b) == LT{} : Cmp}) -> LE(1n+a, b):
  match a b:
    case 0n 0n:
      %Equal.sym(Cmp, EQ{}, LT{}, e) : NotLt(_)
      Unit{}
    case 0n 1n+bp:
      Unit{}
    case 1n+p 0n:
      %Equal.sym(Cmp, GT{}, LT{}, e) : NotLt(_)
      Unit{}
    case 1n+p 1n+q:
      leSuccOfLt(p, q, e)

def isGeFalse(c: Cmp, e: {Cmp.is_ge(c) == False{} : Bool}) -> {c == LT{} : Cmp}:
  match c:
    case LT{}:
      {==}
    case EQ{}:
      absurd({EQ{} == LT{} : Cmp}, fromFalseTrue(Equal.sym(Bool, True{}, False{}, e)))
    case GT{}:
      absurd({GT{} == LT{} : Cmp}, fromFalseTrue(Equal.sym(Bool, True{}, False{}, e)))

# The motive that refutes {1n+p == 0n}: DiscNat(0n) is Empty, DiscNat(1n+p) is Unit.
def DiscNat(n: Nat) -> Type:
  match n:
    case 0n:
      Empty
    case 1n+p:
      Unit

def leOf(a: Nat, b: Nat, e: {Nat.is_le(a, b) == True{} : Bool}) -> LE(a, b):
  match a b:
    case 0n b0:
      Unit{}
    case 1n+p 0n:
      fromFalseTrue(e)
    case 1n+p 1n+q:
      leOf(p, q, e)

def addComm(+a: Nat, +b: Nat) -> {Nat.add(a, b) == Nat.add(b, a) : Nat}:
  match a:
    case 0n:
      Equal.sym(Nat, Nat.add(b, 0n), b, addZero(b))
    case 1n+p:
      %Equal.sym(Nat, Nat.add(b, 1n+p), 1n+Nat.add(b, p), addSuccR(b, p)) : {1n+Nat.add(p, b) == _ : Nat}
      %addComm(p, b) : {1n+Nat.add(p, b) == 1n+_ : Nat}
      {==}
