Feature: Demand-driven proof research
  A checked source, an independent review and operator acceptance have distinct authority.

  Scenario: One researcher supplies an exact Lean candidate
    Given a pinned formal goal and four immutable role profiles
    When the first researcher supplies a complete Lean source
    Then Lean checks that source without a separate formalizer
    And boundary checks and a distinct reviewer still run
    And the review binds the actual source author
    And the goal remains unaccepted until an operator decision

  Scenario: A specialist is needed
    Given the first response lacks Lean source or its native check fails
    When the attempt budget permits formalization and review
    Then the formalizer receives the original result and the diagnostic
    And each new response has separate provenance

  Scenario: Skill applicability is checked
    Given versioned skills with role, method and evidence requirements
    When the coordinator assigns a task
    Then only satisfied requirements select a skill
    And the decision records selected and rejected versions
    And skill instructions do not grant tools or acceptance authority

  Scenario: Separate Lean tactic trials
    Given simplification leaves an unresolved polynomial goal
    When a bounded portfolio runs
    Then later tactics have independent proof states and reports
    And an exact native proof is not an operator acceptance

  Scenario: A lemma parameter is proposed
    Given a ready accepted module and a native binder description
    When a parameter binding is proposed for an exact target
    Then Lean checks the generated application
    And a wrong parameter type or target is rejected

  Scenario: A pilot is created while research is paused
    Given the workspace is paused
    When the operator creates the known injective-iteration recipe
    Then the statements, dependencies and coverage obligation are visible
    And no model request starts
    And the proof progress names unaccepted foundations
