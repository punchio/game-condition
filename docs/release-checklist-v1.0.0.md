# `v1.0.0` release checklist

This checklist prepares the first tagged release of `github.com/punchio/game-condition`. It does not represent a completed release.

## Release identity

| Field | Value |
| --- | --- |
| Tag | `v1.0.0` |
| Target branch | `main` |
| GitHub Release title | `game-condition v1.0.0: Generic Instance Keys` |
| Release notes | `docs/release-notes-v1.0.0.md` |
| GitHub Release type | Latest; not a pre-release |

The generic key API breaks source compatibility with earlier untagged revisions. Because `v1.0.0` is the first tag, no prior tagged Go module major version needs a compatibility release.

## 1. Completed local preparation

- [x] `Counter`, `Manager`, and `NewManager` support `InstanceKey comparable`.
- [x] String and comparable-struct keys are tested through update, read, callback, unregister, and delete paths.
- [x] Existing tests and benchmarks use `uint64` keys.
- [x] Public examples, package documentation, design documentation, and the migration guide are updated.
- [x] Local tests, examples, `go vet`, formatting checks, and `git diff --check` pass.
- [x] All benchmarks complete a one-iteration smoke run; this is not a performance comparison.
- [x] Local race testing status is documented: it was not run because gcc is unavailable on the Windows development machine.

## 2. Review and rerun local checks

Review the complete diff, including untracked files, before staging:

```bash
git status --short
git diff
git diff --check
```

Run the final local gates:

```bash
files="$(gofmt -l .)"
if [ -n "$files" ]; then
  printf 'Unformatted files:\n%s\n' "$files"
  exit 1
fi

go test -count=1 ./...
go vet ./...
go test -run '^Example' -count=1 -v .
go test -run '^$' -bench . -benchtime 1x -count=1 .
```

- [ ] The diff contains only the intended generic-key implementation, tests, examples, and documentation.
- [ ] No internal design or implementation-plan files are included.
- [ ] Formatting, tests, examples, vet, benchmark smoke tests, and whitespace checks pass.
- [ ] Release notes and this checklist are included in the files to be published.

## 3. Commit and push `main`

Stage the reviewed files explicitly:

```bash
git add README.md contract.go manager.go program.go reader.go doc.go \
  condition_test.go harness_test.go generic_key_test.go \
  bench_test.go event_bench_test.go example_test.go \
  docs/design.md docs/migration-generic-instance-key.md \
  docs/release-notes-v1.0.0.md docs/release-checklist-v1.0.0.md
```

Confirm the staged diff, then commit and push:

```bash
git diff --cached --check
git diff --cached --stat
git commit -m "feat: support generic instance keys"
git push origin main
```

- [ ] The commit contains the complete generic-key change and release materials.
- [ ] `main` is pushed without force.
- [ ] `git status --short --branch` shows a clean worktree aligned with `origin/main`.

## 4. Require green CI before tagging

Open the [CI workflow](https://github.com/punchio/game-condition/actions/workflows/ci.yml) for the pushed `main` commit.

- [ ] The Go 1.22.x job passes formatting, `go vet ./...`, and `go test -race -count=1 ./...`.
- [ ] The Go stable job passes formatting, `go vet ./...`, and `go test -race -count=1 ./...`.
- [ ] The successful CI run belongs to the exact commit to be tagged.

Do not tag if either job fails or if the run belongs to an earlier commit.

## 5. Create and push the tag

Confirm that the tag does not already exist, then tag the verified `main` commit:

```bash
git switch main
git pull --ff-only origin main
git ls-remote --tags origin v1.0.0
git tag -a v1.0.0 -m "game-condition v1.0.0"
git push origin v1.0.0
```

`git ls-remote --tags origin v1.0.0` must produce no output before `git tag` is run.

- [ ] The annotated `v1.0.0` tag points to the CI-verified commit.
- [ ] The tag is pushed without force.

## 6. Publish the GitHub Release

Open [Create a new release](https://github.com/punchio/game-condition/releases/new) and use:

- **Choose a tag:** `v1.0.0`
- **Target:** `main`
- **Release title:** `game-condition v1.0.0: Generic Instance Keys`
- **Description:** copy the sections from `## Highlights` through `## Validation` in `docs/release-notes-v1.0.0.md`
- **Set as the latest release:** enabled
- **Pre-release:** disabled

- [ ] The description includes the breaking-change notice and migration-guide link.
- [ ] The description does not claim that local race testing passed.
- [ ] The release is published against `v1.0.0`, not an untagged commit.

## 7. Verify the published release

Open the expected URLs after publishing:

- Release: <https://github.com/punchio/game-condition/releases/tag/v1.0.0>
- Go reference: <https://pkg.go.dev/github.com/punchio/game-condition>

Verify that the module proxy can resolve the release:

```bash
go list -m github.com/punchio/game-condition@v1.0.0
```

- [ ] The GitHub Release is visible and marked Latest.
- [ ] The tag and release point to the intended commit.
- [ ] The migration-guide link works from the published release.
- [ ] The module version resolves successfully; allow time for proxy and documentation indexing if necessary.
