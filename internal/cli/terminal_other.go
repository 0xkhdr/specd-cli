//go:build !linux && !darwin && !windows

package cli

import "os"

// isTerminal fails closed on a platform this build has never probed: an
// underived route is agent-capable, so the human gate stays shut and a real
// terminal declares itself through SPECD_ROUTE.
func isTerminal(*os.File) bool { return false }
