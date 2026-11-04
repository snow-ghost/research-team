import Wrapped4.NearBinary

namespace Wrapped4

def sevenUniqueFlipOps (n : Nat) : List Op :=
  [Op.left 0, Op.left 0, Op.left 4, Op.left 4, Op.left 7, Op.right 4, Op.left 7, Op.left 5] ++
    (moveRightOps (n - 15)).map (shiftOp 8) ++
    [Op.right (n - 3), Op.left 0, Op.right (n - 3), Op.right (n - 1),
      Op.left (n - 4), Op.right (n - 1)]

theorem sevenUniqueFlipOps_length {n : Nat} (hn : 15 ≤ n) : (sevenUniqueFlipOps n).length = n - 1 := by
  simp [sevenUniqueFlipOps, moveRightOps_length]
  omega

theorem sevenUniqueFlipPrefix (m : Nat) (a b c d e f g x : α) :
    applyWrappedOps [Op.left 0, Op.left 0, Op.left 4, Op.left 4, Op.left 7,
      Op.right 4, Op.left 7, Op.left 5]
      ([a, b, c, d, e, f, g] ++ List.replicate (m + 8) x) =
      [c, d, a, b] ++ List.replicate 4 x ++ [g, f, e] ++ List.replicate (m + 4) x := by
  have h4 : 4 % (m + 15) = 4 := Nat.mod_eq_of_lt (by omega)
  have h5 : 5 % (m + 15) = 5 := Nat.mod_eq_of_lt (by omega)
  have h7 : 7 % (m + 15) = 7 := Nat.mod_eq_of_lt (by omega)
  simp [applyWrappedOps, applyWrappedOp, leftWrapped4, rightWrapped4, h4, h5, h7,
    List.replicate_succ, List.append_assoc]

theorem sevenUniqueFlipStep1 (C : List α) (a b c d e f g x : α) :
    rightWrapped4 (C.length + 8)
      ([c, d, a, b] ++ C ++ [g, f, e] ++ List.replicate 4 x) =
      [x, d, a, b] ++ C ++ [g, f, e] ++ [x, c, x, x] := by
  have hm : (C.length + 8) % (C.length + 11) = C.length + 8 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.replicate_succ, List.append_assoc]

theorem sevenUniqueFlipStep2 (C : List α) (a b c d e f g x : α) :
    leftWrapped4 0
      ([x, d, a, b] ++ C ++ [g, f, e] ++ [x, c, x, x]) =
      [d, a, b, x] ++ C ++ [g, f, e] ++ [x, c, x, x] := by
  simp [leftWrapped4, List.append_assoc]

theorem sevenUniqueFlipStep3 (C : List α) (a b c d e f g x : α) :
    rightWrapped4 (C.length + 8)
      ([d, a, b, x] ++ C ++ [g, f, e] ++ [x, c, x, x]) =
      [x, a, b, x] ++ C ++ [g, f, e] ++ [x, d, c, x] := by
  have hm : (C.length + 8) % (C.length + 11) = C.length + 8 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sevenUniqueFlipStep4 (C : List α) (a b c d e f g x : α) :
    rightWrapped4 (C.length + 10)
      ([x, a, b, x] ++ C ++ [g, f, e] ++ [x, d, c, x]) =
      [x, x, a, x] ++ C ++ [g, f, e] ++ [x, d, c, b] := by
  have hm : (C.length + 10) % (C.length + 11) = C.length + 10 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sevenUniqueFlipStep5 (C : List α) (a b c d e f g x : α) :
    leftWrapped4 (C.length + 7)
      ([x, x, a, x] ++ C ++ [g, f, e] ++ [x, d, c, b]) =
      [x, x, a, x] ++ C ++ [g, f, e] ++ [d, c, b, x] := by
  have hm : (C.length + 7) % (C.length + 11) = C.length + 7 := Nat.mod_eq_of_lt (by omega)
  simp [leftWrapped4, hm, List.append_assoc]

theorem sevenUniqueFlipStep6 (C : List α) (a b c d e f g x : α) :
    rightWrapped4 (C.length + 10)
      ([x, x, a, x] ++ C ++ [g, f, e] ++ [d, c, b, x]) =
      [x, x, x, x] ++ C ++ [g, f, e, d, c, b, a] := by
  have hm : (C.length + 10) % (C.length + 11) = C.length + 10 := Nat.mod_eq_of_lt (by omega)
  simp [rightWrapped4, hm, List.append_assoc]

theorem sevenUniqueFlipRange (m : Nat) (a b c d e f g x : α) :
    applyWrappedOps ((moveRightOps m).map (shiftOp 8))
      ([c, d, a, b] ++ List.replicate 4 x ++ [g, f, e] ++ List.replicate m x ++ List.replicate 4 x) =
      [c, d, a, b] ++ List.replicate 4 x ++ List.replicate m x ++ [g, f, e] ++ List.replicate 4 x := by
  simpa using (applyWrappedOps_moveRight_shift ([c, d, a, b] ++ List.replicate 4 x)
    (List.replicate m x) (List.replicate 4 x) g f e)

