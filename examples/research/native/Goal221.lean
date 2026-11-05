import Wrapped4.NearBinary
open Wrapped4
def Research221 : Prop := ∀ (n : Nat), 10 ≤ n →
  (sixUniqueFarOps (n + 2)).length = (sixUniqueFarOps n).length + 2
