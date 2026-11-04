import Wrapped4.Certificates

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

theorem twoUniqueFlipOps_correct_small {n : Nat} (h6 : 6 ≤ n) (h10 : n < 10) (a b x : α) :
    applyWrappedOps (twoUniqueFlipOps n) ([a, b] ++ List.replicate (n - 2) x) =
      List.replicate (n - 2) x ++ [b, a] := by
  have h : n = 6 ∨ n = 7 ∨ n = 8 ∨ n = 9 := by omega
  rcases h with rfl | rfl | rfl | rfl <;>
    simp [twoUniqueFlipOps, applyWrappedOps, applyWrappedOp, leftWrapped4, rightWrapped4,
      replicate_append, drop_replicate_prefix, take_replicate_prefix, List.replicate_succ,
      List.append_assoc]

theorem twoUniqueFlipOps_correct_six {n : Nat} (hn : 6 ≤ n) (a b x : α) :
    applyWrappedOps (twoUniqueFlipOps n) ([a, b] ++ List.replicate (n - 2) x) =
      List.replicate (n - 2) x ++ [b, a] := by
  if h : n < 10 then
    exact twoUniqueFlipOps_correct_small hn h a b x
  else
    exact twoUniqueFlipOps_correct (by omega) a b x

theorem twoUniqueFlipOps_counterexample_five :
    ¬ applyWrappedOps (twoUniqueFlipOps 5) ([0, 1] ++ List.replicate 3 2) =
      List.replicate 3 2 ++ [1, 0] := by decide

theorem rightWrapped4_append (P B : List α) (a b c d : α) :
    rightWrapped4 P.length (P ++ [a, b, c, d] ++ B) = P ++ [d, a, b, c] ++ B := by
  have hmod : P.length % (P.length + (B.length + 4)) = P.length :=
    Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, List.take_append, List.drop_append, List.append_assoc, hmod,
    show P.length + (B.length + 1 + 1 + 1 + 1) - P.length = B.length + 4 by omega]

theorem applyWrappedOps_moveRight_shift (P C B : List α) (a b c : α) :
    applyWrappedOps ((moveRightOps C.length).map (shiftOp P.length))
      (P ++ [a, b, c] ++ C ++ B) = P ++ C ++ [a, b, c] ++ B := by
  induction C generalizing P B with
  | nil => simp [moveRightOps, applyWrappedOps]
  | cons x C ih =>
    simp only [moveRightOps, List.length_cons, List.map_cons, List.map_map,
      applyWrappedOps, List.foldl_cons, shiftOp]
    rw [show applyWrappedOp (Op.right (0 + P.length)) (P ++ [a,b,c] ++ (x :: C) ++ B) =
      (P ++ [x]) ++ [a,b,c] ++ C ++ B by
      simp only [Nat.zero_add, applyWrappedOp]
      rw [show P ++ [a,b,c] ++ (x :: C) ++ B = P ++ [a,b,c,x] ++ C ++ B by
        simp only [List.append_assoc, List.cons_append, List.nil_append]]
      simpa only [List.append_assoc, List.cons_append, List.nil_append] using
        rightWrapped4_append P (C ++ B) a b c x]
    have hop (op : Op) : (shiftOp P.length ∘ shiftOp 1) op = shiftOp (P ++ [x]).length op := by
      cases op <;> simp [shiftOp] <;> omega
    have hlist : List.map (shiftOp P.length ∘ shiftOp 1) (moveRightOps C.length) =
        List.map (shiftOp (P ++ [x]).length) (moveRightOps C.length) := by
      induction moveRightOps C.length with
      | nil => rfl
      | cons op ops ih => simp only [List.map_cons, hop, ih]
    change applyWrappedOps
      (List.map (shiftOp P.length ∘ shiftOp 1) (moveRightOps C.length))
      ((P ++ [x]) ++ [a,b,c] ++ C ++ B) = _
    rw [hlist]
    simpa only [List.append_assoc, List.cons_append, List.nil_append] using ih (P ++ [x]) B