theorem sevenUniqueFlipTail (C : List α) (a b c d e f g x : α) :
    applyWrappedOps [Op.right (C.length + 8), Op.left 0, Op.right (C.length + 8),
      Op.right (C.length + 10), Op.left (C.length + 7), Op.right (C.length + 10)]
      ([c, d, a, b] ++ C ++ [g, f, e] ++ List.replicate 4 x) =
      [x, x, x, x] ++ C ++ [g, f, e, d, c, b, a] := by
  simp only [applyWrappedOps, applyWrappedOp, List.foldl_cons, List.foldl_nil]
  rw [sevenUniqueFlipStep1, sevenUniqueFlipStep2, sevenUniqueFlipStep3,
    sevenUniqueFlipStep4, sevenUniqueFlipStep5, sevenUniqueFlipStep6]

theorem sevenUniqueFlipOps_correct_fifteen (m : Nat) (a b c d e f g x : α) :
    applyWrappedOps (sevenUniqueFlipOps (m + 15))
      ([a, b, c, d, e, f, g] ++ List.replicate (m + 8) x) =
      List.replicate (m + 8) x ++ [g, f, e, d, c, b, a] := by
  have h15 : m + 15 - 15 = m := by omega
  have h3 : m + 15 - 3 = m + 12 := by omega
  have h1 : m + 15 - 1 = m + 14 := by omega
  have h4 : m + 15 - 4 = m + 11 := by omega
  simp only [sevenUniqueFlipOps, h15, h3, h1, h4]
  rw [applyWrappedOps_append, applyWrappedOps_append, sevenUniqueFlipPrefix]
  rw [replicate_append m 4 x]
  simp only [← List.append_assoc]
  rw [sevenUniqueFlipRange]
  have h12 : m + 12 = (List.replicate 4 x ++ List.replicate m x).length + 8 := by simp
  have h14 : m + 14 = (List.replicate 4 x ++ List.replicate m x).length + 10 := by simp
  have h11 : m + 11 = (List.replicate 4 x ++ List.replicate m x).length + 7 := by simp
  rw [h12, h14, h11]
  have hc : [x, x, x, x] ++ (List.replicate 4 x ++ List.replicate m x) =
      List.replicate (m + 8) x := by
    calc
      [x, x, x, x] ++ (List.replicate 4 x ++ List.replicate m x) =
          List.replicate 8 x ++ List.replicate m x := by
        simp
      _ = List.replicate (8 + m) x := (replicate_append 8 m x).symm
      _ = List.replicate (m + 8) x := by rw [Nat.add_comm]
  have h := sevenUniqueFlipTail (List.replicate 4 x ++ List.replicate m x) a b c d e f g x
  rw [hc] at h
  exact h

theorem sevenUniqueFlipOps_correct {n : Nat} (hn : 15 ≤ n) (a b c d e f g x : α) :
    applyWrappedOps (sevenUniqueFlipOps n)
      ([a, b, c, d, e, f, g] ++ List.replicate (n - 7) x) =
      List.replicate (n - 7) x ++ [g, f, e, d, c, b, a] := by
  obtain ⟨m, rfl⟩ : ∃ m, n = m + 15 := ⟨n - 15, by omega⟩
  have h7 : m + 15 - 7 = m + 8 := by omega
  rw [h7]
  exact sevenUniqueFlipOps_correct_fifteen m a b c d e f g x

def sevenUniqueFlip14Ops : List Op :=
  [Op.left 0, Op.left 0, Op.left 4, Op.left 5, Op.left 6, Op.left 4, Op.right 5,
    Op.right 11, Op.left 0, Op.right 11, Op.right 13, Op.left 10, Op.right 13]

theorem sevenUniqueFlip14Ops_correct :
    applyWrappedOps sevenUniqueFlip14Ops ([0, 1, 2, 3, 4, 5, 6] ++ List.replicate 7 7) =
      List.replicate 7 7 ++ [6, 5, 4, 3, 2, 1, 0] := by decide

theorem sevenUniqueFlip14Ops_length : sevenUniqueFlip14Ops.length = 13 := by
  rfl

def eightUniqueFlip15Ops : List Op :=
  [Op.left 0, Op.left 0, Op.left 4, Op.right 5, Op.left 4, Op.right 6, Op.right 7,
    Op.left 5, Op.right 12, Op.left 0, Op.right 12, Op.right 14, Op.left 11, Op.right 14]

theorem eightUniqueFlip15Ops_correct :
    applyWrappedOps eightUniqueFlip15Ops ([0, 1, 2, 3, 4, 5, 6, 7] ++ List.replicate 7 8) =
      List.replicate 7 8 ++ [7, 6, 5, 4, 3, 2, 1, 0] := by decide

theorem eightUniqueFlip15Ops_length : eightUniqueFlip15Ops.length = 14 := by
  rfl

def eightUniqueFlip16Ops : List Op :=
  [Op.left 15, Op.left 14, Op.right 0, Op.left 14, Op.right 11, Op.left 14,
    Op.left 13, Op.left 11, Op.right 5, Op.right 6, Op.left 8, Op.left 4,
    Op.right 6, Op.left 8, Op.left 7, Op.right 5]

theorem eightUniqueFlip16Ops_correct :
    applyWrappedOps eightUniqueFlip16Ops ([0, 1, 2, 3, 4, 5, 6, 7] ++ List.replicate 8 8) =
      List.replicate 8 8 ++ [7, 6, 5, 4, 3, 2, 1, 0] := by
  decide

theorem eightUniqueFlip16Ops_length : eightUniqueFlip16Ops.length = 16 := by
  rfl

end Wrapped4
