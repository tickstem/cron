package cron

// Version is set at build time via -ldflags="-X github.com/tickstem/cron.Version=vX.Y.Z".
// Falls back to "dev" when built without ldflags (local development).
var Version = "dev"
