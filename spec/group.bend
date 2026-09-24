import Base
import ./nat.bend as N

type FileKind is Data:
  Normal{}
  Test{implStem: Nat, inTestDir: Bool}
  Lock{fam: Nat}
  HeaderC{stem: Nat}
  HeaderCxx{stem: Nat}
  Locale{key: Nat}

type ChangedFile is Data:
  File{id: Nat, dir: Nat, stem: Nat, ext: Nat, kind: FileKind, lines: Nat}

type Edge is Data:
  Edge{from: Nat, to: Nat}

type FamId is Data:
  FamLocale{dir: Nat, key: Nat}
  FamHeaderC{stem: Nat}
  FamHeaderCxx{stem: Nat}
  FamStem{dir: Nat, stem: Nat}
  FamLock{dir: Nat, fam: Nat}

type FamPass is Data:
  PassLocale{}
  PassHeader{}
  PassStem{}
  PassLock{}

type GKey is Data:
  KDir{d: Nat}
  KFile{id: Nat}
  KFam{fam: FamId}

type Item is Data:
  Item{file: ChangedFile, key: GKey}

type Group is Data:
  Group{files: List<&2, ChangedFile>}

type Pathspec is Data:
  SpecDir{}
  SpecFiles{}

type PackSt is Data:
  AtRest{rest: List<&2, ChangedFile>, chunk: List<&2, ChangedFile>, nf: Nat, nl: Nat, acc: List<&2, Group>}
  AtNeed{t: List<&2, ChangedFile>, h: ChangedFile, chunk: List<&2, ChangedFile>, nf: Nat, nl: Nat, acc: List<&2, Group>, need: Bool}

def maxFiles() -> Nat:
  25n

def maxLines() -> Nat:
  2000n

def initKeyAt(id: Nat, dir: Nat, root: Bool) -> GKey:
  match root:
    case True{}:
      KFile{id}
    case False{}:
      KDir{dir}

def initKey(f: ChangedFile) -> GKey:
  match f:
    case File{id, +dir, stem, ext, kind, lines}:
      initKeyAt(id, dir, Nat.is_eq(dir, 0n))

def shouldAttach(testDir: Nat, implDir: Nat, inTestDir: Bool) -> Bool:
  match inTestDir:
    case False{}:
      False{}
    case True{}:
      Nat.is_ne(testDir, implDir)

def uniqueHitsTail(h: Nat, t: List<&2, Nat>) -> Maybe<&2, Nat>:
  match t:
    case Nil{}:
      Some{h}
    case x <> xs:
      None{}

def uniqueHits(hits: List<&2, Nat>) -> Maybe<&2, Nat>:
  match hits:
    case Nil{}:
      None{}
    case h <> t:
      uniqueHitsTail(h, t)

def familyOk(n: Nat) -> Bool:
  Nat.is_ge(n, 2n)

def headerOk(+n: Nat) -> Bool:
  Bool.and(Nat.is_ge(n, 2n), Nat.is_le(n, 4n))

def needFlush(+nf: Nat, nl: Nat, add: Nat) -> Bool:
  match nf:
    case 0n:
      False{}
    case 1n+p:
      Bool.or(Nat.is_ge(nf, maxFiles()), Nat.is_gt(Nat.add(nl, add), maxLines()))

def pathspecExcl(excl: Bool) -> Pathspec:
  match excl:
    case True{}:
      SpecDir{}
    case False{}:
      SpecFiles{}

def pathspec(oneDir: Bool, excl: Bool) -> Pathspec:
  match oneDir:
    case False{}:
      SpecFiles{}
    case True{}:
      pathspecExcl(excl)

def isFamilyKey(k: GKey) -> Bool:
  match k:
    case KDir{d}:
      False{}
    case KFile{id}:
      False{}
    case KFam{fam}:
      True{}

def takeFamily(already: Bool, n: Nat) -> Bool:
  match already:
    case True{}:
      False{}
    case False{}:
      familyOk(n)

def fileId(f: ChangedFile) -> Nat:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      id

def fileDir(f: ChangedFile) -> Nat:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      dir

def fileStem(f: ChangedFile) -> Nat:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      stem

def fileExt(f: ChangedFile) -> Nat:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      ext

def fileKind(f: ChangedFile) -> FileKind:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      kind

def fileLines(f: ChangedFile) -> Nat:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      lines

def bump(b: Bool, n: Nat) -> Nat:
  match b:
    case True{}:
      1n+n
    case False{}:
      n

def countId(+id: Nat, fs: List<&2, ChangedFile>) -> Nat:
  match fs:
    case Nil{}:
      0n
    case File{fid, dir, stem, ext, kind, lines} <> t:
      bump(Nat.is_eq(id, fid), countId(id, t))

def countIdGroups(+id: Nat, gs: List<&2, Group>) -> Nat:
  match gs:
    case Nil{}:
      0n
    case Group{fs} <> t:
      Nat.add(countId(id, fs), countIdGroups(id, t))

def sumLines(fs: List<&2, ChangedFile>) -> Nat:
  match fs:
    case Nil{}:
      0n
    case h <> t:
      Nat.add(fileLines(h), sumLines(t))

def groupFiles(g: Group) -> List<&2, ChangedFile>:
  match g:
    case Group{fs}:
      fs

def capOk(n: Nat, maxN: Nat) -> Bool:
  match maxN:
    case 0n:
      True{}
    case 1n+p:
      Nat.is_le(n, maxN)

def nat2Eq(a: Nat, b: Nat, c: Nat, d: Nat) -> Bool:
  Bool.and(Nat.is_eq(a, c), Nat.is_eq(b, d))

def famEq(a: FamId, b: FamId) -> Bool:
  match a b:
    case FamLocale{d1, k1} FamLocale{d2, k2}:
      nat2Eq(d1, k1, d2, k2)
    case FamHeaderC{s1} FamHeaderC{s2}:
      Nat.is_eq(s1, s2)
    case FamHeaderCxx{s1} FamHeaderCxx{s2}:
      Nat.is_eq(s1, s2)
    case FamStem{d1, s1} FamStem{d2, s2}:
      nat2Eq(d1, s1, d2, s2)
    case FamLock{d1, f1} FamLock{d2, f2}:
      nat2Eq(d1, f1, d2, f2)
    case FamLocale{d, k} b0:
      False{}
    case FamHeaderC{s} b0:
      False{}
    case FamHeaderCxx{s} b0:
      False{}
    case FamStem{d, s} b0:
      False{}
    case FamLock{d, f} b0:
      False{}

def keyEqDir(x: Nat, b: GKey) -> Bool:
  match b:
    case KDir{y}:
      Nat.is_eq(x, y)
    case KFile{id}:
      False{}
    case KFam{fam}:
      False{}

def keyEqFile(x: Nat, b: GKey) -> Bool:
  match b:
    case KDir{d}:
      False{}
    case KFile{y}:
      Nat.is_eq(x, y)
    case KFam{fam}:
      False{}

def keyEqFam(f: FamId, b: GKey) -> Bool:
  match b:
    case KDir{d}:
      False{}
    case KFile{id}:
      False{}
    case KFam{g}:
      famEq(f, g)

def keyEq(a: GKey, b: GKey) -> Bool:
  match a:
    case KDir{x}:
      keyEqDir(x, b)
    case KFile{x}:
      keyEqFile(x, b)
    case KFam{f}:
      keyEqFam(f, b)

def famLocale(dir: Nat, kind: FileKind) -> Maybe<&2, FamId>:
  match kind:
    case Locale{key}:
      Some{FamLocale{dir, key}}
    case Normal{}:
      None{}
    case Test{s, t}:
      None{}
    case Lock{f}:
      None{}
    case HeaderC{s}:
      None{}
    case HeaderCxx{s}:
      None{}

def famHeader(stem: Nat, kind: FileKind) -> Maybe<&2, FamId>:
  match kind:
    case HeaderC{s}:
      Some{FamHeaderC{s}}
    case HeaderCxx{s}:
      Some{FamHeaderCxx{s}}
    case Normal{}:
      None{}
    case Test{a, b}:
      None{}
    case Lock{f}:
      None{}
    case Locale{k}:
      None{}

def famLock(dir: Nat, kind: FileKind) -> Maybe<&2, FamId>:
  match kind:
    case Lock{fam}:
      Some{FamLock{dir, fam}}
    case Normal{}:
      None{}
    case Test{a, b}:
      None{}
    case HeaderC{s}:
      None{}
    case HeaderCxx{s}:
      None{}
    case Locale{k}:
      None{}

def famKind(pass: FamPass, dir: Nat, stem: Nat, kind: FileKind) -> Maybe<&2, FamId>:
  match pass:
    case PassLocale{}:
      famLocale(dir, kind)
    case PassHeader{}:
      famHeader(stem, kind)
    case PassStem{}:
      Some{FamStem{dir, stem}}
    case PassLock{}:
      famLock(dir, kind)

