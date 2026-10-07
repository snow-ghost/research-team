Feature: Operator-controlled research management
  Scenario: An agent proposes a plan without authorizing its execution
    Given an open pinned formal goal and a permitted planning profile
    When the planner proposes lemmas, coverage and method priorities
    Then the proposal preserves the goal revision and source result hash
    And no method branch starts before operator confirmation

  Scenario: Dependent branches wait for accepted grounds
    Given an approved plan with a lemma and a coverage obligation
    When coverage has higher priority than the lemma
    Then the coordinator starts the independent lemma first
    And the original proposition is not accepted by decomposition alone

  Scenario: Profile specialization does not change running attempts
    Given a trusted provider template and an immutable profile version
    When the operator creates another version with revised skills
    Then both versions remain selectable after restart
    And attempts retain their original configuration

  Scenario: Structural search proposes a proof obligation
    Given an accepted compiled lemma with indexed types and conditions
    When a matching conclusion is requested with missing conditions
    Then the result lists the missing conditions
    And applicability requires a pinned Lean obligation

  Scenario: Recovery preserves historical evidence
    Given a completed private backup with matching database and file hashes
    When the operator restores into a new database and state directory
    Then the original instance is not overwritten
    And the restored instance starts paused
