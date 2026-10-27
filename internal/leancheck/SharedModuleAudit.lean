import Lean

open Lean

-- This entry point leaves the historical Audit.lean procedure unchanged.
def checkSharedCandidate (env : Environment) (goalName candidateName goalModule proofModule : Name) : CoreM Json := do
  let some goalIdx := env.getModuleIdxFor? goalName
    | throwError "Goal declaration has no imported module"
  unless env.header.moduleNames[goalIdx.toNat]? == some goalModule do
    throwError "Goal does not come from the pinned definition module"
  let some proofIdx := env.getModuleIdxFor? candidateName
    | throwError "Candidate has no imported module"
  unless env.header.moduleNames[proofIdx.toNat]? == some proofModule do
    throwError "Candidate does not come from the proof module"
  let goal ← getConstInfo goalName
  let candidate ← getConstInfo candidateName
  unless goal.levelParams.length == candidate.levelParams.length do
    throwError "Universe parameters differ from the fixed goal"
  let some value := candidate.value? (allowOpaque := true)
    | throwError "Candidate has no proof term"
  let levels := goal.levelParams.map Level.param
  let value := value.instantiateLevelParams candidate.levelParams levels
  let checkedType ← match Kernel.check env {} value with
    | .ok type => pure type
    | .error _ => throwError "Kernel rejected the proof term"
  match Kernel.isDefEq env {} checkedType (mkConst goalName levels) with
    | .ok true => pure ()
    | _ => throwError "Candidate does not prove the fixed goal"
  let axioms ← collectAxioms candidateName
  return Json.mkObj [("matches_goal", toJson true),
    ("axioms", toJson (axioms.map Name.toString))]

def main (args : List String) : IO UInt32 := do
  try
    let [goal, candidate, proofModule, goalModule] := args
      | throw (IO.userError "Expected goal, candidate, proof module and pinned definition module")
    initSearchPath (← findSysroot)
    let env ← importModules #[{ module := proofModule.toName }] {} 0 (loadExts := false)
    let report ← (checkSharedCandidate env goal.toName candidate.toName goalModule.toName proofModule.toName).toIO'
      {fileName := "SharedModuleAudit", fileMap := FileMap.ofString ""} {env}
    IO.println report.compress
    return 0
  catch error =>
    IO.eprintln s!"Shared module audit failed: {error}"
    return 1
