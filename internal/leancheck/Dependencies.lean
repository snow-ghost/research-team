import Lean

open Lean

def dependencies (env : Environment) (candidate : Name) : CoreM Json := do
  let info ← getConstInfo candidate
  let some value := info.value? (allowOpaque := true)
    | throwError "Candidate has no proof term"
  let direct := value.getUsedConstants
  let mut pending := direct.toList
  let mut seen : NameSet := {}
  let mut refs : Array Json := #[]
  while !pending.isEmpty do
    let name := pending.head!
    pending := pending.tail!
    if seen.contains name then continue
    seen := seen.insert name
    if seen.size > 8192 then throwError "Dependency traversal limit"
    let moduleName := match env.getModuleIdxFor? name with
      | some idx => (env.header.moduleNames[idx.toNat]!).toString
      | none => ""
    let library := moduleName.startsWith "ResearchLemma_"
    if library then
      refs := refs.push (Json.mkObj [
        ("declaration", toJson name.toString), ("module", toJson moduleName),
        ("direct", toJson (direct.contains name))])
    -- Do not recursively expand Mathlib. Follow local helpers and research modules.
    if moduleName == "Candidate" || moduleName == "Goal" || library then
      let c ← getConstInfo name
      if let some body := c.value? (allowOpaque := true) then
        pending := body.getUsedConstants.toList ++ pending
  return Json.mkObj [("direct", toJson (direct.map Name.toString)),
    ("library_references", toJson refs)]

def main (args : List String) : IO UInt32 := do
  try
    let [candidate] := args | throw (IO.userError "Expected candidate declaration")
    initSearchPath (← findSysroot)
    let env ← importModules #[{ module := `Candidate }] {} 0 (loadExts := false)
    let report ← (dependencies env candidate.toName).toIO'
      {fileName := "Dependencies", fileMap := FileMap.ofString ""} {env}
    IO.println report.compress
    return 0
  catch error =>
    IO.eprintln s!"Dependency audit failed: {error}"
    return 1
