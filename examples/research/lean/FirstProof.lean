namespace FirstResearch

def Target : Prop := forall n : Nat, 0 + n = n

theorem candidate : Target := by
  intro n
  exact Nat.zero_add n

example : Target := candidate

#print axioms candidate

end FirstResearch
