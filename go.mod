module github.com/0xkhdr/specd-cli

go 1.26

// v0.1.0 published no binaries: its release run failed on macOS and Windows,
// and the publish job was correctly gated behind that failure. The tag is
// installable, but the release/release-decision.md inside it describes those
// two platforms as gated-but-not-yet-observed when the run that observed them
// had already failed. Retracted so a false platform claim is not selectable.
// The code itself is sound on linux/amd64. Use v0.1.1.
retract v0.1.0
