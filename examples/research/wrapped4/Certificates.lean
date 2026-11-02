import Wrapped4.Arithmetic

namespace Wrapped4

inductive Op where
  | left (i : Nat)
  | right (i : Nat)
deriving DecidableEq, Repr

def left4 (i : Nat) (xs : List α) : List α :=
  xs.take i ++ (xs.drop (i + 1)).take 3 ++ (xs.drop i).take 1 ++ xs.drop (i + 4)

def right4 (i : Nat) (xs : List α) : List α :=
  xs.take i ++ (xs.drop (i + 3)).take 1 ++ (xs.drop i).take 3 ++ xs.drop (i + 4)

def applyOp (op : Op) (xs : List α) : List α :=
  match op with
  | .left i => left4 i xs
  | .right i => right4 i xs

def applyOps (ops : List Op) (xs : List α) : List α :=
  ops.foldl (fun xs op => applyOp op xs) xs

def leftWrapped4 (i : Nat) (xs : List α) : List α :=
  let ys := xs.drop i ++ xs.take i
  let shifted := (ys.drop 1).take 3 ++ ys.take 1 ++ ys.drop 4
  let offset := xs.length - i % xs.length
  shifted.drop offset ++ shifted.take offset

def rightWrapped4 (i : Nat) (xs : List α) : List α :=
  let ys := xs.drop i ++ xs.take i
  let shifted := (ys.drop 3).take 1 ++ ys.take 3 ++ ys.drop 4
  let offset := xs.length - i % xs.length
  shifted.drop offset ++ shifted.take offset

def applyWrappedOp (op : Op) (xs : List α) : List α :=
  match op with
  | .left i => leftWrapped4 i xs
  | .right i => rightWrapped4 i xs

def applyWrappedOps (ops : List Op) (xs : List α) : List α :=
  ops.foldl (fun xs op => applyWrappedOp op xs) xs

def shiftOp (k : Nat) : Op → Op
  | .left i => .left (i + k)
  | .right i => .right (i + k)

theorem left4_shift_one (i : Nat) (x : α) (xs : List α) :
    left4 (i + 1) (x :: xs) = x :: left4 i xs := by
  simp [left4]

theorem right4_shift_one (i : Nat) (x : α) (xs : List α) :
    right4 (i + 1) (x :: xs) = x :: right4 i xs := by
  simp [right4]

theorem applyOps_shift_one (ops : List Op) (x : α) (xs : List α) :
    applyOps (ops.map (shiftOp 1)) (x :: xs) = x :: applyOps ops xs := by
  induction ops generalizing xs with
  | nil => rfl
  | cons op ops ih =>
    cases op with
    | left i =>
      simp [applyOps, shiftOp, applyOp, left4_shift_one] at *
      exact ih _
    | right i =>
      simp [applyOps, shiftOp, applyOp, right4_shift_one] at *
      exact ih _

def moveRightOps : Nat → List Op
  | 0 => []
  | n + 1 => .right 0 :: (moveRightOps n).map (shiftOp 1)

theorem moveRight_apply (a b c : α) (C B : List α) :
    applyOps (moveRightOps C.length) ([a, b, c] ++ C ++ B) = C ++ [a, b, c] ++ B := by
  induction C with
  | nil => simp [moveRightOps, applyOps]
  | cons x C ih =>
    simp only [moveRightOps, List.length_cons, applyOps, List.foldl_cons]
    rw [show applyOp (Op.right 0) ([a, b, c] ++ (x :: C) ++ B) = x :: ([a, b, c] ++ C ++ B) by
      simp [applyOp, right4]]
    change applyOps (List.map (shiftOp 1) (moveRightOps C.length)) (x :: ([a, b, c] ++ C ++ B)) = _
    rw [applyOps_shift_one]
    rw [ih]
    simp

def moveLeftOps : Nat → List Op
  | 0 => []
  | n + 1 => .left n :: moveLeftOps n

theorem moveLeft_apply (a b c : α) (C B : List α) :
    applyOps (moveLeftOps C.length) (C ++ [a, b, c] ++ B) = [a, b, c] ++ C ++ B := by
  induction h : C.length using Nat.strongRecOn generalizing C B with
  | ind n ih =>
    rcases List.eq_nil_or_concat C with hnil | ⟨C', x, rfl⟩
    · subst C
      simp at h
      subst n
      simp [moveLeftOps, applyOps]
    · have hn : n = C'.length + 1 := by simpa using h.symm
      subst n
      rw [List.concat_eq_append] at *
      simp only [List.length_append, List.length_singleton, ← List.append_assoc, List.cons_append, List.nil_append]
      simp only [moveLeftOps, applyOps, List.foldl_cons]
      rw [show applyOp (Op.left C'.length) (C' ++ [x] ++ [a, b, c] ++ B) = C' ++ [a, b, c, x] ++ B by
        simp [applyOp, left4]]
      change applyOps (moveLeftOps C'.length) (C' ++ [a, b, c, x] ++ B) = _
      simpa only [List.append_assoc, List.cons_append, List.nil_append] using ih C'.length (by omega) C' (x :: B) rfl

