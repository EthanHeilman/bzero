# Contributing guidelines

We at BastionZero appreciate your interest and welcome contributions to this project from the open-source community! Before making a contribution, please be aware of the following:
  - By contributing to this repository, you agree to license your work under the Apache 2.0 license
  - Community contributions should be linked to a [GitHub issue](https://github.com/bastionzero/bzero/issues). If no issue exists, please create one!
  - All contributions are subject to code review from the core BastionZero team, and will be run against our automated testing suites. We reserve the right to reject changes that weaken our security model or are otherwise inconsistent with the project's goals

## Contributor workflow

All proposed patches to this project are reviewed via pull requests (PRs) in GitHub. Once you've decided to work on an issue, the process of updating the codebase looks like this:
  1. Create a new branch off of `develop`. It may be prefixed with `feat/`, `fix/`, or `refactor/` (see below), but a prefix is not required
  2. Write and test your patch locally, add appropriate tests (see below), and increment the number in [VERSION](https://github.com/bastionzero/bzero/blob/develop/VERSION)
  3. Create a pull request and fill out the template describing how to test your changes
  4. Request a review from the core team (@bastionzero/backend-team) and address their feedback
  5. If your patch passes our automated tests and is approved by the reviewers, it can be merged!

## Best practices

Please adhere to the following standards when making a contribution. These will make your PR easier to review and ultiamtely more likely to improve the codebase:
  - Your patch might add a **feature**, fix a **bug**, or **refactor** existing code; a single patch should only do one of these things at a time
  - The smaller the patch, the better!
  - All new functionality should include automated tests that adhere to our [best practices](https://github.com/bastionzero/bzero/wiki/Unit-testing-best-practices)
  - Go files must be formatted with `gofmt`
  - Variable names and other code patterns should be consistent with existing project norms. If you have any questions about these, feel free to create an issue or ask a reviewer for clarification. We will be happy to add the answer to your question to our [wiki](https://github.com/bastionzero/bzero/wiki)