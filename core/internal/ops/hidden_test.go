package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
)

// Only a file that is really absent is a sparse-checkout entry. Any other
// failure to look at a marked file must stop the operation, not hide the file.
func TestHiddenChangesFailsClosed(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "update-index", "--assume-unchanged", "README")
	denied := func(string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: "README", Err: fs.ErrPermission}
	}
	files, err := hiddenChangesWith(context.Background(), f.run, f.r.Dir, denied)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("files = %v, err = %v, want the permission error", files, err)
	}
	absent := func(string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: "README", Err: fs.ErrNotExist}
	}
	if files, err := hiddenChangesWith(context.Background(), f.run, f.r.Dir, absent); err != nil || len(files) != 0 {
		t.Fatalf("absent file: files = %v, err = %v, want none", files, err)
	}
	if files, err := hiddenChangesWith(context.Background(), f.run, f.r.Dir, os.Lstat); err != nil || len(files) != 1 || files[0] != "README" {
		t.Fatalf("present file: files = %v, err = %v", files, err)
	}
}