def famOf(pass: FamPass, f: ChangedFile) -> Maybe<&2, FamId>:
  match f:
    case File{id, dir, stem, ext, kind, lines}:
      famKind(pass, dir, stem, kind)

def passMax(pass: FamPass) -> Nat:
  match pass:
    case PassLocale{}:
      0n
    case PassHeader{}:
      4n
    case PassStem{}:
      0n
    case PassLock{}:
      0n

def isTestKind(k: FileKind) -> Bool:
  match k:
    case Test{s, t}:
      True{}
    case Normal{}:
      False{}
    case Lock{f}:
      False{}
    case HeaderC{s}:
      False{}
    case HeaderCxx{s}:
      False{}
    case Locale{key}:
      False{}

def initItems(fs: List<&2, ChangedFile>) -> List<&2, Item>:
  match fs:
    case Nil{}:
      Nil{}
    case +h <> t:
      Item{h, initKey(h)} <> initItems(t)

def famHitM(fam: FamId, m: Maybe<&2, FamId>) -> Bool:
  match m:
    case None{}:
      False{}
    case Some{g}:
      famEq(fam, g)

def famHit(pass: FamPass, fam: FamId, f: ChangedFile) -> Bool:
  famHitM(fam, famOf(pass, f))

def countFamHit(pass: FamPass, fam: FamId, f: ChangedFile, k: GKey) -> Bool:
  Bool.and(Bool.not(isFamilyKey(k)), famHit(pass, fam, f))

def countFam(+pass: FamPass, +fam: FamId, xs: List<&2, Item>) -> Nat:
  match xs:
    case Nil{}:
      0n
    case Item{f, k} <> t:
      bump(countFamHit(pass, fam, f, k), countFam(pass, fam, t))

def rekeyHit(fam: FamId, k: GKey, hit: Bool) -> GKey:
  match hit:
    case True{}:
      KFam{fam}
    case False{}:
      k

def rekeyOne(pass: FamPass, +fam: FamId, f: ChangedFile, +k: GKey) -> GKey:
  rekeyHit(fam, k, countFamHit(pass, fam, f, k))

def rekeyFam(+pass: FamPass, +fam: FamId, xs: List<&2, Item>) -> List<&2, Item>:
  match xs:
    case Nil{}:
      Nil{}
    case Item{+f, k} <> t:
      Item{f, rekeyOne(pass, fam, f, k)} <> rekeyFam(pass, fam, t)

def tryMergeB(pass: FamPass, fam: FamId, xs: List<&2, Item>, ok: Bool) -> List<&2, Item>:
  match ok:
    case True{}:
      rekeyFam(pass, fam, xs)
    case False{}:
      xs

def tryMergeN(pass: FamPass, +fam: FamId, +xs: List<&2, Item>, +n: Nat, maxN: Nat) -> List<&2, Item>:
  tryMergeB(pass, fam, xs, Bool.and(familyOk(n), capOk(n, maxN)))

def tryMergeM(+pass: FamPass, +xs: List<&2, Item>, m: Maybe<&2, FamId>) -> List<&2, Item>:
  match m:
    case None{}:
      xs
    case Some{+fam}:
      tryMergeN(pass, fam, xs, countFam(pass, fam, xs), passMax(pass))

def tryMerge(+pass: FamPass, owner: Item, xs: List<&2, Item>) -> List<&2, Item>:
  match owner:
    case Item{f, k}:
      tryMergeM(pass, xs, famOf(pass, f))

def mergeFrom(+pass: FamPass, owners: List<&2, Item>, xs: List<&2, Item>) -> List<&2, Item>:
  match owners:
    case Nil{}:
      xs
    case h <> t:
      mergeFrom(pass, t, tryMerge(pass, h, xs))

def mergePass(pass: FamPass, +xs: List<&2, Item>) -> List<&2, Item>:
  mergeFrom(pass, xs, xs)

def mergeAll(xs: List<&2, Item>) -> List<&2, Item>:
  mergePass(PassLock{}, mergePass(PassStem{}, mergePass(PassHeader{}, mergePass(PassLocale{}, xs))))

type Found is Data:
  FoundNone{}
  FoundOne{item: Item}
  FoundDup{}

def itemFile(it: Item) -> ChangedFile:
  match it:
    case Item{f, k}:
      f

def itemKey(it: Item) -> GKey:
  match it:
    case Item{f, k}:
      k

def filesOf(xs: List<&2, Item>) -> List<&2, ChangedFile>:
  match xs:
    case Nil{}:
      Nil{}
    case Item{f, k} <> t:
      f <> filesOf(t)

def initFiles(fs: List<&2, ChangedFile>) -> {filesOf(initItems(fs)) == fs : List<&2, ChangedFile>}:
  match fs:
    case Nil{}:
      {==}
    case +h <> +t:
      %initFiles(t) : {h <> filesOf(initItems(t)) == h <> _ : List<&2, ChangedFile>}
      {==}

def implMatches(+stem: Nat, +ext: Nat, f: ChangedFile) -> Bool:
  match f:
    case File{id, dir, st, ex, kind, lines}:
      Bool.and(Bool.not(isTestKind(kind)), Bool.and(Nat.is_eq(stem, st), Nat.is_eq(ext, ex)))

def addFoundHit(acc: Found, item: Item) -> Found:
  match acc:
    case FoundNone{}:
      FoundOne{item}
    case FoundOne{x}:
      FoundDup{}
    case FoundDup{}:
      FoundDup{}

def addFound(acc: Found, item: Item, hit: Bool) -> Found:
  match hit:
    case False{}:
      acc
    case True{}:
      addFoundHit(acc, item)

def foundItem(acc: Found) -> Maybe<&2, Item>:
  match acc:
    case FoundNone{}:
      None{}
    case FoundOne{item}:
      Some{item}
    case FoundDup{}:
      None{}

def uniqueImplGo(+stem: Nat, +ext: Nat, xs: List<&2, Item>, acc: Found) -> Found:
  match xs:
    case Nil{}:
      acc
    case +h <> t:
      uniqueImplGo(stem, ext, t, addFound(acc, h, implMatches(stem, ext, itemFile(h))))

def uniqueImpl(stem: Nat, ext: Nat, xs: List<&2, Item>) -> Maybe<&2, Item>:
  foundItem(uniqueImplGo(stem, ext, xs, FoundNone{}))

@unsafe def remapId(+id: Nat, +newKey: GKey, xs: List<&2, Item>) -> List<&2, Item>:
  match xs:
    case Nil{}:
      Nil{}
    case Item{+f, k} <> t:
      remapIdAt(id, newKey, f, k, t, Nat.is_eq(id, fileId(f)))

def remapIdAt(+id: Nat, +newKey: GKey, f: ChangedFile, k: GKey, t: List<&2, Item>, eq: Bool) -> List<&2, Item>:
  match eq:
    case True{}:
      Item{f, newKey} <> remapId(id, newKey, t)
    case False{}:
      Item{f, k} <> remapId(id, newKey, t)

def testInDirK(k: FileKind) -> Bool:
  match k:
    case Test{s, t}:
      t
    case Normal{}:
      False{}
    case Lock{x}:
      False{}
    case HeaderC{s}:
      False{}
    case HeaderCxx{s}:
      False{}
    case Locale{key}:
      False{}

def testInDir(f: ChangedFile) -> Bool:
  testInDirK(fileKind(f))

def attachHitB(test: Item, impl: Item, xs: List<&2, Item>, ok: Bool) -> List<&2, Item>:
  match ok:
    case True{}:
      remapId(fileId(itemFile(test)), itemKey(impl), xs)
    case False{}:
      xs

def attachHit(+test: Item, +impl: Item, xs: List<&2, Item>) -> List<&2, Item>:
  attachHitB(test, impl, xs, shouldAttach(fileDir(itemFile(test)), fileDir(itemFile(impl)), testInDir(itemFile(test))))

def attachSome(test: Item, m: Maybe<&2, Item>, xs: List<&2, Item>) -> List<&2, Item>:
  match m:
    case None{}:
      xs
    case Some{impl}:
      attachHit(test, impl, xs)

def attachOneK(+it: Item, +xs: List<&2, Item>, k: FileKind) -> List<&2, Item>:
  match k:
    case Test{stem, td}:
      attachSome(it, uniqueImpl(stem, fileExt(itemFile(it)), xs), xs)
    case Normal{}:
      xs
    case Lock{f}:
      xs
    case HeaderC{s}:
      xs
    case HeaderCxx{s}:
      xs
    case Locale{key}:
      xs

def attachOne(+it: Item, xs: List<&2, Item>) -> List<&2, Item>:
  attachOneK(it, xs, fileKind(itemFile(it)))

def attachFrom(todo: List<&2, Item>, xs: List<&2, Item>) -> List<&2, Item>:
  match todo:
    case Nil{}:
      xs
    case h <> t:
      attachFrom(t, attachOne(h, xs))

def attachAll(+xs: List<&2, Item>) -> List<&2, Item>:
  attachFrom(xs, xs)