theorem shiftOp_zero (op : Op) : shiftOp 0 op = op := by
  cases op <;> simp [shiftOp]

theorem shiftOp_succ (n : Nat) (op : Op) :
    shiftOp (n + 1) op = shiftOp 1 (shiftOp n op) := by
  cases op <;> simp [shiftOp] <;> omega

theorem map_shiftOp_zero (ops : List Op) : ops.map (shiftOp 0) = ops := by
  induction ops with
  | nil => rfl
  | cons op ops ih => simp [shiftOp_zero, ih]

theorem map_shiftOp_succ (ops : List Op) (n : Nat) :
    ops.map (shiftOp (n + 1)) = (ops.map (shiftOp n)).map (shiftOp 1) := by
  induction ops with
  | nil => rfl
  | cons op ops ih =>
    simp only [List.map, List.cons.injEq]
    rw [shiftOp_succ, ih]
    constructor <;> rfl

theorem applyOps_shift_prefix (ops : List Op) (P xs : List α) :
    applyOps (ops.map (shiftOp P.length)) (P ++ xs) = P ++ applyOps ops xs := by
  induction P with
  | nil => simp [map_shiftOp_zero, applyOps]
  | cons y P ih =>
    simp only [List.length_cons]
    rw [map_shiftOp_succ]
    change applyOps (List.map (shiftOp 1) (List.map (shiftOp P.length) ops)) (y :: (P ++ xs)) = _
    rw [applyOps_shift_one]
    rw [ih]
    simp

theorem applyOps_append (ops₁ ops₂ : List Op) (xs : List α) :
    applyOps (ops₁ ++ ops₂) xs = applyOps ops₂ (applyOps ops₁ xs) := by
  simp [applyOps, List.foldl_append]

