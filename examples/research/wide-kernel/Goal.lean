import Mathlib.LinearAlgebra.Matrix.ToLin
import Mathlib.LinearAlgebra.FiniteDimensional.Lemmas

namespace ResearchWideKernel

universe u

def Statement : Prop :=
  forall (F : Type u) [Field F] (m n : Nat),
    m < n ->
    forall (A : Matrix (Fin m) (Fin n) F),
      Exists (fun v : Fin n -> F => Not (v = 0) ∧ A.mulVec v = 0)

#check Statement

end ResearchWideKernel
