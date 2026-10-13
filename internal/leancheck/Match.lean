import Lean

open Lean Meta

def describeMatch (moduleName goalName candidateName : Name) : MetaM Json := do
  let env <- getEnv
  let candidate <- getConstInfo candidateName
  let some idx := env.getModuleIdxFor? candidateName
    | throwError "Candidate is not imported"
  unless env.header.moduleNames[idx.toNat]? == some moduleName do
    throwError "Candidate module differs from the pinned library"
  let _ <- getConstInfo goalName
  let goal <- mkConstWithFreshMVarLevels goalName
  forallTelescopeReducing goal fun locals conclusion => do
    if locals.size > 128 then throwError "Target binder limit"
    let lemma <- mkConstWithFreshMVarLevels candidate.name
    let (args, infos, resultType) <- forallMetaTelescopeReducing (<- inferType lemma)
    if args.size > 128 then throwError "Candidate binder limit"
    unless (<- isDefEq resultType conclusion) do
      return Json.mkObj [("status", toJson "no_match"), ("bindings", toJson (#[] : Array Json)),
        ("missing", toJson (#[] : Array Json))]
    -- Later premises can determine the types and functions of earlier arguments.
    for _ in [:args.size + 1] do
      let mut progress := false
      for i in [:args.size] do
        let arg := args[i]!
        if !(<- arg.mvarId!.isAssigned) then
          let decl <- arg.mvarId!.getDecl
          let type <- instantiateMVars decl.type
          if infos[i]! == .instImplicit then
            if let some value <- synthInstance? type then arg.mvarId!.assign value
          if !(<- arg.mvarId!.isAssigned) && (<- isProp type) then
            for x in locals do
              let saved <- saveState
              if (<- isDefEq (<- inferType x) type) then
                arg.mvarId!.assign x
                break
              else saved.restore
          if (<- arg.mvarId!.isAssigned) then progress := true
      if !progress then break
    let mut bindings : Array Json := #[]
    let mut missing : Array Json := #[]
    let mut unresolved := false
    for i in [:args.size] do
      let arg := args[i]!
      let decl <- arg.mvarId!.getDecl
      let type <- instantiateMVars decl.type
      let value <- instantiateMVars arg
      let type <- instantiateMVars type
      let kind <- if infos[i]! == .instImplicit then pure "instance"
        else if (<- isProp type) then pure "premise" else pure "parameter"
      if value.hasMVar then
        unresolved := true
        let closed <- mkForallFVars locals type
        let closedType <- if closed.hasMVar then pure "" else pure (toString (<- ppExpr closed))
        missing := missing.push (Json.mkObj [
          ("name", toJson decl.userName.toString), ("kind", toJson kind),
          ("type", toJson (toString (<- ppExpr type))),
          ("closed_type", toJson closedType)])
      else
        bindings := bindings.push (Json.mkObj [("name", toJson decl.userName.toString),
          ("term", toJson (toString (<- ppExpr value)))])
    let mut term := ""
    if !unresolved then
      let proof <- instantiateMVars (mkAppN lemma args)
      let proof <- mkLambdaFVars locals proof
      unless proof.hasMVar do term := toString (<- ppExpr proof)
    return Json.mkObj [("status", toJson (if term == "" then "obligations" else "proposed")),
      ("bindings", toJson bindings), ("missing", toJson missing), ("term", toJson term)]

def main (args : List String) : IO UInt32 := do
  try
    let [moduleName, goalName, candidateName] := args
      | throw (IO.userError "Expected module, target and candidate names")
    initSearchPath (<- findSysroot)
    let opts := (({} : Options).setBool `pp.fullNames true).set `maxHeartbeats (200000 : Nat)
    let env <- importModules #[{ module := moduleName.toName }, { module := `Goal }] opts 0 (loadExts := false)
    let result <- (describeMatch moduleName.toName goalName.toName candidateName.toName).run'.toIO'
      {fileName := "Match", fileMap := FileMap.ofString "", options := opts} {env}
    IO.println result.compress
    return 0
  catch error =>
    IO.eprintln s!"Typed matching failed: {error}"
    return 1
