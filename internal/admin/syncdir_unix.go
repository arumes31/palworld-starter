//go:build unix

package admin

import (
	"errors"
	"fmt"
	"os"
)

// syncParentDirectory makes a same-directory rename durable on Unix filesystems.
func syncParentDirectory(dir string) error {
	f, err := os.Open(dir) // #nosec G304 -- dir comes from the configured state path
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}

	var errs []error
	if err := f.Sync(); err != nil {
		errs = append(errs, fmt.Errorf("sync directory: %w", err))
	}
	if err := f.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close directory: %w", err))
	}
	return errors.Join(errs...)
}
