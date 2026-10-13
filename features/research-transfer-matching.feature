Feature: Audited continuation and transfer of proof research
  Source authorship, local verification and operator acceptance remain separate.

  Scenario: A completed result is moved to another workspace
    Given a result with an exact proof binding and separate reviewer
    When the operator imports its bounded package into a paused workspace
    Then original responses, failed attempts and journal digests are preserved
    And scoped inputs have separate original and projected digests
    And local Lean checks the unchanged source again
    And historical model requests are not charged to the local budget again
    And the original acceptance does not become a local acceptance

  Scenario: A standalone task has competing skills
    Given skills with server-checkable applicability and an exclusive group
    When an assignment is authorized
    Then unavailable skills are rejected before a budget reservation
    And the applicable group winner has highest priority and deterministic identity
    And the chosen method and all selections are pinned in the attempt

  Scenario: Lean infers a lemma application
    Given a ready accepted module and an exact open target
    When Lean unifies the conclusion and infers the arguments
    Then a reconstructed complete term needs a separate kernel check
    And unresolved logical premises become separately confirmed obligations
    And no term or obligation is automatically accepted

  Scenario: A remote process disappears
    Given a separately running local worker with a scoped key
    When its process stops and its lease expires
    Then the old outcome stays unknown and charged
    And no automatic replacement starts
    When the operator authorizes a fresh assignment to another bound worker
    Then the new attempt links to the old one
    And old or cancelled results cannot overwrite the new result

  Scenario: A comparison uses demand-driven specialists
    Given six pinned public cases and two repetitions
    When version 3 compares one researcher, a fixed team and demand-driven specialists
    Then each model arm has the same wall-clock and request cap
    And the reviewer uses proof-review@2 for every arm
    And the Lean baseline has independent fresh tactic states
    And the previous version 2 report remains unchanged
