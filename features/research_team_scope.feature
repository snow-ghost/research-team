Feature: Separate research obligations
  Scenario: Prove one part without starting the parent
    Given an open study with several unproved parts
    And only the selected part has a formal Lean goal
    When the operator starts a team with that part as target and a six-attempt limit
    Then all attempts refer to the selected part
    And accepting its reviewed proof completes the team
    And the parent remains open without new attempts

  Scenario: Reject an unrelated target
    Given two studies
    When a team for the first study selects an obligation of the second
    Then the start is rejected without scheduling an attempt

  Scenario: Limit accepted lemma context
    Given accepted lemmas inside and outside the target dependency graph
    When the target context is prepared
    Then it contains only reachable accepted lemmas

  Scenario: Reserve a review after formal repair
    Given a Lean candidate that failed checking
    And only one attempt remains in the team budget
    When the coordinator considers another formalization
    Then no repair attempt is scheduled
    And the team reports that repair and review need two attempts
