import Wrapped4.NearBinary
open Wrapped4
def Research222 : Prop := ∀ (n : Nat), 10 ≤ n →
  (sixUniqueFarOps (n + 3)).length = (sixUniqueFarOps n).length + 3
