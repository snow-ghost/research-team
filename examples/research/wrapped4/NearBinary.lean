import Wrapped4.Core

namespace Wrapped4

theorem replicate_append (m k : Nat) (x : α) :
    List.replicate (m + k) x = List.replicate m x ++ List.replicate k x := by
  induction m with
  | zero => simp
  | succ m ih =>
    rw [show m + 1 + k = (m + k) + 1 by omega, List.replicate_succ, ih]
    simp only [List.replicate_succ, List.cons_append]

theorem drop_replicate_prefix (m i : Nat) (x : α) (C : List α) :
    List.drop (m + i) (List.replicate m x ++ C) = List.drop i C := by
  rw [show m + i = (List.replicate m x).length + i by simp]
  exact List.drop_length_add_append i

theorem take_replicate_prefix (m i : Nat) (x : α) (C : List α) :
    List.take (m + i) (List.replicate m x ++ C) = List.replicate m x ++ List.take i C := by
  rw [show m + i = (List.replicate m x).length + i by simp]
  exact List.take_length_add_append i

def twoUniqueFlipOps (n : Nat) : List Op :=
  [Op.left (n - 3), Op.left (n - 4), Op.right (n - 2)]

theorem twoUniqueFlipOps_length (n : Nat) : (twoUniqueFlipOps n).length = 3 := by
  rfl

theorem twoUniqueFlipStep1 {n : Nat} (hn : 10 ≤ n) (a b x : α) :
    leftWrapped4 (n - 3) ([a, b] ++ List.replicate (n - 2) x) =
      [x, b] ++ List.replicate (n - 3) x ++ [a] := by
  obtain ⟨m, rfl⟩ : ∃ m, n = m + 10 := ⟨n - 10, by omega⟩
  have hm : (m + 7) % (m + 10) = m + 7 := Nat.mod_eq_of_lt (by omega)
  have h2 : m + 10 - 2 = m + 8 := by omega
  have h3 : m + 10 - 3 = m + 7 := by omega
  rw [h2, h3, replicate_append, replicate_append]
  simp [leftWrapped4, hm, drop_replicate_prefix, take_replicate_prefix, List.replicate_succ]

theorem twoUniqueFlipStep2 {n : Nat} (hn : 10 ≤ n) (a b x : α) :
    leftWrapped4 (n - 4) ([x, b] ++ List.replicate (n - 3) x ++ [a]) =
      [x, b] ++ List.replicate (n - 4) x ++ [a, x] := by
  obtain ⟨m, rfl⟩ : ∃ m, n = m + 10 := ⟨n - 10, by omega⟩
  have hm : (m + 6) % (m + 10) = m + 6 := Nat.mod_eq_of_lt (by omega)
  have h3 : m + 10 - 3 = m + 7 := by omega
  have h4 : m + 10 - 4 = m + 6 := by omega
  rw [h3, h4, replicate_append, replicate_append]
  simp [leftWrapped4, hm, drop_replicate_prefix, take_replicate_prefix, List.replicate_succ]

theorem twoUniqueFlipStep3 {n : Nat} (hn : 10 ≤ n) (a b x : α) :
    rightWrapped4 (n - 2) ([x, b] ++ List.replicate (n - 4) x ++ [a, x]) =
      List.replicate (n - 2) x ++ [b, a] := by
  obtain ⟨m, rfl⟩ : ∃ m, n = m + 10 := ⟨n - 10, by omega⟩
  have hm : (m + 8) % (m + 10) = m + 8 := Nat.mod_eq_of_lt (by omega)
  have h4 : m + 10 - 4 = m + 6 := by omega
  have h2 : m + 10 - 2 = m + 8 := by omega
  rw [h4, h2, replicate_append, replicate_append]
  simp [rightWrapped4, hm, drop_replicate_prefix, take_replicate_prefix, List.replicate_succ,
    List.append_assoc]
  have hc : [x, x] ++ List.replicate m x = List.replicate m x ++ [x, x] := by
    calc
      [x, x] ++ List.replicate m x = List.replicate (2 + m) x := (replicate_append 2 m x).symm
      _ = List.replicate (m + 2) x := by rw [Nat.add_comm]
      _ = List.replicate m x ++ [x, x] := replicate_append m 2 x
  change [x, x] ++ (List.replicate m x ++ [x, x, x, x, x, x, b, a]) =
    List.replicate m x ++ [x, x, x, x, x, x, x, x, b, a]
  rw [← List.append_assoc, hc]
  simp only [List.append_assoc, List.cons_append, List.nil_append]

theorem twoUniqueFlipOps_correct {n : Nat} (hn : 10 ≤ n) (a b x : α) :
    applyWrappedOps (twoUniqueFlipOps n) ([a, b] ++ List.replicate (n - 2) x) =
      List.replicate (n - 2) x ++ [b, a] := by
  simp only [twoUniqueFlipOps, applyWrappedOps, applyWrappedOp, List.foldl_cons, List.foldl_nil]
  rw [twoUniqueFlipStep1 hn, twoUniqueFlipStep2 hn, twoUniqueFlipStep3 hn]

end Wrapped4
