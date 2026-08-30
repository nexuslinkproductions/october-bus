# Releases

October Bus publishes native runtime archives for macOS, Linux, and Windows on amd64 and arm64.

## Tagged runtime releases

A tag matching `v*` runs the full Go and TypeScript validation suite, builds the runtime and conformance runner, creates archives, generates SPDX SBOMs and SHA-256 checksums, attaches GitHub build-provenance attestations, and creates a GitHub release.

Release binaries embed the version from the tag. Tags containing a hyphen create a prerelease.

Before creating a tag:

1. confirm the protocol and package versions;
2. run the complete local validation suite;
3. update release notes and migration guidance;
4. verify that no compatibility claim exceeds current public evidence;
5. use an annotated, signed Git tag.

Platform code signing and notarization require the relevant platform identities. Unsigned artifacts must remain clearly identified until those identities and verification steps are configured.

## TypeScript prereleases

The TypeScript client uses pre-1.0 versions. Install the current package with `npm install october-bus@next`. The prerelease workflow runs typecheck, build, error tests, and Go interoperability tests before publishing. Stable releases require an approved stable protocol and SDK compatibility policy.