theorem range'_eq_map (s n : Nat) :
    List.range' s n = (List.range n).map (fun x => s + x) := by
  induction n with
  | zero => rfl
  | succ n ih =>
    rw [List.range_succ, List.map_append, ← ih, List.range'_concat]
    simp

theorem left4_map (i : Nat) (f : α → β) (xs : List α) :
    left4 i (xs.map f) = (left4 i xs).map f := by
  simp [left4, List.map, List.map_take, List.map_drop, List.map_append]

theorem right4_map (i : Nat) (f : α → β) (xs : List α) :
    right4 i (xs.map f) = (right4 i xs).map f := by
  simp [right4, List.map, List.map_take, List.map_drop, List.map_append]

theorem applyOps_map (ops : List Op) (f : α → β) (xs : List α) :
    applyOps ops (xs.map f) = (applyOps ops xs).map f := by
  induction ops generalizing xs with
  | nil => rfl
  | cons op ops ih =>
    cases op with
    | left i =>
      change applyOps ops (left4 i (xs.map f)) = (applyOps (Op.left i :: ops) xs).map f
      rw [left4_map]
      exact ih _
    | right i =>
      change applyOps ops (right4 i (xs.map f)) = (applyOps (Op.right i :: ops) xs).map f
      rw [right4_map]
      exact ih _

def baseCert : Nat → List Op
  | 6 => [.left 0, .right 2, .left 0, .right 2, .left 0]
  | 7 => [.left 0, .right 2, .left 0, .right 2, .right 3, .right 0, .left 1]
  | 8 => [.left 2, .left 0, .left 1, .right 0, .right 3, .left 4, .right 0, .left 2, .right 3, .right 0]
  | 9 => [.left 0, .left 5, .right 2, .left 4, .left 3, .right 2, .left 0, .right 2, .left 0, .right 5, .left 4, .left 3]
  | 10 => [.left 3, .left 0, .right 1, .left 0, .right 2, .right 4, .left 6, .right 1, .left 5, .right 4, .right 1, .left 3, .right 4, .left 0, .right 1]
  | 11 => [.left 0, .left 0, .right 1, .left 6, .right 3, .left 4, .left 7, .right 6, .left 0, .right 3, .right 0, .left 5, .right 4, .right 1, .right 4, .right 6, .left 3, .left 2, .right 5]
  | _ => []

theorem applyOps_shift_three (a b c : α) (ops : List Op) (C B : List α) :
    applyOps (ops.map (shiftOp 3)) ([a, b, c] ++ C ++ B) = [a, b, c] ++ applyOps ops (C ++ B) := by
  exact applyOps_shift_prefix ops [a, b, c] (C ++ B)

def intervalCert (m : Nat) : List Op :=
  if 6 ≤ m ∧ m ≤ 11 then baseCert m
  else if 12 ≤ m then
    (intervalCert (m - 6)).map (shiftOp 3) ++ moveRightOps (m - 6) ++
      (baseCert 6).map (shiftOp (m - 6)) ++ moveLeftOps (m - 6)
  else []
  termination_by m
  decreasing_by omega

theorem range_decomposition (m : Nat) (h : 12 ≤ m) :
    [0, 1, 2] ++ List.range' 3 (m - 6) ++ [m - 3, m - 2, m - 1] = List.range m := by
  have hlast : [m - 3, m - 2, m - 1] = List.range' (m - 3) 3 := by
    simp [List.range'] <;> omega
  have hlen : 3 + (m - 6) + 3 = m := by omega
  rw [List.range_eq_range', hlast]
  apply Eq.trans (b := List.range' 0 (3 + (m - 6) + 3))
  · calc
      List.range' 0 3 ++ List.range' 3 (m - 6) ++ List.range' (m - 3) 3 =
          List.range' 0 3 ++ List.range' 3 (m - 3) := by
        have h := List.range'_append (s := 3) (m := m - 6) (n := 3) (step := 1)
        simp only [Nat.one_mul] at h
        rw [show m - 6 + 3 = m - 3 by omega, show 3 + (m - 6) = m - 3 by omega] at h
        exact congrArg (fun xs => List.range' 0 3 ++ xs) h
      _ = List.range' 0 (3 + (m - 6) + 3) := by
        have h := List.range'_append (s := 0) (m := 3) (n := m - 3) (step := 1)
        simp only [Nat.zero_add, Nat.one_mul] at h
        rw [show 3 + (m - 3) = 3 + (m - 6) + 3 by omega] at h
        exact h
  · congr 1 <;> omega

theorem baseCert_correct (m : Nat) (h : 6 ≤ m ∧ m ≤ 11) :
    applyOps (baseCert m) (List.range m) = (List.range m).reverse := by
  have hm : m = 6 ∨ m = 7 ∨ m = 8 ∨ m = 9 ∨ m = 10 ∨ m = 11 := by omega
  rcases hm with rfl | rfl | rfl | rfl | rfl | rfl <;> decide

theorem baseCert_correct_range' (s m : Nat) (h : 6 ≤ m ∧ m ≤ 11) :
    applyOps (baseCert m) (List.range' s m) = (List.range' s m).reverse := by
  rw [range'_eq_map, applyOps_map, baseCert_correct m h, List.map_reverse]

theorem baseCert_length (m : Nat) (h : 6 ≤ m ∧ m ≤ 11) :
    (baseCert m).length = (m * (m - 1) + 5) / 6 := by
  have hm : m = 6 ∨ m = 7 ∨ m = 8 ∨ m = 9 ∨ m = 10 ∨ m = 11 := by omega
  rcases hm with rfl | rfl | rfl | rfl | rfl | rfl <;> decide

def fullFlip7Ops : List Op := [.left 0, .left 1, .left 6, .left 0, .left 5]

theorem fullFlip7Ops_correct :
    applyWrappedOps fullFlip7Ops (List.range 7) = (List.range 7).reverse := by decide

theorem fullFlip7Ops_length : fullFlip7Ops.length = 5 := by decide

def twoCopies10Start : List Nat := [0, 1, 2, 3, 4, 5, 6, 7, 8, 8]

def twoCopies10FlipOps : List Op :=
  [.left 1, .right 2, .right 8, .left 5, .left 3, .right 1, .right 9, .right 2]

def twoCopies10FarOps : List Op :=
  [.left 0, .left 0, .left 1, .right 7, .right 9, .right 2, .right 0, .right 3, .left 8]

theorem twoCopies10FlipOps_correct :
    applyWrappedOps twoCopies10FlipOps twoCopies10Start = twoCopies10Start.reverse := by decide

theorem twoCopies10FlipOps_length : twoCopies10FlipOps.length = 8 := by decide

theorem twoCopies10FarOps_correct :
    applyWrappedOps twoCopies10FarOps twoCopies10Start = [8, 7, 8, 6, 5, 4, 3, 2, 1, 0] := by decide

theorem twoCopies10FarOps_length : twoCopies10FarOps.length = 9 := by decide

end Wrapped4
