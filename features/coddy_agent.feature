Feature: Execute research attempts through Coddy Agent ACP
  The direct executor returns an unverified candidate.
  Coddy Bot remains a separate GitHub workflow.

  Scenario: Complete a bounded research attempt
    Given a reviewed Coddy Agent 1.1.64 binary and an isolated workspace
    And a profile with versioned skills and an explicit model
    When ACP confirms the session, ask mode and permission settings
    And the prompt ends with end_turn and nonempty assistant text
    Then the result is a candidate with task, snapshot and profile hashes
    And usage remains unknown until complete accounting is implemented

  Scenario: Receive the command catalog before session creation completes
    Given a session/new request in progress
    When a command catalog arrives with a provisional session identifier
    Then the session/new response must confirm that identifier
    And a different identifier rejects the attempt

  Scenario Outline: Reject incomplete and untrusted output
    When the agent reports <condition>
    Then no candidate is published
    Examples:
      | condition                  |
      | max_tokens                 |
      | max_turns                  |
      | max_turn_requests          |
      | refusal                    |
      | cancelled                  |
      | missing terminal response  |
      | foreign session identifier |
      | changed model              |
      | changed mode               |

  Scenario: Decline tool permission
    When the agent requests tool permission
    Then the client returns cancelled regardless of option order
    And the decision is recorded
    And client filesystem and terminal capabilities remain disabled

  Scenario: Enforce process limits
    When the output exceeds the byte limit or the deadline expires
    Then the process group is stopped and its pipes are closed
    And the partial answer is discarded
    And local cancellation does not establish remote cancellation

  Scenario: Check a real agent against a local model substitute
    Given CODDY_AGENT_TEST_BINARY points to the reviewed binary
    When the real ACP server receives a deterministic local model response
    Then a complete response becomes a candidate
    And a response ending with length becomes limit_reached
