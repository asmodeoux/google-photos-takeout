package version

// Version is the public release name. It matches CHANGELOG.md. CI builds set
// it with -ldflags "-X github.com/asmodeoux/google-photos-takeout/internal/version.Version=...".
var Version = "0.1.0"