theorem applyWrappedOps_append (ops₁ ops₂ : List Op) (xs : List α) :
    applyWrappedOps (ops₁ ++ ops₂) xs = applyWrappedOps ops₂ (applyWrappedOps ops₁ xs) := by
  simp [applyWrappedOps, List.foldl_append]

theorem moveRightOps_length (m : Nat) : (moveRightOps m).length = m := by
  induction m with
  | zero => rfl
  | succ m ih => simp [moveRightOps, ih]

def sixUniqueFarOps (n : Nat) : List Op :=
  [Op.left 0, Op.left 1] ++ (moveRightOps (n - 10)).map (shiftOp 3) ++
    [Op.right (n - 3), Op.right (n - 2), Op.right (n - 1),
      Op.left (n - 4), Op.right (n - 6), Op.right (n - 7)]

theorem sixUniqueFarOps_length {n : Nat} (hn : 10 ≤ n) : (sixUniqueFarOps n).length = n - 2 := by
  simp [sixUniqueFarOps, moveRightOps_length]
  omega

@[simp] theorem take_append_add (C D : List α) (k : Nat) :
    List.take (C.length + k) (C ++ D) = C ++ List.take k D := by
  rw [List.take_append, List.take_of_length_le (by omega)]
  simp

theorem sixUniqueFarStep01 (C : List α) (a b c d e f x : α) :
    applyWrappedOps [Op.left 0, Op.left 1]
      ([a, b, c, d, e, f] ++ C ++ List.replicate 4 x) =
      [b, d, a, e, c, f] ++ C ++ List.replicate 4 x := by
  simp [applyWrappedOps, applyWrappedOp, leftWrapped4, List.replicate_succ]

theorem sixUniqueFarStepRange (C : List α) (a b c d e f x : α) :
    applyWrappedOps ((moveRightOps C.length).map (shiftOp 3))
      ([b, d, a, e, c, f] ++ C ++ List.replicate 4 x) =
      [b, d, a] ++ C ++ [e, c, f] ++ List.replicate 4 x := by
  simpa [List.append_assoc] using
    (applyWrappedOps_moveRight_shift [b, d, a] C (List.replicate 4 x) e c f)

theorem sixUniqueFarStep1 (C : List α) (a b c d e f x : α) :
    rightWrapped4 (C.length + 7)
      ([b, d, a] ++ C ++ [e, c, f] ++ List.replicate 4 x) =
      [x, d, a] ++ C ++ [e, c, f] ++ [x, b, x, x] := by
  have hm : (C.length + 7) % (C.length + 10) = C.length + 7 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.replicate_succ, List.append_assoc]

theorem sixUniqueFarStep2 (C : List α) (a b c d e f x : α) :
    rightWrapped4 (C.length + 8)
      ([x, d, a] ++ C ++ [e, c, f] ++ [x, b, x, x]) =
      [x, x, a] ++ C ++ [e, c, f] ++ [x, b, d, x] := by
  have hm : (C.length + 8) % (C.length + 10) = C.length + 8 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sixUniqueFarStep3 (C : List α) (a b c d e f x : α) :
    rightWrapped4 (C.length + 9)
      ([x, x, a] ++ C ++ [e, c, f] ++ [x, b, d, x]) =
      [x, x, x] ++ C ++ [e, c, f] ++ [x, b, d, a] := by
  have hm : (C.length + 9) % (C.length + 10) = C.length + 9 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sixUniqueFarStep4 (C : List α) (a b c d e f x : α) :
    leftWrapped4 (C.length + 6)
      ([x, x, x] ++ C ++ [e, c, f] ++ [x, b, d, a]) =
      [x, x, x] ++ C ++ [e, c, f] ++ [b, d, a, x] := by
  have hm : (C.length + 6) % (C.length + 10) = C.length + 6 := Nat.mod_eq_of_lt (by omega)
  simp [leftWrapped4, hm, List.append_assoc]

