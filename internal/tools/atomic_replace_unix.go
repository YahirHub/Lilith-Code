//go:build !windows

package tools

import "os"

func commitFileAtomic(source, destination string, replaceExisting bool) (sourceMoved bool, err error) {
	if replaceExisting {
		if err := os.Rename(source, destination); err != nil {
			return false, err
		}
		return true, nil
	}
	// The temporary file lives in the same directory, so linking it publishes
	// the complete inode atomically and fails with fs.ErrExist rather than
	// clobbering a path created after preflight. The caller removes the temporary
	// name after the link succeeds.
	if err := os.Link(source, destination); err != nil {
		return false, err
	}
	return false, nil
}

func syncParentDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
