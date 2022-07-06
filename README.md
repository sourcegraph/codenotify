# Codenotify

Codenotify is a tool that analyzes the files changed in one or more git commits and emits the list of people who have subscribed to be notified when those files change. File subscribers are defined in [CODENOTIFY](#codenotify) files.

Codenotify can be run on the command line, or as a GitHub Action.

### CLI

When run on the command line, Codenotify prints the results.

```
$ codenotify -baseRef a1b2c3 -headRef HEAD
a1b2c3...HEAD
@go -> file.go, dir/file.go
@js -> file.js, dir/file.js
```

### GitHub Action

When run as a GitHub Action, Codenotify will post a comment that mentions people who have subscribed to files changed in that pull request.

> Notifying subscribers in [CODENOTIFY](https://github.com/sourcegraph/codenotify) files for diff a1b2c3...d4e5f6.
>
> | Notify | File(s)                |
> | ------ | ---------------------- |
> | @go    | file.go<br>dir/file.go |
> | @js    | file.js<br>dir/file.js |

If a comment already exists, it will update the existing comment.

#### Setup

Add `.github/workflows/codenotify.yml` to your repository with the following contents:

```yaml
name: codenotify
on:
  pull_request_target:
    types: [opened, synchronize, ready_for_review]

jobs:
  codenotify:
    runs-on: ubuntu-latest
    name: codenotify
    permissions:
      pull-requests: write
    steps:
      - uses: actions/checkout@v2
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - uses: sourcegraph/codenotify@v0.5
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
#       with:
#         # Filename in which file subscribers are defined, default is 'CODENOTIFY'
#         filename: 'CODENOTIFY'
#         # The threshold of notifying subscribers to prevent broad spamming, 0 to disable (default)
#         subscriber-threshold: '10'
```

##### GITHUB_TOKEN

The default configuration above uses [automatic token authentication](https://docs.github.com/en/actions/security-guides/automatic-token-authentication#about-the-github_token-secret), but a limitation with this method of authentication is that Codenotify will not be able to mention teams.

If you want Codenotify to be able to mention teams, then you need to:
1. Create a [personal access token](https://github.com/settings/tokens) with the following permissions:
    * `read:org` is necessary to mention teams
    * `repo` is necessary if you want to use Codenotify with private repositories. Otherwise, `public_repo` is sufficient.
    * If you are an organization, consider creating the PAT under a separate bot account.
2. Store the PAT as a secret in your repository or organization (recommend naming this `CODENOTIFY_GITHUB_TOKEN`)
3. Update `.github/workflows/codenotify.yml` to use the secret you just created. For example:
    ```diff
    - GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    + GITHUB_TOKEN: ${{ secrets.CODENOTIFY_GITHUB_TOKEN }}
    ```
    
##### Behavior on forks

Codenotify does not work on forks

## CODENOTIFY files

CODENOTIFY files contain rules that define who gets notified when files change.

Here is an example:

```ignore
# Lines that start with a # are a comment.

# Empty lines are ignored.

# Each non-comment/non-empty line is a file pattern followed by one or more subscribers separated by whitespace.
# File patterns are relative to the directory of the CODENOTIFY file that they are defined in.
# Absolute paths that start with a slash will not match anything.
# Example:
# Both @alice and @bob subscribe to file.go.
# @wont-match is not subscribed to any changes because /file.go will never match.
file.go     @alice @bob
/file.go    @wont-match

# Ordering of rules, and ordering of subscribers within rules, does not matter.
# Each additional rule is additive and there is no precedence.
# Example: Both @alice and @bob subscribe to file.go.
file.go @alice
file.go @bob

# A rule can match files in subdirectories of the CODENOTIFY file's directory.
# Example:
subdir/file.go @alice
# Alternatively, you can place a CODENOTIFY file in the subdirectory.

# A rule can not match files in parent directories of the CODENOTIFY file.
# Example: @wont-match won't be notified of changes to file.go.
../file.go @wont-match

# * is a wildcard that matches any part of a file name (but not directory separators).
# It does not recursively match files in subdirectories.
# Example:
# @all-this-dir subscribes to all files in this directory.
# @all-go-this-dir subscribes to all Go files in this directory.
*       @all-this-dir
*.go    @all-go-this-dir

# ** is a wildcard that matches zero or more directories.
# Example:
# @all-readme subscribes to all readme.md files in this directory and all subdirectories.
# @all-dir subscribes to all files inside of directory "dir" relative to the location of this CODEOWNERS file.
# @all-docs subscribes to all files that are have an ancestor directory named "doc"
# @all-go subscribes to all Go files in this directory and all subdirectories.
# @all subscribes to all files in this directory and all subdirectories.
**/readme.md    @all-readme
dir/**"         @all-dir
**/doc/**       @all-docs
**/*.go         @all-go
**/*            @all
```



## Why use Codenotify?

GitHub projects can create a [CODEOWNERS](https://docs.github.com/en/github/creating-cloning-and-archiving-repositories/about-code-owners) file (inspired by [Chromium's use of OWNERS files](https://chromium.googlesource.com/chromium/src/+/master/docs/code_reviews.md#OWNERS-files)) and GitHub will automatically add reviewers. There are a few downsides to this approach:

1. There can be only one CODEOWNERS file, which causes the file to be hard to maintain for large projects.
1. Rules have precedence, which means you have to understand the context of the whole CODEOWNERS file to understand the implications of a single rule.
1. Some developers want to be notified of changes to particular files without creating the expectation that they will review changes. Unfortunately, both the name and GitHub's UI/UX treatment of CODEOWNERS implies a gatekeeper approach to code review. When CODEOWNERS is used for notifications, the number of reviewers automatically added to PRs increases, which creates two problems:

   1. There is a diffusion of responsibility for the reviewers because it isn't clear who is responsible for actually reviewing the code.
   1. PR authors don't know whose review they are actually waiting for.

The [OWNERS](https://chromium.googlesource.com/chromium/src/+/master/docs/code_reviews.md#OWNERS-files) files in the Chromium project have the same set of tradeoffs as CODEOWNERS, except OWNERS files can be nested in directories.

Codenotify makes a different set of tradeoffs:

1. There can be a CODENOTIFY file in any directory.
1. Rules are additive and do not have precedence.
1. CODENOTIFY is focused on notifications, not code review. The GitHub Action mentions subscribers in a comment instead of adding them to the "Reviewers" list of a PR.

Codenotify can be used in conjunction with, or as a replacement for CODEOWNERS.

Using codenotify as a replacement for CODEOWNERS gives PR authors the agency to decide who the best person to review the code is (e.g., based on who authored/edited/reviewed the code most recently/frequently) and to explicitly request those reviews.
