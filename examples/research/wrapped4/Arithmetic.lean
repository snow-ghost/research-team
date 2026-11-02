import Lean

namespace Wrapped4

def lowerBound (n : Int) : Int := (n - 1) ^ 2 / 4

def minimalLength (D ε : Int) : Int := ε + 2 * ((D - 3 * ε + 5) / 6)

def admissible (D ε L : Int) : Prop := 0 ≤ L ∧ D ≤ 3 * L ∧ L % 2 = ε

def IsLeast (p : Int → Prop) (x : Int) : Prop := p x ∧ ∀ L, p L → x ≤ L

theorem minimalLength_spec {D ε : Int} (hε : ε = 0 ∨ ε = 1) (hD : 3 * ε ≤ D) :
    IsLeast (fun L => admissible D ε L) (minimalLength D ε) := by
  constructor
  · rcases hε with rfl | rfl <;> simp [admissible, minimalLength] at * <;> omega
  · intro L hL
    rcases hε with rfl | rfl <;> simp [admissible, minimalLength] at * <;> omega

theorem circular_parity_rounding {D U ε : Int}
    (hε : ε = 0 ∨ ε = 1) (hparity : U % 2 = ε)
    (hlower : D ≤ 3 * U) (hupper : 3 * U < D + 6) :
    U = minimalLength D ε := by
  rcases hε with rfl | rfl <;> simp [minimalLength] at * <;> omega

theorem circular_length_is_least {D U ε : Int}
    (hε : ε = 0 ∨ ε = 1) (hparity : U % 2 = ε) (hD : 3 * ε ≤ D)
    (hlower : D ≤ 3 * U) (hupper : 3 * U < D + 6) :
    IsLeast (fun L => admissible D ε L) U := by
  rw [circular_parity_rounding hε hparity hlower hupper]
  exact minimalLength_spec hε hD

end Wrapped4
