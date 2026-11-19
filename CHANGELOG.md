# Changelog

All notable changes of `multi-tour-worker-financial-timeouts` (Financial timeouts worker) are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [3.1.0] - 2026-11-18

Minor release after 3.0.0. The entry below is a pull request merged into `qa` and re-applied to `release/3.1.0` with `git cherry-pick -x`; it names no user story, so it is grouped under the story of this component, and the entry is the title of the pull request without its type. The frozen head of `qa` is `7a3cda119373cda3833f7630ac5478a8fe1fc1ea`.

### Changed

#### HU-WRK-002 - Apply the financial timeouts

- Fix the readme on proof pages and add the pr template ([PR 18](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/18))

## [3.0.0] - 2026-11-14

Cut 3 release. Every entry below is a pull request merged into `qa` and re-applied to `release/3.0.0` with `git cherry-pick -x`; the heading of each group is the user story named in the pull request, and the entry is the title of the pull request without its type. The frozen head of `qa` is `e5348fc90efb99b6f11093cb03df64c242ccce99`.

### Changed

#### HU-WRK-002 - Apply the financial timeouts

- The module and configuration of job 2 (part 1) ([PR 5](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/5))
- The job and its adapters of job 2 (part 2) ([PR 6](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/6))
- The active tenants of job 2 (part 3) ([PR 7](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/7))
- The checks on pull requests to main ([PR 10](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/10))
- The HU-WRK-002 tests ([PR 11](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/11))
- Run the test workflow only on pull requests to develop and qa ([PR 15](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/15))

### Fixed

#### HU-WRK-002 - Apply the financial timeouts

- Walk the pages of the overdue proofs and forget the reviewed ones ([PR 13](https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/pull/13))

[Unreleased]: https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/compare/v3.1.0...HEAD
[3.1.0]: https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/releases/tag/v3.1.0
[3.0.0]: https://github.com/code-corhuila/multi-tour-worker-financial-timeouts/releases/tag/v3.0.0
