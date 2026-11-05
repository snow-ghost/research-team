import Wrapped4.NearBinary
open Wrapped4
def Research202 : Prop := ∀ (n : Nat), 10 ≤ n → ∀ (a b c d e f x : Nat),
  applyWrappedOps (sixUniqueFarOps n) ([a, b, c, d, e, f] ++ List.replicate (n - 6) x) =
    List.replicate (n - 7) x ++ [f, e, d, c, b, a] ++ [x]
