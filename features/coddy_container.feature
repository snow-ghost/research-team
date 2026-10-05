Feature: Bounded Coddy execution
  Scenario: The installed reviewed agent uses a separate home
    Given a Coddy 1.2.1 profile and a local model stub
    When the server requests a candidate
    Then the request contains the configured skills and response limit
    And shell, editing and subagent tools are absent
    And a truncated answer is rejected

  Scenario: Container cancellation releases the worker
    Given a locally pinned image and read-only input materials
    When execution is cancelled
    Then the attached process is stopped
    And its container is removed with a separate cleanup deadline
    And no host home or Docker socket is mounted

  Scenario: Personal credentials remain private
    Given explicit operator approval to import the selected Coddy login
    When the provider endpoint differs from the login hub
    Then import fails without exposing credential values
    And the original Coddy installation is unchanged
