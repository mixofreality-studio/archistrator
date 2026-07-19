//go:build !localdist

// spa_stub.go is the no-op arm of the embedded-SPA seam (spaFS) for the
// DEFAULT build — i.e. every build that does NOT pass `-tags localdist`. This
// is what cloud images build (see Dockerfile): the SPA stays nginx-served,
// unchanged, exactly as before local-first-init-funnel Task 4. mountSPA
// (spa_handler.go) checks the boolean this returns and no-ops when false, so
// hooks.go's ExtraMounts can call it unconditionally for every profile/build
// without needing its own build-tag branch.
package main

import "io/fs"

func spaFS() (fs.FS, bool) { return nil, false }
