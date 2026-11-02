import Lean

namespace Wrapped4

def repeatedRep (x : Nat) : Nat := x / 3 + x % 3

def repeatedCost (n d : Nat) : Nat := min (repeatedRep d) (repeatedRep (n - d))

def repeatedDiameterFormula (n : Nat) : Nat := n / 6 + 1 + if n % 6 = 4 then 1 else 0

theorem repeatedRep_gt {x D : Nat} (h : D < repeatedRep x) : 3 * D ≤ x + 1 := by
  apply Classical.byContradiction
  intro hx
  unfold repeatedRep at h
  have hx' : x + 1 < 3 * D := by omega
  have hdiv : x / 3 ≤ D - 1 := by
    have hle := Nat.div_le_div_right (c := 3) (show x ≤ 3 * (D - 1) by omega)
    have hcancel : 3 * (D - 1) / 3 = D - 1 := by simp
    omega
  have hmod := Nat.mod_lt x (by decide : 0 < 3)
  have hxeq := Nat.div_add_mod x 3
  omega

theorem repeated_cost_upper {n D : Nat} (h : n + 2 < 6 * D) (d : Nat) : repeatedCost n d ≤ D := by
  apply Classical.byContradiction
  intro hcost
  unfold repeatedCost at hcost
  have hd : D < repeatedRep d := by
    have hle := Nat.min_le_left (repeatedRep d) (repeatedRep (n - d))
    omega
  have hn : D < repeatedRep (n - d) := by
    have hle := Nat.min_le_right (repeatedRep d) (repeatedRep (n - d))
    omega
  have hd' := repeatedRep_gt hd
  have hn' := repeatedRep_gt hn
  omega

theorem rep_three_mul (q : Nat) : repeatedRep (3 * q) = q := by
  simp [repeatedRep]

theorem rep_succ (q : Nat) : repeatedRep (3 * q + 1) = q + 1 := by
  simp [repeatedRep]
  omega

theorem rep_succ_two (q : Nat) : repeatedRep (3 * q + 2) = q + 2 := by
  simp [repeatedRep]
  omega

theorem rep_pred {q : Nat} (hq : 1 ≤ q) : repeatedRep (3 * q - 1) = q + 1 := by
  have hq' := Nat.div_add_mod q 1
  simp [repeatedRep]
  omega

theorem rep_succ_three (q : Nat) : repeatedRep (3 * q + 3) = q + 1 := by
  simpa [Nat.mul_add] using rep_three_mul (q + 1)

theorem repeated_cost_upper_five {n d : Nat} (hdiv : 6 * (n / 6) + n % 6 = n) (h5 : n % 6 = 5) :
    repeatedCost n d ≤ repeatedDiameterFormula n := by
  have hmod := Nat.mod_lt n (by decide : 0 < 6)
  apply Classical.byContradiction
  intro hcost
  unfold repeatedCost at hcost
  have hd : repeatedDiameterFormula n < repeatedRep d := by
    have hle := Nat.min_le_left (repeatedRep d) (repeatedRep (n - d))
    omega
  have hn : repeatedDiameterFormula n < repeatedRep (n - d) := by
    have hle := Nat.min_le_right (repeatedRep d) (repeatedRep (n - d))
    omega
  have hd' := repeatedRep_gt hd
  have hn' := repeatedRep_gt hn
  have hd_cases : d = 3 * (n / 6) + 2 ∨ d = 3 * (n / 6) + 3 := by
    simp [repeatedDiameterFormula, h5] at hd' hn'
    omega
  rcases hd_cases with hd2 | hd3
  · have heq : n - d = 3 * (n / 6) + 3 := by omega
    rw [heq, rep_succ_three] at hn
    simp [repeatedDiameterFormula, h5] at hn
  · have heq : n - d = 3 * (n / 6) + 2 := by omega
    rw [hd3, rep_succ_three] at hd
    simp [repeatedDiameterFormula, h5] at hd

theorem repeated_cost_upper_all {n : Nat} (hn : 8 ≤ n) (d : Nat) :
    repeatedCost n d ≤ repeatedDiameterFormula n := by
  have hdiv := Nat.div_add_mod n 6
  have hmod := Nat.mod_lt n (by decide : 0 < 6)
  by_cases h5 : n % 6 = 5
  · exact repeated_cost_upper_five hdiv h5
  · have hupper : n + 2 < 6 * repeatedDiameterFormula n := by
      by_cases h4 : n % 6 = 4
      · simp [repeatedDiameterFormula, h4]
        omega
      · simp [repeatedDiameterFormula, h4]
        omega
    exact repeated_cost_upper hupper d
theorem repeated_cost_witness {n : Nat} (hn : 8 ≤ n) :
    ∃ d < n, repeatedCost n d = repeatedDiameterFormula n := by
  have hdiv := Nat.div_add_mod n 6
  have hmod := Nat.mod_lt n (by decide : 0 < 6)
  have hr : n % 6 = 0 ∨ n % 6 = 1 ∨ n % 6 = 2 ∨ n % 6 = 3 ∨ n % 6 = 4 ∨ n % 6 = 5 := by omega
  rcases hr with h0 | h1 | h2 | h3 | h4 | h5
  · have hq : 1 ≤ n / 6 := by omega
    refine ⟨3 * (n / 6) - 1, by omega, ?_⟩
    have heq : n - (3 * (n / 6) - 1) = 3 * (n / 6) + 1 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h0, heq, rep_pred hq, rep_succ]
  · have hq : 1 ≤ n / 6 := by omega
    refine ⟨3 * (n / 6) - 1, by omega, ?_⟩
    have heq : n - (3 * (n / 6) - 1) = 3 * (n / 6) + 2 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h1, heq, rep_pred hq, rep_succ_two]
  · refine ⟨3 * (n / 6) + 1, by omega, ?_⟩
    have heq : n - (3 * (n / 6) + 1) = 3 * (n / 6) + 1 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h2, heq, rep_succ]
  · refine ⟨3 * (n / 6) + 1, by omega, ?_⟩
    have heq : n - (3 * (n / 6) + 1) = 3 * (n / 6) + 2 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h3, heq, rep_succ, rep_succ_two]
  · refine ⟨3 * (n / 6) + 2, by omega, ?_⟩
    have heq : n - (3 * (n / 6) + 2) = 3 * (n / 6) + 2 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h4, heq, rep_succ_two]
  · refine ⟨3 * (n / 6) + 2, by omega, ?_⟩
    have heq : n - (3 * (n / 6) + 2) = 3 * (n / 6) + 3 := by omega
    simp [repeatedCost, repeatedDiameterFormula, h5, heq, rep_succ_two, rep_succ_three]

theorem repeated_cost_base :
    repeatedDiameterFormula 5 = 1 ∧ repeatedDiameterFormula 6 = 2 ∧ repeatedDiameterFormula 7 = 2 := by decide

theorem repeated_cost_formula {n : Nat} (hn : 8 ≤ n) :
    (∀ d < n, repeatedCost n d ≤ repeatedDiameterFormula n) ∧
      ∃ d < n, repeatedCost n d = repeatedDiameterFormula n :=
  ⟨fun d _ => repeated_cost_upper_all hn d, repeated_cost_witness hn⟩

end Wrapped4