@unsafe def keyOf(+id: Nat, xs: List<&2, Item>) -> Maybe<&2, GKey>:
  match xs:
    case Nil{}:
      None{}
    case Item{+f, k} <> t:
      keyOfAt(id, f, k, t, Nat.is_eq(id, fileId(f)))

def keyOfAt(+id: Nat, f: ChangedFile, +k: GKey, t: List<&2, Item>, eq: Bool) -> Maybe<&2, GKey>:
  match eq:
    case True{}:
      Some{k}
    case False{}:
      keyOf(id, t)

@unsafe def remapKey(+old: GKey, +new: GKey, xs: List<&2, Item>) -> List<&2, Item>:
  match xs:
    case Nil{}:
      Nil{}
    case Item{f, +k} <> t:
      remapKeyAt(old, new, f, k, t, keyEq(old, k))

def remapKeyAt(+old: GKey, +new: GKey, f: ChangedFile, k: GKey, t: List<&2, Item>, eq: Bool) -> List<&2, Item>:
  match eq:
    case True{}:
      Item{f, new} <> remapKey(old, new, t)
    case False{}:
      Item{f, k} <> remapKey(old, new, t)

def unionKeys(xs: List<&2, Item>, ka: Maybe<&2, GKey>, kb: Maybe<&2, GKey>) -> List<&2, Item>:
  match ka kb:
    case None{} kb0:
      xs
    case Some{a} None{}:
      xs
    case Some{a} Some{b}:
      remapKey(b, a, xs)

def applyEdge(+xs: List<&2, Item>, e: Edge) -> List<&2, Item>:
  match e:
    case Edge{+a, +b}:
      unionKeys(xs, keyOf(a, xs), keyOf(b, xs))

def applyEdges(es: List<&2, Edge>, xs: List<&2, Item>) -> List<&2, Item>:
  match es:
    case Nil{}:
      xs
    case h <> t:
      applyEdges(t, applyEdge(xs, h))

type Bucket is Data:
  Bucket{key: GKey, files: List<&2, ChangedFile>}

@unsafe def collectKey(+k: GKey, t: List<&2, Bucket>) -> List<&2, ChangedFile>:
  match t:
    case Nil{}:
      Nil{}
    case Bucket{+k2, fs} <> rest:
      collectEq(k, k2, fs, rest, keyEq(k, k2))

def collectEq(+k: GKey, k2: GKey, fs: List<&2, ChangedFile>, rest: List<&2, Bucket>, eq: Bool) -> List<&2, ChangedFile>:
  match eq:
    case True{}:
      List.append(&2, ChangedFile, fs, collectKey(k, rest))
    case False{}:
      collectKey(k, rest)

@unsafe def skipKey(+k: GKey, t: List<&2, Bucket>) -> List<&2, Bucket>:
  match t:
    case Nil{}:
      Nil{}
    case Bucket{+k2, fs} <> rest:
      skipEq(k, k2, fs, rest, keyEq(k, k2))

def skipEq(+k: GKey, k2: GKey, fs: List<&2, ChangedFile>, rest: List<&2, Bucket>, eq: Bool) -> List<&2, Bucket>:
  match eq:
    case True{}:
      skipKey(k, rest)
    case False{}:
      Bucket{k2, fs} <> skipKey(k, rest)

@unsafe def mergeBuckets(bs: List<&2, Bucket>) -> List<&2, Group>:
  match bs:
    case Nil{}:
      Nil{}
    case Bucket{+k, fs} <> +t:
      Group{List.append(&2, ChangedFile, fs, collectKey(k, t))} <> mergeBuckets(skipKey(k, t))

def singletons(xs: List<&2, Item>) -> List<&2, Bucket>:
  match xs:
    case Nil{}:
      Nil{}
    case Item{f, k} <> t:
      Bucket{k, [f]} <> singletons(t)

def bucket(xs: List<&2, Item>) -> List<&2, Group>:
  mergeBuckets(singletons(xs))

def flushEnd(chunk: List<&2, ChangedFile>, acc: List<&2, Group>) -> List<&2, Group>:
  match chunk:
    case Nil{}:
      acc
    case h <> t:
      List.append(&2, Group, acc, [Group{h <> t}])

def snocFile(chunk: List<&2, ChangedFile>, h: ChangedFile) -> List<&2, ChangedFile>:
  List.append(&2, ChangedFile, chunk, [h])

def snocGroup(acc: List<&2, Group>, chunk: List<&2, ChangedFile>) -> List<&2, Group>:
  List.append(&2, Group, acc, [Group{chunk}])

@unsafe def pack(rest: List<&2, ChangedFile>, chunk: List<&2, ChangedFile>, +nf: Nat, +nl: Nat, acc: List<&2, Group>) -> List<&2, Group>:
  match rest:
    case Nil{}:
      flushEnd(chunk, acc)
    case +h <> t:
      packNeed(t, h, chunk, nf, nl, acc, needFlush(nf, nl, fileLines(h)))

def packNeed(t: List<&2, ChangedFile>, +h: ChangedFile, chunk: List<&2, ChangedFile>, nf: Nat, nl: Nat, acc: List<&2, Group>, need: Bool) -> List<&2, Group>:
  match need:
    case True{}:
      pack(t, [h], 1n, fileLines(h), snocGroup(acc, chunk))
    case False{}:
      pack(t, snocFile(chunk, h), 1n+nf, Nat.add(nl, fileLines(h)), acc)

def oversized(g: Group) -> Bool:
  match g:
    case Group{+fs}:
      Bool.or(Nat.is_gt(List.length(&2, ChangedFile, fs), maxFiles()), Nat.is_gt(sumLines(fs), maxLines()))

def splitOneB(g: Group, big: Bool) -> List<&2, Group>:
  match big:
    case False{}:
      [g]
    case True{}:
      pack(groupFiles(g), Nil{}, 0n, 0n, Nil{})

def splitOne(+g: Group) -> List<&2, Group>:
  splitOneB(g, oversized(g))

def splitAll(gs: List<&2, Group>) -> List<&2, Group>:
  match gs:
    case Nil{}:
      Nil{}
    case h <> t:
      List.append(&2, Group, splitOne(h), splitAll(t))

def cluster(fs: List<&2, ChangedFile>, es: List<&2, Edge>) -> List<&2, Group>:
  splitAll(bucket(applyEdges(es, attachAll(mergeAll(initItems(fs))))))

def CappedTail(h: ChangedFile, t: List<&2, ChangedFile>) -> Type:
  match t:
    case Nil{}:
      Unit
    case x <> xs:
      N.LE(List.length(&2, ChangedFile, h <> x <> xs), maxFiles()) & N.LE(sumLines(h <> x <> xs), maxLines())

def CappedFiles(fs: List<&2, ChangedFile>) -> Type:
  match fs:
    case Nil{}:
      Unit
    case h <> t:
      CappedTail(h, t)

def AllCapped(gs: List<&2, Group>) -> Type:
  match gs:
    case Nil{}:
      Unit
    case Group{fs} <> t:
      (CappedFiles(fs) & AllCapped(t))

def inFiles(+id: Nat, fs: List<&2, ChangedFile>) -> Bool:
  match fs:
    case Nil{}:
      False{}
    case File{fid, dir, stem, ext, kind, lines} <> t:
      Bool.or(Nat.is_eq(id, fid), inFiles(id, t))

def inGroups(+id: Nat, gs: List<&2, Group>) -> Bool:
  match gs:
    case Nil{}:
      False{}
    case Group{fs} <> t:
      Bool.or(inFiles(id, fs), inGroups(id, t))

def sameGroup(+a: Nat, +b: Nat, gs: List<&2, Group>) -> Bool:
  match gs:
    case Nil{}:
      False{}
    case Group{+fs} <> t:
      Bool.or(Bool.and(inFiles(a, fs), inFiles(b, fs)), sameGroup(a, b, t))

