# Contributing to the native archive fork

This repository develops an independent native media archive based on Stash.
Read the [fork policy](../FORK.md), [transition plan](native-archive-transition-plan.md)
and [implementation progress](native-archive-progress.md) before proposing
changes. Implementation continues on `v3-rewrite`; merging into `develop`
requires verification and the owner's success review.

The final v2.5-compatible release is frozen at `v2.5-compatible-final` with
pinned images. Compatibility with its database, GraphQL API, UI or unversioned
plugins is not a requirement for new features. Existing supported installations
are migration inputs. Preserve their data and provide explicit upgrade and
recovery paths; do not add permanent compatibility sidecars or dual writers.
StashDB/stash-box remains a supported external protocol independently of those
upstream implementation details.

## AI Usage Policy

Please see our [AI Usage Policy](AI_POLICY.md) for guidelines on the use of AI in contributions to this project.

## Issues

Bug reports and feature requests must use descriptive and concise titles and follow the provided templates. Please use the search function to make sure that you are not submitting duplicates, and that a similar report or request has not already been resolved or rejected.

All issues must be written by humans. Fully AI-generated issues will be closed without comment.

## Pull Requests

All pull requests must use descriptive and concise titles and follow the provided templates. In addition, they must follow the the following guidelines:

- You must link to an open issue that pull request addresses (see [GitHub documentation](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue) on how to do that).
- Pull requests must be focused on a single issue or feature. Large, multi-purpose pull requests will be rejected.
- Large features must be discussed with maintainers before submitting a pull request to ensure it fits with the overall design vision of the project. Failure to do so may result in the pull request being rejected.
- Pull requests must include code tests that sufficiently cover the changes made.
- You must detail the manual testing done and describe the steps taken to sufficiently verify the changes.
- You must be able to explain any line of code and design decision during the review process.
- You may not have more than 3 open pull requests at a time, unless you have received explicit permission from a maintainer. If you have more than 3 open pull requests, maintainers may ask you to close some of them before they will review any of them.

By submitting a pull request, you agree that you have read and understood and that you are in compliance with the guidelines outlined here, including the [AI Usage Policy](AI_POLICY.md).

You also agree to license your contribution under the [AGPL](../LICENSE) license, and that all of your previous contributions to the project are also licensed under the AGPL.

## Bounties

Pull requests for bounties must be discussed with maintainers before submitting a pull request to ensure it fits with the overall design vision of the project. Failure to do so may result in the pull request being rejected.

## Goals and Design Vision

The archive organizes and presents videos, images and galleries together with
their source evidence and reviewed metadata. Performer UUIDs, accounts,
collections, posts, shared revisions, media associations and durable work are
core concepts. Purchased or manually scanned files need no invented source
post. Source publishers, including aggregator accounts, remain distinct from
depicted performers.

Gallery-dl and other external producers own website access and downloads.
They submit idempotent events through the native API, retaining unsent events
locally. Stash owns archive consistency, scheduling state, association review
and after-success notifications. Website credentials stay with the producer;
Stash API tokens authorize its scoped archive operations. Plugins can extend
the application through the v3 contract, but archive integrity must not depend
on a plugin or an external catalog database.

Preserve portable exports, offline use, intentional share/media URLs and the
StashDB/stash-box protocol. Backups and restore must cover source evidence,
identity history, retained metadata decisions, original record images and
pending producer state. Changes to these contracts require migration and
recovery verification as well as ordinary feature tests.

Use the existing React v3 application and shared Base UI components for desktop
and mobile. Keep account linking and other individual operations bounded to
their relevant records; long operations need durable progress and safe retries.
New work should support the archive's defined scope without introducing
unrelated infrastructure. Preserve upstream attribution and the AGPL license.

## Validation and deployment

Follow the [development guide](../ui/v3/docs/development.md) for dependencies,
generation and tests. Build real embedded assets before full backend checks;
`make validate-fork` is the pre-push gate. Never edit generated Go/TypeScript
bindings by hand. Test migration outcomes, restarts and replay with real SQLite
fixtures when a change affects durable state.

Publish incremental commits without rewriting published history. Use native
preview images with recorded source digests, and follow the
[deployment runbook](v3-deployment.md) and transition cutover gates. A passing
build is not evidence that production has migrated. Keep production changes,
rehearsal results, remaining work and recovery limits explicit.
