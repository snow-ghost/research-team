import Lean

open Lean Meta

def describe (goalName candidateName moduleName : Name) : MetaM Json := do
  let env <- getEnv
  let some idx := env.getModuleIdxFor? goalName
    | throwError "Statement is not imported"
  unless env.header.moduleNames[idx.toNat]? == some moduleName do
    throwError "Statement module differs from the pinned library"
  let candidate <- getConstInfo candidateName
  let fullType := (toString (<- ppExpr candidate.type))
  forallTelescopeReducing candidate.type fun xs conclusion => do
    let mut binders : Array Json := #[]
    for x in xs do
      let binder <- x.fvarId!.getDecl
      let kind <- if binder.binderInfo == .instImplicit then pure "instance"
        else if (<- isProp binder.type) then pure "premise" else pure "parameter"
      binders := binders.push (Json.mkObj [
        ("name", toJson binder.userName.toString),
        ("type", toJson (toString (<- ppExpr binder.type))),
        ("kind", toJson kind),
        ("implicit", toJson (binder.binderInfo != .default))])
    return Json.mkObj [("binders", toJson binders),
      ("conclusion", toJson (toString (<- ppExpr conclusion))),
      ("full_type", toJson fullType),
      ("universes", toJson (candidate.levelParams.map Name.toString))]

def main (args : List String) : IO UInt32 := do
  try
    let [moduleName, goalName, candidateName] := args
      | throw (IO.userError "Expected module and declaration names")
    initSearchPath (<- findSysroot)
    let env <- importModules #[{ module := moduleName.toName }] {} 0 (loadExts := false)
    let result <- (describe goalName.toName candidateName.toName moduleName.toName).run'.toIO'
      {fileName := "Inspect", fileMap := FileMap.ofString ""} {env}
    IO.println result.compress
    return 0
  catch error =>
    IO.eprintln s!"Signature extraction failed: {error}"
    return 1
