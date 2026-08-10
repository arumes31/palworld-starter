//go:build !unix

package admin

// syncParentDirectory is a no-op for non-Unix local-development builds. The
// supported release and container targets are Linux; non-Unix os.Rename does
// not provide the same atomic-durability guarantee.
func syncParentDirectory(string) error {
	return nil
}
