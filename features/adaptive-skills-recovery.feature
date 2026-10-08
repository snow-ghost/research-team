Feature: Adaptive proof research with pinned skills and recoverable queues
  Scenario: A counterexample is formalized before any positive proof search
    Given a pinned open Lean goal and an authorized adaptive team
    When the counterexample role supplies a negative candidate
    And its first formalization fails native verification
    Then a bounded formalizer retry receives the diagnostic and negative goal
    And a distinct reviewer is bound to the successful formalizer attempt
    And refutation still requires the operator decision

  Scenario: A malformed answer is retried without editing the original
    Given an authorized team with enough attempts for repair and review
    When the formalizer returns no completed Lean block
    Then the original response remains unchanged in the journal
    And another attempt receives the output-format diagnostic

  Scenario: A skill cannot expand profile permissions
    Given an immutable skill revision requiring check_lean
    When a profile without that tool references the revision
    Then profile creation is rejected
    And no permission is added by the skill instructions

  Scenario: An unchanged idle state avoids full traversal
    Given a paused server without due deadlines
    When coordinator ticks continue without a new database revision
    Then lightweight revision probes continue
    And full state reads and coordinator cycles do not increase

  Scenario: Maintenance preserves work that has never started
    Given a paused server with queued attempts and no running process
    When the operator pins the maintenance queue and restarts the server
    Then those attempts remain queued and not_started
    And ambiguous attempts are never automatically replayed

  Scenario: A portable backup excludes operational configuration
    Given a verified backup with state artifacts and private launch settings
    When a new portable kit is exported
    Then launch settings and access credentials are not copied
    And copied artifacts and the database dump retain verified checksums
    And altered files are rejected on validation
