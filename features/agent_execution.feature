Feature: Execute a bounded research attempt through a selected agent
  Research acceptance is separate from executor completion.

  Scenario: A model reads evidence using an approved tool
    Given a pinned task snapshot and versioned skills
    And a model connection using base_url, token reference and model
    When the model requests read_file and then returns a final response
    Then the tool result is attached to the correct call
    And the executor returns a candidate with the original lease epoch
    And the candidate is not an accepted proof

  Scenario Outline: An external agent returns structured output
    Given a version-pinned <provider> adapter and operator-managed isolation
    When the process returns its successful terminal event
    Then its final text becomes a candidate
    And unrelated environment secrets are not inherited
    And the task text is passed through standard input
    Examples:
      | provider |
      | codex    |
      | claude   |
      | opencode |

  Scenario: The installed agent has an unexpected version
    When the version probe differs from the profile
    Then no research command is started
    And credentials are not passed to the probe

  Scenario: The model requests an unavailable tool
    Then the attempt fails without executing that tool

  Scenario: A tool request escapes the workspace
    Then parent traversal and escaping symbolic links are denied

  Scenario: A model response is incomplete or a budget is exhausted
    Then no candidate is published
    And unknown usage is not recorded as zero

  Scenario: An external process exceeds its output limit
    Then execution is cancelled and output memory remains bounded

  Scenario: A running attempt is cancelled
    Then the local HTTP request or owned process group is stopped
    And remote completion is not inferred from local cancellation

  Scenario: The provider redirects or echoes a credential in an error
    Then the redirect is not followed
    And the provider error body is not included in diagnostics

  Scenario: Coddy Bot is configured as a direct process executor
    Given Coddy Bot manages its own repository task queue
    When its profile is passed to the executor factory
    Then a workflow connector is required
    And credentials are not resolved
    And no executor is constructed

  Scenario: A Coddy workflow reports done
    Then its status is not parsed as a completed research candidate
