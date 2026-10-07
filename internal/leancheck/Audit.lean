import Lean

open Lean

def checkCandidate (env : Environment) (goalName candidateName goalModuleName : Name) : CoreM Json := do
  let some goalModuleIdx := env.getModuleIdxFor? goalName
    | throwError "Goal declaration has no imported module"
  unless env.header.moduleNames[goalModuleIdx.toNat]? == some goalModuleName do
    throwError "Goal declaration does not come from the trusted goal module"
  let goal ← getConstInfo goalName
  let candidate ← getConstInfo candidateName
  unless goal.levelParams.length == candidate.levelParams.length do
    throwError "Universe parameters differ from the fixed goal"
  let some value := candidate.value? (allowOpaque := true) | throwError "Candidate has no proof term"
  let levels := goal.levelParams.map Level.param
  let value := value.instantiateLevelParams candidate.levelParams levels
  let checkedType ← match Kernel.check env {} value with
    | .ok type => pure type
    | .error _ => throwError "Kernel rejected the proof term"
  let target := mkConst goalName levels
  match Kernel.isDefEq env {} checkedType target with
    | .ok true => pure ()
    | _ => throwError "Candidate does not prove the fixed goal"
  let axioms ← collectAxioms candidateName
  return Json.mkObj [("matches_goal", toJson true),
    ("axioms", toJson (axioms.map Name.toString))]

def main (args : List String) : IO UInt32 := do
  try
    let (goal, candidate, moduleName, goalModuleName) ← match args with
      | [goal, candidate] => pure (goal, candidate, "Candidate", "Goal")
      | [goal, candidate, moduleName] => pure (goal, candidate, moduleName, moduleName)
      | _ => throw (IO.userError "Expected declaration names and an optional module name")
    initSearchPath (← findSysroot)
    -- Import data without executing untrusted initializers or environment extensions.
    let env ← importModules #[{ module := moduleName.toName }] {} 0 (loadExts := false)
    let report ← (checkCandidate env goal.toName candidate.toName goalModuleName.toName).toIO'
      {fileName := "Audit", fileMap := FileMap.ofString ""} {env}
    IO.println report.compress
    return 0
  catch error =>
    IO.eprintln s!"Candidate audit failed: {error}"
    return 1
