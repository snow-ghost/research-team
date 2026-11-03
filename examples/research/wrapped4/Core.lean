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

end Wrapped4
