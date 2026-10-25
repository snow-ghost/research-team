Feature: Bounded research completion and reusable skill procedures

  Scenario: Operator accepts the main goal while another agent is still working
    Given an accepted main goal and a running attempt on an optional child claim
    When the acceptance is committed
    Then the child computation is marked for cancellation
    And its unknown remote outcome remains unknown
    And completed evidence remains unchanged
    And separately authorized operator questions remain available

  Scenario: An interrupted comparison continues after a mount alias changes
    Given immutable completed comparison records and the same Lean dependencies
    When the operator explicitly resolves dependency aliases and resumes
    Then only unstarted slots run
    And original configuration and completed records are preserved
    And effective environment hashes are recorded

  Scenario: Library search reaches its native-check budget
    Given accepted reusable modules and an open pinned target
    When an operator starts a bounded catalog search
    Then failed and successful checks are recorded
    And the same command returns recorded checks without repeating them
    And no hypothesis is accepted automatically

  Scenario: A skill rejects an unstructured agent result
    Given a pinned skill requiring a typed review report
    When an agent returns prose instead of the report
    Then the structural check fails
    And the original answer, precheck status and token usage remain available
    And no mathematical verification is claimed by the structural check