def rekeyFiles(+pass: FamPass, +fam: FamId, xs: List<&2, Item>) -> {filesOf(rekeyFam(pass, fam, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match xs:
    case Nil{}:
      {==}
    case Item{+f, k} <> +t:
      %rekeyFiles(pass, fam, t) : {f <> filesOf(rekeyFam(pass, fam, t)) == f <> _ : List<&2, ChangedFile>}
      {==}

def tryMergeBFiles(pass: FamPass, fam: FamId, +xs: List<&2, Item>, ok: Bool) -> {filesOf(tryMergeB(pass, fam, xs, ok)) == filesOf(xs) : List<&2, ChangedFile>}:
  match ok:
    case True{}:
      rekeyFiles(pass, fam, xs)
    case False{}:
      {==}

def tryMergeNFiles(pass: FamPass, +fam: FamId, +xs: List<&2, Item>, +n: Nat, maxN: Nat) -> {filesOf(tryMergeN(pass, fam, xs, n, maxN)) == filesOf(xs) : List<&2, ChangedFile>}:
  tryMergeBFiles(pass, fam, xs, Bool.and(familyOk(n), capOk(n, maxN)))

def tryMergeMFiles(+pass: FamPass, +xs: List<&2, Item>, m: Maybe<&2, FamId>) -> {filesOf(tryMergeM(pass, xs, m)) == filesOf(xs) : List<&2, ChangedFile>}:
  match m:
    case None{}:
      {==}
    case Some{+fam}:
      tryMergeNFiles(pass, fam, xs, countFam(pass, fam, xs), passMax(pass))

def tryMergeFiles(+pass: FamPass, owner: Item, xs: List<&2, Item>) -> {filesOf(tryMerge(pass, owner, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match owner:
    case Item{f, k}:
      tryMergeMFiles(pass, xs, famOf(pass, f))

def mergeFromFiles(+pass: FamPass, owners: List<&2, Item>, +xs: List<&2, Item>) -> {filesOf(mergeFrom(pass, owners, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match owners:
    case Nil{}:
      {==}
    case +h <> +t:
      %tryMergeFiles(pass, h, xs) : {filesOf(mergeFrom(pass, t, tryMerge(pass, h, xs))) == _ : List<&2, ChangedFile>}
      mergeFromFiles(pass, t, tryMerge(pass, h, xs))

def mergePassFiles(pass: FamPass, +xs: List<&2, Item>) -> {filesOf(mergePass(pass, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  mergeFromFiles(pass, xs, xs)

def mergeAllFilesGo(+xs: List<&2, Item>, +xs1: List<&2, Item>, e1: {filesOf(xs1) == filesOf(xs) : List<&2, ChangedFile>}) -> {filesOf(mergePass(PassLock{}, mergePass(PassStem{}, mergePass(PassHeader{}, xs1)))) == filesOf(xs) : List<&2, ChangedFile>}:
  %Equal.sym(List<&2, ChangedFile>, filesOf(mergePass(PassLock{}, mergePass(PassStem{}, mergePass(PassHeader{}, xs1)))), filesOf(mergePass(PassStem{}, mergePass(PassHeader{}, xs1))), mergePassFiles(PassLock{}, mergePass(PassStem{}, mergePass(PassHeader{}, xs1)))) : {_ == filesOf(xs) : List<&2, ChangedFile>}
  %Equal.sym(List<&2, ChangedFile>, filesOf(mergePass(PassStem{}, mergePass(PassHeader{}, xs1))), filesOf(mergePass(PassHeader{}, xs1)), mergePassFiles(PassStem{}, mergePass(PassHeader{}, xs1))) : {_ == filesOf(xs) : List<&2, ChangedFile>}
  %Equal.sym(List<&2, ChangedFile>, filesOf(mergePass(PassHeader{}, xs1)), filesOf(xs1), mergePassFiles(PassHeader{}, xs1)) : {_ == filesOf(xs) : List<&2, ChangedFile>}
  e1

def mergeAllFiles(+xs: List<&2, Item>) -> {filesOf(mergeAll(xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  mergeAllFilesGo(xs, mergePass(PassLocale{}, xs), mergePassFiles(PassLocale{}, xs))

@unsafe def remapIdFiles(+id: Nat, +newKey: GKey, xs: List<&2, Item>) -> {filesOf(remapId(id, newKey, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match xs:
    case Nil{}:
      {==}
    case Item{+f, k} <> +t:
      remapIdFilesAt(id, newKey, f, k, t, Nat.is_eq(id, fileId(f)))

def remapIdFilesAt(+id: Nat, +newKey: GKey, +f: ChangedFile, k: GKey, +t: List<&2, Item>, eq: Bool) -> {filesOf(remapIdAt(id, newKey, f, k, t, eq)) == f <> filesOf(t) : List<&2, ChangedFile>}:
  match eq:
    case True{}:
      %remapIdFiles(id, newKey, t) : {f <> filesOf(remapId(id, newKey, t)) == f <> _ : List<&2, ChangedFile>}
      {==}
    case False{}:
      %remapIdFiles(id, newKey, t) : {f <> filesOf(remapId(id, newKey, t)) == f <> _ : List<&2, ChangedFile>}
      {==}

def attachHitBFiles(test: Item, impl: Item, +xs: List<&2, Item>, ok: Bool) -> {filesOf(attachHitB(test, impl, xs, ok)) == filesOf(xs) : List<&2, ChangedFile>}:
  match ok:
    case True{}:
      remapIdFiles(fileId(itemFile(test)), itemKey(impl), xs)
    case False{}:
      {==}

def attachHitFiles(+test: Item, +impl: Item, xs: List<&2, Item>) -> {filesOf(attachHit(test, impl, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  attachHitBFiles(test, impl, xs, shouldAttach(fileDir(itemFile(test)), fileDir(itemFile(impl)), testInDir(itemFile(test))))

def attachSomeFiles(test: Item, m: Maybe<&2, Item>, xs: List<&2, Item>) -> {filesOf(attachSome(test, m, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match m:
    case None{}:
      {==}
    case Some{impl}:
      attachHitFiles(test, impl, xs)

def attachOneKFiles(+it: Item, +xs: List<&2, Item>, k: FileKind) -> {filesOf(attachOneK(it, xs, k)) == filesOf(xs) : List<&2, ChangedFile>}:
  match k:
    case Test{stem, td}:
      attachSomeFiles(it, uniqueImpl(stem, fileExt(itemFile(it)), xs), xs)
    case Normal{}:
      {==}
    case Lock{f}:
      {==}
    case HeaderC{s}:
      {==}
    case HeaderCxx{s}:
      {==}
    case Locale{key}:
      {==}

def attachOneFiles(+it: Item, xs: List<&2, Item>) -> {filesOf(attachOne(it, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  attachOneKFiles(it, xs, fileKind(itemFile(it)))

def attachFromFiles(todo: List<&2, Item>, +xs: List<&2, Item>) -> {filesOf(attachFrom(todo, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match todo:
    case Nil{}:
      {==}
    case +h <> +t:
      %attachOneFiles(h, xs) : {filesOf(attachFrom(t, attachOne(h, xs))) == _ : List<&2, ChangedFile>}
      attachFromFiles(t, attachOne(h, xs))

def attachAllFiles(+xs: List<&2, Item>) -> {filesOf(attachAll(xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  attachFromFiles(xs, xs)

@unsafe def remapKeyFiles(+old: GKey, +new: GKey, xs: List<&2, Item>) -> {filesOf(remapKey(old, new, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match xs:
    case Nil{}:
      {==}
    case Item{+f, +k} <> +t:
      remapKeyFilesAt(old, new, f, k, t, keyEq(old, k))

def remapKeyFilesAt(+old: GKey, +new: GKey, +f: ChangedFile, k: GKey, +t: List<&2, Item>, eq: Bool) -> {filesOf(remapKeyAt(old, new, f, k, t, eq)) == f <> filesOf(t) : List<&2, ChangedFile>}:
  match eq:
    case True{}:
      %remapKeyFiles(old, new, t) : {f <> filesOf(remapKey(old, new, t)) == f <> _ : List<&2, ChangedFile>}
      {==}
    case False{}:
      %remapKeyFiles(old, new, t) : {f <> filesOf(remapKey(old, new, t)) == f <> _ : List<&2, ChangedFile>}
      {==}

def unionKeysFiles(+xs: List<&2, Item>, ka: Maybe<&2, GKey>, kb: Maybe<&2, GKey>) -> {filesOf(unionKeys(xs, ka, kb)) == filesOf(xs) : List<&2, ChangedFile>}:
  match ka kb:
    case None{} kb0:
      {==}
    case Some{a} None{}:
      {==}
    case Some{a} Some{b}:
      remapKeyFiles(b, a, xs)

def applyEdgeFiles(+xs: List<&2, Item>, e: Edge) -> {filesOf(applyEdge(xs, e)) == filesOf(xs) : List<&2, ChangedFile>}:
  match e:
    case Edge{+a, +b}:
      unionKeysFiles(xs, keyOf(a, xs), keyOf(b, xs))

def applyEdgesFiles(es: List<&2, Edge>, +xs: List<&2, Item>) -> {filesOf(applyEdges(es, xs)) == filesOf(xs) : List<&2, ChangedFile>}:
  match es:
    case Nil{}:
      {==}
    case +h <> +t:
      %applyEdgeFiles(xs, h) : {filesOf(applyEdges(t, applyEdge(xs, h))) == _ : List<&2, ChangedFile>}
      applyEdgesFiles(t, applyEdge(xs, h))

def bumpAddR(b: Bool, x: Nat, y: Nat) -> {bump(b, Nat.add(x, y)) == Nat.add(bump(b, x), y) : Nat}:
  match b:
    case True{}:
      {==}
    case False{}:
      {==}

def countIdAppend(+id: Nat, a: List<&2, ChangedFile>, +b: List<&2, ChangedFile>) -> {countId(id, List.append(&2, ChangedFile, a, b)) == Nat.add(countId(id, a), countId(id, b)) : Nat}:
  match a:
    case Nil{}:
      {==}
    case File{+fid, dir, stem, ext, kind, lines} <> +t:
      %Equal.sym(Nat, countId(id, List.append(&2, ChangedFile, t, b)), Nat.add(countId(id, t), countId(id, b)), countIdAppend(id, t, b)) : {bump(Nat.is_eq(id, fid), _) == Nat.add(bump(Nat.is_eq(id, fid), countId(id, t)), countId(id, b)) : Nat}
      bumpAddR(Nat.is_eq(id, fid), countId(id, t), countId(id, b))

def countIdBuckets(+id: Nat, bs: List<&2, Bucket>) -> Nat:
  match bs:
    case Nil{}:
      0n
    case Bucket{k, fs} <> t:
      Nat.add(countId(id, fs), countIdBuckets(id, t))

def bumpAdd(b: Bool, n: Nat) -> {Nat.add(bump(b, 0n), n) == bump(b, n) : Nat}:
  match b:
    case True{}:
      {==}
    case False{}:
      {==}

def addBump0(+n: Nat, b: Bool) -> {Nat.add(n, bump(b, 0n)) == bump(b, n) : Nat}:
  match b:
    case True{}:
      %Equal.sym(Nat, Nat.add(n, 1n), 1n+Nat.add(n, 0n), N.addSuccR(n, 0n)) : {_ == 1n+n : Nat}
      %Equal.sym(Nat, Nat.add(n, 0n), n, N.addZero(n)) : {1n+_ == 1n+n : Nat}
      {==}
    case False{}:
      N.addZero(n)

def singCount(+id: Nat, xs: List<&2, Item>) -> {countIdBuckets(id, singletons(xs)) == countId(id, filesOf(xs)) : Nat}:
  match xs:
    case Nil{}:
      {==}
    case Item{File{+fid, dir, stem, ext, kind, lines}, k} <> +t:
      %Equal.sym(Nat, countIdBuckets(id, singletons(t)), countId(id, filesOf(t)), singCount(id, t)) : {Nat.add(bump(Nat.is_eq(id, fid), 0n), _) == bump(Nat.is_eq(id, fid), countId(id, filesOf(t))) : Nat}
      bumpAdd(Nat.is_eq(id, fid), countId(id, filesOf(t)))

@unsafe def collectSkip(+id: Nat, +k: GKey, t: List<&2, Bucket>) -> {Nat.add(countId(id, collectKey(k, t)), countIdBuckets(id, skipKey(k, t))) == countIdBuckets(id, t) : Nat}:
  match t:
    case Nil{}:
      {==}
    case Bucket{+k2, +fs} <> +rest:
      collectSkipEq(id, k, k2, fs, rest, keyEq(k, k2))

def collectSkipEq(+id: Nat, +k: GKey, k2: GKey, +fs: List<&2, ChangedFile>, +rest: List<&2, Bucket>, eq: Bool) -> {Nat.add(countId(id, collectEq(k, k2, fs, rest, eq)), countIdBuckets(id, skipEq(k, k2, fs, rest, eq))) == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}:
  match eq:
    case True{}:
      %Equal.sym(Nat, countId(id, List.append(&2, ChangedFile, fs, collectKey(k, rest))), Nat.add(countId(id, fs), countId(id, collectKey(k, rest))), countIdAppend(id, fs, collectKey(k, rest))) : {Nat.add(_, countIdBuckets(id, skipKey(k, rest))) == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}
      %Equal.sym(Nat, Nat.add(Nat.add(countId(id, fs), countId(id, collectKey(k, rest))), countIdBuckets(id, skipKey(k, rest))), Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))), N.addAssoc(countId(id, fs), countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))) : {_ == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}
      %collectSkip(id, k, rest) : {Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))) == Nat.add(countId(id, fs), _) : Nat}
      {==}
    case False{}:
      %N.addAssoc(countId(id, collectKey(k, rest)), countId(id, fs), countIdBuckets(id, skipKey(k, rest))) : {_ == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}
      %Equal.sym(Nat, Nat.add(countId(id, collectKey(k, rest)), countId(id, fs)), Nat.add(countId(id, fs), countId(id, collectKey(k, rest))), N.addComm(countId(id, collectKey(k, rest)), countId(id, fs))) : {Nat.add(_, countIdBuckets(id, skipKey(k, rest))) == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}
      %Equal.sym(Nat, Nat.add(Nat.add(countId(id, fs), countId(id, collectKey(k, rest))), countIdBuckets(id, skipKey(k, rest))), Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))), N.addAssoc(countId(id, fs), countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))) : {_ == Nat.add(countId(id, fs), countIdBuckets(id, rest)) : Nat}
      %collectSkip(id, k, rest) : {Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, rest)), countIdBuckets(id, skipKey(k, rest)))) == Nat.add(countId(id, fs), _) : Nat}
      {==}

@unsafe def mergeCount(+id: Nat, bs: List<&2, Bucket>) -> {countIdGroups(id, mergeBuckets(bs)) == countIdBuckets(id, bs) : Nat}:
  match bs:
    case Nil{}:
      {==}
    case Bucket{+k, +fs} <> +t:
      %Equal.sym(Nat, countId(id, List.append(&2, ChangedFile, fs, collectKey(k, t))), Nat.add(countId(id, fs), countId(id, collectKey(k, t))), countIdAppend(id, fs, collectKey(k, t))) : {Nat.add(_, countIdGroups(id, mergeBuckets(skipKey(k, t)))) == Nat.add(countId(id, fs), countIdBuckets(id, t)) : Nat}
      %Equal.sym(Nat, countIdGroups(id, mergeBuckets(skipKey(k, t))), countIdBuckets(id, skipKey(k, t)), mergeCount(id, skipKey(k, t))) : {Nat.add(Nat.add(countId(id, fs), countId(id, collectKey(k, t))), _) == Nat.add(countId(id, fs), countIdBuckets(id, t)) : Nat}
      %Equal.sym(Nat, Nat.add(Nat.add(countId(id, fs), countId(id, collectKey(k, t))), countIdBuckets(id, skipKey(k, t))), Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, t)), countIdBuckets(id, skipKey(k, t)))), N.addAssoc(countId(id, fs), countId(id, collectKey(k, t)), countIdBuckets(id, skipKey(k, t)))) : {_ == Nat.add(countId(id, fs), countIdBuckets(id, t)) : Nat}
      %collectSkip(id, k, t) : {Nat.add(countId(id, fs), Nat.add(countId(id, collectKey(k, t)), countIdBuckets(id, skipKey(k, t)))) == Nat.add(countId(id, fs), _) : Nat}
      {==}

def bucketCount(+id: Nat, +xs: List<&2, Item>) -> {countIdGroups(id, bucket(xs)) == countId(id, filesOf(xs)) : Nat}:
  %Equal.sym(Nat, countIdGroups(id, mergeBuckets(singletons(xs))), countIdBuckets(id, singletons(xs)), mergeCount(id, singletons(xs))) : {_ == countId(id, filesOf(xs)) : Nat}
  singCount(id, xs)

def countIdGroupsAppend(+id: Nat, a: List<&2, Group>, +b: List<&2, Group>) -> {countIdGroups(id, List.append(&2, Group, a, b)) == Nat.add(countIdGroups(id, a), countIdGroups(id, b)) : Nat}:
  match a:
    case Nil{}:
      {==}
    case Group{+fs} <> +t:
      %Equal.sym(Nat, countIdGroups(id, List.append(&2, Group, t, b)), Nat.add(countIdGroups(id, t), countIdGroups(id, b)), countIdGroupsAppend(id, t, b)) : {Nat.add(countId(id, fs), _) == Nat.add(Nat.add(countId(id, fs), countIdGroups(id, t)), countIdGroups(id, b)) : Nat}
      %N.addAssoc(countId(id, fs), countIdGroups(id, t), countIdGroups(id, b)) : {_ == Nat.add(Nat.add(countId(id, fs), countIdGroups(id, t)), countIdGroups(id, b)) : Nat}
      {==}

def flushCount(+id: Nat, chunk: List<&2, ChangedFile>, +acc: List<&2, Group>) -> {countIdGroups(id, flushEnd(chunk, acc)) == Nat.add(countId(id, chunk), countIdGroups(id, acc)) : Nat}:
  match chunk:
    case Nil{}:
      {==}
    case +h <> +t:
      %Equal.sym(Nat, countIdGroups(id, List.append(&2, Group, acc, [Group{h <> t}])), Nat.add(countIdGroups(id, acc), countIdGroups(id, [Group{h <> t}])), countIdGroupsAppend(id, acc, [Group{h <> t}])) : {_ == Nat.add(countId(id, h <> t), countIdGroups(id, acc)) : Nat}
      %Equal.sym(Nat, Nat.add(countId(id, h <> t), 0n), countId(id, h <> t), N.addZero(countId(id, h <> t))) : {Nat.add(countIdGroups(id, acc), _) == Nat.add(countId(id, h <> t), countIdGroups(id, acc)) : Nat}
      N.addComm(countIdGroups(id, acc), countId(id, h <> t))

def packTrueN(+ct: Nat, +cc: Nat, +ca: Nat, +b: Bool) -> {Nat.add(ct, Nat.add(bump(b, 0n), Nat.add(ca, Nat.add(cc, 0n)))) == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}:
  %Equal.sym(Nat, Nat.add(cc, 0n), cc, N.addZero(cc)) : {Nat.add(ct, Nat.add(bump(b, 0n), Nat.add(ca, _))) == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}
  %Equal.sym(Nat, Nat.add(ca, cc), Nat.add(cc, ca), N.addComm(ca, cc)) : {Nat.add(ct, Nat.add(bump(b, 0n), _)) == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}
  %N.addAssoc(ct, bump(b, 0n), Nat.add(cc, ca)) : {_ == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}
  %Equal.sym(Nat, Nat.add(ct, bump(b, 0n)), bump(b, ct), addBump0(ct, b)) : {Nat.add(_, Nat.add(cc, ca)) == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}
  {==}

def packCountTrueF(+id: Nat, +t: List<&2, ChangedFile>, +fid: Nat, dir: Nat, stem: Nat, ext: Nat, kind: FileKind, lines: Nat, +chunk: List<&2, ChangedFile>, +acc: List<&2, Group>) -> {Nat.add(countId(id, t), Nat.add(countId(id, [File{fid, dir, stem, ext, kind, lines}]), countIdGroups(id, snocGroup(acc, chunk)))) == Nat.add(countId(id, File{fid, dir, stem, ext, kind, lines} <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  %Equal.sym(Nat, countIdGroups(id, snocGroup(acc, chunk)), Nat.add(countIdGroups(id, acc), countIdGroups(id, [Group{chunk}])), countIdGroupsAppend(id, acc, [Group{chunk}])) : {Nat.add(countId(id, t), Nat.add(bump(Nat.is_eq(id, fid), 0n), _)) == Nat.add(bump(Nat.is_eq(id, fid), countId(id, t)), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}
  packTrueN(countId(id, t), countId(id, chunk), countIdGroups(id, acc), Nat.is_eq(id, fid))

def packCountTrue(+id: Nat, t: List<&2, ChangedFile>, h: ChangedFile, +chunk: List<&2, ChangedFile>, +acc: List<&2, Group>) -> {Nat.add(countId(id, t), Nat.add(countId(id, [h]), countIdGroups(id, snocGroup(acc, chunk)))) == Nat.add(countId(id, h <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  match h:
    case File{+fid, dir, stem, ext, kind, lines}:
      packCountTrueF(id, t, fid, dir, stem, ext, kind, lines, chunk, acc)

def packFalseN(+ct: Nat, +cc: Nat, +ca: Nat, b: Bool) -> {Nat.add(ct, Nat.add(Nat.add(cc, bump(b, 0n)), ca)) == Nat.add(bump(b, ct), Nat.add(cc, ca)) : Nat}:
  match b:
    case False{}:
      %Equal.sym(Nat, Nat.add(cc, 0n), cc, N.addZero(cc)) : {Nat.add(ct, Nat.add(_, ca)) == Nat.add(ct, Nat.add(cc, ca)) : Nat}
      {==}
    case True{}:
      %Equal.sym(Nat, Nat.add(cc, 1n), 1n+Nat.add(cc, 0n), N.addSuccR(cc, 0n)) : {Nat.add(ct, Nat.add(_, ca)) == Nat.add(1n+ct, Nat.add(cc, ca)) : Nat}
      %Equal.sym(Nat, Nat.add(cc, 0n), cc, N.addZero(cc)) : {Nat.add(ct, Nat.add(1n+_, ca)) == Nat.add(1n+ct, Nat.add(cc, ca)) : Nat}
      %Equal.sym(Nat, Nat.add(ct, 1n+Nat.add(cc, ca)), 1n+Nat.add(ct, Nat.add(cc, ca)), N.addSuccR(ct, Nat.add(cc, ca))) : {_ == Nat.add(1n+ct, Nat.add(cc, ca)) : Nat}
      {==}

def packCountFalseF(+id: Nat, +t: List<&2, ChangedFile>, +fid: Nat, dir: Nat, stem: Nat, ext: Nat, kind: FileKind, lines: Nat, +chunk: List<&2, ChangedFile>, +acc: List<&2, Group>) -> {Nat.add(countId(id, t), Nat.add(countId(id, snocFile(chunk, File{fid, dir, stem, ext, kind, lines})), countIdGroups(id, acc))) == Nat.add(countId(id, File{fid, dir, stem, ext, kind, lines} <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  %Equal.sym(Nat, countId(id, snocFile(chunk, File{fid, dir, stem, ext, kind, lines})), Nat.add(countId(id, chunk), countId(id, [File{fid, dir, stem, ext, kind, lines}])), countIdAppend(id, chunk, [File{fid, dir, stem, ext, kind, lines}])) : {Nat.add(countId(id, t), Nat.add(_, countIdGroups(id, acc))) == Nat.add(bump(Nat.is_eq(id, fid), countId(id, t)), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}
  packFalseN(countId(id, t), countId(id, chunk), countIdGroups(id, acc), Nat.is_eq(id, fid))

def packCountFalse(+id: Nat, t: List<&2, ChangedFile>, h: ChangedFile, +chunk: List<&2, ChangedFile>, +acc: List<&2, Group>) -> {Nat.add(countId(id, t), Nat.add(countId(id, snocFile(chunk, h)), countIdGroups(id, acc))) == Nat.add(countId(id, h <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  match h:
    case File{+fid, dir, stem, ext, kind, lines}:
      packCountFalseF(id, t, fid, dir, stem, ext, kind, lines, chunk, acc)

@unsafe def packCount(+id: Nat, rest: List<&2, ChangedFile>, chunk: List<&2, ChangedFile>, +nf: Nat, +nl: Nat, acc: List<&2, Group>) -> {countIdGroups(id, pack(rest, chunk, nf, nl, acc)) == Nat.add(countId(id, rest), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  match rest:
    case Nil{}:
      %Equal.sym(Nat, countIdGroups(id, flushEnd(chunk, acc)), Nat.add(countId(id, chunk), countIdGroups(id, acc)), flushCount(id, chunk, acc)) : {_ == Nat.add(countId(id, chunk), countIdGroups(id, acc)) : Nat}
      {==}
    case +h <> +t:
      packCountNeed(id, t, h, chunk, nf, nl, acc, needFlush(nf, nl, fileLines(h)))

def packCountNeed(+id: Nat, +t: List<&2, ChangedFile>, +h: ChangedFile, +chunk: List<&2, ChangedFile>, nf: Nat, nl: Nat, +acc: List<&2, Group>, need: Bool) -> {countIdGroups(id, packNeed(t, h, chunk, nf, nl, acc, need)) == Nat.add(countId(id, h <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}:
  match need:
    case True{}:
      %Equal.sym(Nat, countIdGroups(id, pack(t, [h], 1n, fileLines(h), snocGroup(acc, chunk))), Nat.add(countId(id, t), Nat.add(countId(id, [h]), countIdGroups(id, snocGroup(acc, chunk)))), packCount(id, t, [h], 1n, fileLines(h), snocGroup(acc, chunk))) : {_ == Nat.add(countId(id, h <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}
      packCountTrue(id, t, h, chunk, acc)
    case False{}:
      %Equal.sym(Nat, countIdGroups(id, pack(t, snocFile(chunk, h), 1n+nf, Nat.add(nl, fileLines(h)), acc)), Nat.add(countId(id, t), Nat.add(countId(id, snocFile(chunk, h)), countIdGroups(id, acc))), packCount(id, t, snocFile(chunk, h), 1n+nf, Nat.add(nl, fileLines(h)), acc)) : {_ == Nat.add(countId(id, h <> t), Nat.add(countId(id, chunk), countIdGroups(id, acc))) : Nat}
      packCountFalse(id, t, h, chunk, acc)

def splitOneBCountG(+id: Nat, +fs: List<&2, ChangedFile>, big: Bool) -> {countIdGroups(id, splitOneB(Group{fs}, big)) == countId(id, fs) : Nat}:
  match big:
    case False{}:
      N.addZero(countId(id, fs))
    case True{}:
      %Equal.sym(Nat, countIdGroups(id, pack(fs, Nil{}, 0n, 0n, Nil{})), Nat.add(countId(id, fs), Nat.add(countId(id, Nil{}), countIdGroups(id, Nil{}))), packCount(id, fs, Nil{}, 0n, 0n, Nil{})) : {_ == countId(id, fs) : Nat}
      N.addZero(countId(id, fs))

def splitOneBCount(+id: Nat, g: Group, big: Bool) -> {countIdGroups(id, splitOneB(g, big)) == countId(id, groupFiles(g)) : Nat}:
  match g:
    case Group{+fs}:
      splitOneBCountG(id, fs, big)

def splitOneCount(+id: Nat, +g: Group) -> {countIdGroups(id, splitOne(g)) == countId(id, groupFiles(g)) : Nat}:
  splitOneBCount(id, g, oversized(g))

def splitAllCount(+id: Nat, gs: List<&2, Group>) -> {countIdGroups(id, splitAll(gs)) == countIdGroups(id, gs) : Nat}:
  match gs:
    case Nil{}:
      {==}
    case Group{+fs} <> +t:
      %Equal.sym(Nat, countIdGroups(id, List.append(&2, Group, splitOne(Group{fs}), splitAll(t))), Nat.add(countIdGroups(id, splitOne(Group{fs})), countIdGroups(id, splitAll(t))), countIdGroupsAppend(id, splitOne(Group{fs}), splitAll(t))) : {_ == Nat.add(countId(id, fs), countIdGroups(id, t)) : Nat}
      %Equal.sym(Nat, countIdGroups(id, splitOne(Group{fs})), countId(id, fs), splitOneCount(id, Group{fs})) : {Nat.add(_, countIdGroups(id, splitAll(t))) == Nat.add(countId(id, fs), countIdGroups(id, t)) : Nat}
      %Equal.sym(Nat, countIdGroups(id, splitAll(t)), countIdGroups(id, t), splitAllCount(id, t)) : {Nat.add(countId(id, fs), _) == Nat.add(countId(id, fs), countIdGroups(id, t)) : Nat}
      {==}

def countCong(+id: Nat, a: List<&2, ChangedFile>, b: List<&2, ChangedFile>, e: {a == b : List<&2, ChangedFile>}) -> {countId(id, a) == countId(id, b) : Nat}:
  Equal.cong(List<&2, ChangedFile>, Nat, countId(id), a, b, e)

def clusterPerm2(+id: Nat, +xs: List<&2, Item>) -> {countId(id, filesOf(xs)) == countIdGroups(id, splitAll(bucket(xs))) : Nat}:
  %Equal.sym(Nat, countIdGroups(id, splitAll(bucket(xs))), countIdGroups(id, bucket(xs)), splitAllCount(id, bucket(xs))) : {countId(id, filesOf(xs)) == _ : Nat}
  Equal.sym(Nat, countIdGroups(id, bucket(xs)), countId(id, filesOf(xs)), bucketCount(id, xs))

def clusterPerm1(+id: Nat, +es: List<&2, Edge>, +xs0: List<&2, Item>) -> {countId(id, filesOf(xs0)) == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}:
  %countCong(id, filesOf(mergeAll(xs0)), filesOf(xs0), mergeAllFiles(xs0)) : {_ == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}
  %countCong(id, filesOf(attachAll(mergeAll(xs0))), filesOf(mergeAll(xs0)), attachAllFiles(mergeAll(xs0))) : {_ == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}
  %countCong(id, filesOf(applyEdges(es, attachAll(mergeAll(xs0)))), filesOf(attachAll(mergeAll(xs0))), applyEdgesFiles(es, attachAll(mergeAll(xs0)))) : {_ == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}
  clusterPerm2(id, applyEdges(es, attachAll(mergeAll(xs0))))

def clusterPermGo(+id: Nat, fs: List<&2, ChangedFile>, +es: List<&2, Edge>, +xs0: List<&2, Item>, e0: {filesOf(xs0) == fs : List<&2, ChangedFile>}) -> {countId(id, fs) == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}:
  %countCong(id, filesOf(xs0), fs, e0) : {_ == countIdGroups(id, splitAll(bucket(applyEdges(es, attachAll(mergeAll(xs0)))))) : Nat}
  clusterPerm1(id, es, xs0)

def clusterPerm(+id: Nat, +fs: List<&2, ChangedFile>, +es: List<&2, Edge>) -> {countId(id, fs) == countIdGroups(id, cluster(fs, es)) : Nat}:
  clusterPermGo(id, fs, es, initItems(fs), initFiles(fs))

def fromFT(A: Type, e: {True{} == False{} : Bool}) -> A:
  N.absurd(A, N.fromFalseTrue(Equal.sym(Bool, True{}, False{}, e)))

def notOr(a: Bool, b: Bool, e: {Bool.or(a, b) == False{} : Bool}) -> {a == False{} : Bool} & {b == False{} : Bool}:
  match a b:
    case False{} False{}:
      ({==}, {==})
    case False{} True{}:
      fromFT({False{} == False{} : Bool} & {True{} == False{} : Bool}, e)
    case True{} False{}:
      fromFT({True{} == False{} : Bool} & {False{} == False{} : Bool}, e)
    case True{} True{}:
      fromFT({True{} == False{} : Bool} & {True{} == False{} : Bool}, e)

def notGtLe(a: Nat, b: Nat, e: {Nat.is_gt(a, b) == False{} : Bool}) -> {Nat.is_le(a, b) == True{} : Bool}:
  match a b:
    case 0n 0n:
      {==}
    case 0n 1n+bp:
      {==}
    case 1n+p 0n:
      Equal.sym(Bool, True{}, False{}, e)
    case 1n+p 1n+q:
      notGtLe(p, q, e)

def notBigCapped2Go(+h: ChangedFile, +x: ChangedFile, +xs: List<&2, ChangedFile>, ef: {Nat.is_gt(List.length(&2, ChangedFile, h <> x <> xs), maxFiles()) == False{} : Bool} & {Nat.is_gt(sumLines(h <> x <> xs), maxLines()) == False{} : Bool}) -> N.LE(List.length(&2, ChangedFile, h <> x <> xs), maxFiles()) & N.LE(sumLines(h <> x <> xs), maxLines()):
  (el, es) = ef
  (N.leOf(List.length(&2, ChangedFile, h <> x <> xs), maxFiles(), notGtLe(List.length(&2, ChangedFile, h <> x <> xs), maxFiles(), el)), N.leOf(sumLines(h <> x <> xs), maxLines(), notGtLe(sumLines(h <> x <> xs), maxLines(), es)))

def notBigCapped2(+h: ChangedFile, +x: ChangedFile, +xs: List<&2, ChangedFile>, e: {oversized(Group{h <> x <> xs}) == False{} : Bool}) -> N.LE(List.length(&2, ChangedFile, h <> x <> xs), maxFiles()) & N.LE(sumLines(h <> x <> xs), maxLines()):
  notBigCapped2Go(h, x, xs, notOr(Nat.is_gt(List.length(&2, ChangedFile, h <> x <> xs), maxFiles()), Nat.is_gt(sumLines(h <> x <> xs), maxLines()), e))

def notBigCappedT(h: ChangedFile, t: List<&2, ChangedFile>, e: {oversized(Group{h <> t}) == False{} : Bool}) -> CappedTail(h, t):
  match t:
    case Nil{}:
      Unit{}
    case x <> xs:
      notBigCapped2(h, x, xs, e)

def notBigCapped(+fs: List<&2, ChangedFile>, e: {oversized(Group{fs}) == False{} : Bool}) -> CappedFiles(fs):
  match fs:
    case Nil{}:
      Unit{}
    case h <> t:
      notBigCappedT(h, t, e)

def allCappedNil() -> AllCapped(Nil{}):
  Unit{}

def allCappedAppend(a: List<&2, Group>, b: List<&2, Group>, ca: AllCapped(a), cb: AllCapped(b)) -> AllCapped(List.append(&2, Group, a, b)):
  match a:
    case Nil{}:
      cb
    case Group{fs} <> t:
      (ca_fs, ca_t) = ca
      (ca_fs, allCappedAppend(t, b, ca_t, cb))

# --- cap arithmetic -------------------------------------------------------
# pack grows a chunk one file at a time. To show the grown chunk still respects
# the caps we need the two measures of a chunk to commute with that append.

def lengthSnoc(+xs: List<&2, ChangedFile>, +h: ChangedFile) -> {List.length(&2, ChangedFile, snocFile(xs, h)) == 1n+List.length(&2, ChangedFile, xs) : Nat}:
  match xs:
    case Nil{}:
      {==}
    case y <> ys:
      %lengthSnoc(ys, h) : {1n+List.length(&2, ChangedFile, snocFile(ys, h)) == 1n+_ : Nat}
      {==}

def sumLinesSnoc(+xs: List<&2, ChangedFile>, +h: ChangedFile) -> {sumLines(snocFile(xs, h)) == Nat.add(sumLines(xs), fileLines(h)) : Nat}:
  match xs:
    case Nil{}:
      N.addZero(fileLines(h))
    case y <> ys:
      %Equal.sym(Nat, Nat.add(Nat.add(fileLines(y), sumLines(ys)), fileLines(h)), Nat.add(fileLines(y), Nat.add(sumLines(ys), fileLines(h))), N.addAssoc(fileLines(y), sumLines(ys), fileLines(h))) : {Nat.add(fileLines(y), sumLines(snocFile(ys, h))) == _ : Nat}
      %sumLinesSnoc(ys, h) : {Nat.add(fileLines(y), sumLines(snocFile(ys, h))) == Nat.add(fileLines(y), _) : Nat}
      {==}

def noConfusionLen(+ys: List<&2, ChangedFile>, e: {1n+List.length(&2, ChangedFile, ys) == 0n : Nat}) -> Empty:
  %e : N.DiscNat(_)
  Unit{}

# Zero- and one-element chunks are trivially capped; anything longer must respect
# both caps. So two bounds are all CappedFiles ever asks for.
def cappedOfBounds(fs: List<&2, ChangedFile>, b1: N.LE(List.length(&2, ChangedFile, fs), maxFiles()), b2: N.LE(sumLines(fs), maxLines())) -> CappedFiles(fs):
  match fs:
    case Nil{}:
      Unit{}
    case h <> t:
      match t:
        case Nil{}:
          Unit{}
        case x <> xs:
          (b1, b2)

def snocLenBound(+y: ChangedFile, +ys: List<&2, ChangedFile>, +h: ChangedFile, +nf: Nat, +nl: Nat, el: {nf == List.length(&2, ChangedFile, y <> ys) : Nat}, ef: {Nat.is_ge(nf, maxFiles()) == False{} : Bool} & {Nat.is_gt(Nat.add(nl, fileLines(h)), maxLines()) == False{} : Bool}) -> N.LE(List.length(&2, ChangedFile, snocFile(y <> ys, h)), maxFiles()):
  (ege, egt) = ef
  %Equal.sym(Nat, List.length(&2, ChangedFile, snocFile(y <> ys, h)), 1n+List.length(&2, ChangedFile, y <> ys), lengthSnoc(y <> ys, h)) : N.LE(_, maxFiles())
  %el : N.LE(1n+_, maxFiles())
  N.leSuccOfLt(nf, maxFiles(), N.isGeFalse(Nat.cmp(nf, maxFiles()), ege))

def snocSumBound(+y: ChangedFile, +ys: List<&2, ChangedFile>, +h: ChangedFile, +nf: Nat, +nl: Nat, es: {nl == sumLines(y <> ys) : Nat}, ef: {Nat.is_ge(nf, maxFiles()) == False{} : Bool} & {Nat.is_gt(Nat.add(nl, fileLines(h)), maxLines()) == False{} : Bool}) -> N.LE(sumLines(snocFile(y <> ys, h)), maxLines()):
  (ege, egt) = ef
  %Equal.sym(Nat, sumLines(snocFile(y <> ys, h)), Nat.add(sumLines(y <> ys), fileLines(h)), sumLinesSnoc(y <> ys, h)) : N.LE(_, maxLines())
  %es : N.LE(Nat.add(_, fileLines(h)), maxLines())
  N.leOf(Nat.add(nl, fileLines(h)), maxLines(), notGtLe(Nat.add(nl, fileLines(h)), maxLines(), egt))

# One file appended to a capped chunk stays capped, because needFlush said no.
def snocCapped(+chunk: List<&2, ChangedFile>, +h: ChangedFile, +nf: Nat, +nl: Nat, cc: CappedFiles(chunk), +el: {nf == List.length(&2, ChangedFile, chunk) : Nat}, +es: {nl == sumLines(chunk) : Nat}, +e: {needFlush(nf, nl, fileLines(h)) == False{} : Bool}) -> CappedFiles(snocFile(chunk, h)):
  match chunk:
    case Nil{}:
      Unit{}
    case y <> ys:
      match nf:
        case 0n:
          N.absurd(CappedFiles(snocFile(y <> ys, h)), noConfusionLen(ys, Equal.sym(Nat, 0n, List.length(&2, ChangedFile, y <> ys), el)))
        case 1n+p:
          cappedOfBounds(snocFile(y <> ys, h), snocLenBound(y, ys, h, nf, nl, el, notOr(Nat.is_ge(nf, maxFiles()), Nat.is_gt(Nat.add(nl, fileLines(h)), maxLines()), e)), snocSumBound(y, ys, h, nf, nl, es, notOr(Nat.is_ge(nf, maxFiles()), Nat.is_gt(Nat.add(nl, fileLines(h)), maxLines()), e)))

def flushCapped(chunk: List<&2, ChangedFile>, acc: List<&2, Group>, cc: CappedFiles(chunk), ca: AllCapped(acc)) -> AllCapped(flushEnd(chunk, acc)):
  match chunk:
    case Nil{}:
      ca
    case y <> ys:
      allCappedAppend(acc, [Group{y <> ys}], ca, (cc, Unit{}))

# Mirrors pack/packNeed. pack is @unsafe (mutual recursion), so the proof of it
# has to use the same two-def shape; Bend reports cluster_perm and cluster_caps as
# relying on unsafe code either way.
def snocLenInv(+chunk: List<&2, ChangedFile>, +h: ChangedFile, +nf: Nat, +el: {nf == List.length(&2, ChangedFile, chunk) : Nat}) -> {1n+nf == List.length(&2, ChangedFile, snocFile(chunk, h)) : Nat}:
  %Equal.sym(Nat, List.length(&2, ChangedFile, snocFile(chunk, h)), 1n+List.length(&2, ChangedFile, chunk), lengthSnoc(chunk, h)) : {1n+nf == _ : Nat}
  %el : {1n+nf == 1n+_ : Nat}
  {==}

def snocSumInv(+chunk: List<&2, ChangedFile>, +h: ChangedFile, +nl: Nat, +es: {nl == sumLines(chunk) : Nat}) -> {Nat.add(nl, fileLines(h)) == sumLines(snocFile(chunk, h)) : Nat}:
  %Equal.sym(Nat, sumLines(snocFile(chunk, h)), Nat.add(sumLines(chunk), fileLines(h)), sumLinesSnoc(chunk, h)) : {Nat.add(nl, fileLines(h)) == _ : Nat}
  %es : {Nat.add(nl, fileLines(h)) == Nat.add(_, fileLines(h)) : Nat}
  {==}

@unsafe def packCapped(rest: List<&2, ChangedFile>, +chunk: List<&2, ChangedFile>, +nf: Nat, +nl: Nat, acc: List<&2, Group>, cc: CappedFiles(chunk), ca: AllCapped(acc), +el: {nf == List.length(&2, ChangedFile, chunk) : Nat}, +es: {nl == sumLines(chunk) : Nat}) -> AllCapped(pack(rest, chunk, nf, nl, acc)):
  match rest:
    case Nil{}:
      flushCapped(chunk, acc, cc, ca)
    case +h <> +t:
      packCappedNeed(t, h, chunk, nf, nl, acc, cc, ca, el, es, needFlush(nf, nl, fileLines(h)), {==})

def packCappedNeed(+t: List<&2, ChangedFile>, +h: ChangedFile, +chunk: List<&2, ChangedFile>, +nf: Nat, +nl: Nat, +acc: List<&2, Group>, cc: CappedFiles(chunk), ca: AllCapped(acc), +el: {nf == List.length(&2, ChangedFile, chunk) : Nat}, +es: {nl == sumLines(chunk) : Nat}, need: Bool, ne: {need == needFlush(nf, nl, fileLines(h)) : Bool}) -> AllCapped(packNeed(t, h, chunk, nf, nl, acc, need)):
  match need:
    case True{}:
      packCapped(t, [h], 1n, fileLines(h), snocGroup(acc, chunk), Unit{}, allCappedAppend(acc, [Group{chunk}], ca, (cc, Unit{})), {==}, Equal.sym(Nat, Nat.add(fileLines(h), 0n), fileLines(h), N.addZero(fileLines(h))))
    case False{}:
      packCapped(t, snocFile(chunk, h), 1n+nf, Nat.add(nl, fileLines(h)), acc, snocCapped(chunk, h, nf, nl, cc, el, es, Equal.sym(Bool, need, needFlush(nf, nl, fileLines(h)), ne)), ca, snocLenInv(chunk, h, nf, el), snocSumInv(chunk, h, nl, es))

def splitOneCappedG(+fs: List<&2, ChangedFile>, big: Bool, e: {big == oversized(Group{fs}) : Bool}) -> AllCapped(splitOneB(Group{fs}, big)):
  match big:
    case False{}:
      (notBigCapped(fs, Equal.sym(Bool, big, oversized(Group{fs}), e)), Unit{})
    case True{}:
      packCapped(fs, Nil{}, 0n, 0n, Nil{}, Unit{}, Unit{}, {==}, {==})

def splitOneCappedB(+g: Group, big: Bool, e: {big == oversized(g) : Bool}) -> AllCapped(splitOneB(g, big)):
  match g:
    case Group{+fs}:
      splitOneCappedG(fs, big, e)

def splitOneCapped(+g: Group) -> AllCapped(splitOne(g)):
  splitOneCappedB(g, oversized(g), {==})

def splitAllCapped(gs: List<&2, Group>) -> AllCapped(splitAll(gs)):
  match gs:
    case Nil{}:
      Unit{}
    case +h <> +t:
      allCappedAppend(splitOne(h), splitAll(t), splitOneCapped(h), splitAllCapped(t))

def clusterCaps(+fs: List<&2, ChangedFile>, +es: List<&2, Edge>) -> AllCapped(cluster(fs, es)):
  splitAllCapped(bucket(applyEdges(es, attachAll(mergeAll(initItems(fs))))))





