Feature: Delegate a repository development workflow to Coddy Bot
  GitHub publication and scientific acceptance are separate decisions.

  Scenario: Prepare without publishing
    Given a pinned research task and source commit
    When a delegation is prepared
    Then no network request is sent
    And a repeated preparation returns the same delegation

  Scenario: Complete a development and review cycle
    When the operator explicitly publishes a task assigned to Coddy
    And approves the observed bot plan
    And Coddy opens a pull request on the correlated branch
    And the operator submits line-level change requests for a pinned commit
    And approves the review plan
    Then changed-file metadata is collected for the observed commit
    And the research result remains a candidate

  Scenario: The response to issue creation is lost
    Given the durable write intent precedes the GitHub request
    When the connector restarts
    Then it reconciles the existing issue
    And does not repeat the ambiguous write

  Scenario: A plan changed after inspection
    When approval refers to the previous snapshot
    Then publication is rejected

  Scenario: The pull request changes during collection
    Then no candidate artifact is attached

  Scenario: Pause locally
    When the delegation is held
    Then the connector blocks new writes
    And does not claim that Coddy has stopped remotely

  Scenario: A foreign pull request resembles the result
    Then its repository, branch and author fail correlation

  Scenario: A partial list reaches the pagination limit
    Then the connector reports a limit
    And does not infer that no remote operation exists

  Scenario: A clarification contains an affirmative phrase
    Then ordinary reply publication is rejected
    And explicit plan approval is required

  Scenario: The configured repository is public
    Given public publication is not explicitly enabled
    Then no issue is created even when the command permits a write