theorem sixUniqueFarStep5 (C : List α) (a b c d e f x : α) :
    rightWrapped4 (C.length + 4)
      ([x, x, x] ++ C ++ [e, c, f] ++ [b, d, a, x]) =
      [x, x, x] ++ C ++ [e, d, c, f] ++ [b, a, x] := by
  have hm : (C.length + 4) % (C.length + 10) = C.length + 4 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sixUniqueFarStep6 (C : List α) (a b c d e f x : α) :
    rightWrapped4 (C.length + 3)
      ([x, x, x] ++ C ++ [e, d, c, f] ++ [b, a, x]) =
      [x, x, x] ++ C ++ [f, e, d, c] ++ [b, a, x] := by
  have hm : (C.length + 3) % (C.length + 10) = C.length + 3 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sixUniqueFarOps_correct_list (C : List α) (a b c d e f x : α) :
    applyWrappedOps (sixUniqueFarOps (C.length + 10))
      ([a, b, c, d, e, f] ++ C ++ List.replicate 4 x) =
      [x, x, x] ++ C ++ [f, e, d, c, b, a] ++ [x] := by
  have h10 : C.length + 10 - 10 = C.length := by omega
  have h3 : C.length + 10 - 3 = C.length + 7 := by omega
  have h2 : C.length + 10 - 2 = C.length + 8 := by omega
  have h1 : C.length + 10 - 1 = C.length + 9 := by omega
  have h4 : C.length + 10 - 4 = C.length + 6 := by omega
  have h6 : C.length + 10 - 6 = C.length + 4 := by omega
  have h7 : C.length + 10 - 7 = C.length + 3 := by omega
  simp only [sixUniqueFarOps, h10, h3, h2, h1, h4, h6, h7]
  rw [applyWrappedOps_append, applyWrappedOps_append, sixUniqueFarStep01, sixUniqueFarStepRange]
  simp only [applyWrappedOps, applyWrappedOp, List.foldl_cons, List.foldl_nil]
  rw [sixUniqueFarStep1, sixUniqueFarStep2, sixUniqueFarStep3, sixUniqueFarStep4,
    sixUniqueFarStep5, sixUniqueFarStep6]
  simp only [List.append_assoc, List.cons_append, List.nil_append]

theorem sixUniqueFarOps_correct_ten (m : Nat) (a b c d e f x : α) :
    applyWrappedOps (sixUniqueFarOps (m + 10))
      ([a, b, c, d, e, f] ++ List.replicate (m + 4) x) =
      List.replicate (m + 3) x ++ [f, e, d, c, b, a] ++ [x] := by
  have hc : [x, x, x] ++ List.replicate m x = List.replicate (m + 3) x := by
    calc
      [x, x, x] ++ List.replicate m x = List.replicate (3 + m) x :=
        (replicate_append 3 m x).symm
      _ = List.replicate (m + 3) x := by rw [Nat.add_comm]
  rw [replicate_append m 4 x]
  simpa only [List.length_replicate, List.append_assoc, List.cons_append, List.nil_append, hc] using
    (sixUniqueFarOps_correct_list (List.replicate m x) a b c d e f x)

theorem sixUniqueFarOps_correct {n : Nat} (hn : 10 ≤ n) (a b c d e f x : α) :
    applyWrappedOps (sixUniqueFarOps n)
      ([a, b, c, d, e, f] ++ List.replicate (n - 6) x) =
      List.replicate (n - 7) x ++ [f, e, d, c, b, a] ++ [x] := by
  obtain ⟨m, rfl⟩ : ∃ m, n = m + 10 := ⟨n - 10, by omega⟩
  have h6 : m + 10 - 6 = m + 4 := by omega
  have h7 : m + 10 - 7 = m + 3 := by omega
  rw [h6, h7]
  exact sixUniqueFarOps_correct_ten m a b c d e f x

theorem sixUniqueFarOps_correct_extra (G : List α) (m : Nat) (a b c d e f x : α) :
    applyWrappedOps (sixUniqueFarOps (G.length + m + 10))
      ([a, b, c, d, e, f] ++ G ++ List.replicate (m + 4) x) =
      [x, x, x] ++ G ++ List.replicate m x ++ [f, e, d, c, b, a] ++ [x] := by
  rw [replicate_append m 4 x]
  simpa [List.append_assoc] using
    (sixUniqueFarOps_correct_list (G ++ List.replicate m x) a b c d e f x)

end Wrapped4
