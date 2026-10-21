package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func crocoAtomicSnapshotFixture(t *testing.T) (string, *os.File, *crocoFieldSnapshot, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "change.json")
	snap := &crocoFieldSnapshot{Version: 1, Site: "https://fixture.invalid/wp-json", PostType: "post", ID: "13", Before: map[string]any{"x": "original-before"}, After: map[string]any{"x": "requested-after"}}
	file, err := crocoCreateFieldSnapshot(path, snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, file, snap, original
}

func crocoAssertNoSnapshotTemps(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary snapshot files leaked: %v, %v", matches, err)
	}
}

func TestCrocoSnapshotFinalizationFailurePreservesBefore(t *testing.T) {
	failure := errors.New("injected snapshot IO failure")
	for _, stage := range []string{"partial-write", "sync", "rename"} {
		t.Run(stage, func(t *testing.T) {
			path, file, snap, original := crocoAtomicSnapshotFixture(t)
			snap.Verified = true
			contents, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			err = crocoFinalizeFieldSnapshot(file, contents, func(temporary *os.File, contents []byte) error {
				if stage == "partial-write" {
					if _, err := temporary.Write(contents[:12]); err != nil {
						t.Fatal(err)
					}
					return failure
				}
				if _, err := temporary.Write(contents); err != nil {
					t.Fatal(err)
				}
				if stage == "sync" {
					return failure
				}
				if err := temporary.Sync(); err != nil {
					t.Fatal(err)
				}
				// A vanished source makes the final rename fail after the new
				// bytes were fully written, without touching the original path.
				return os.Remove(temporary.Name())
			})
			if err == nil {
				t.Fatal("finalization failure reported success")
			}
			persisted, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(original, persisted) {
				t.Fatalf("original snapshot changed on %s failure: %v", stage, err)
			}
			var saved crocoFieldSnapshot
			if err := json.Unmarshal(persisted, &saved); err != nil || saved.Before["x"] != "original-before" || saved.Verified {
				t.Fatalf("pre-write backup is not recoverable: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("snapshot lost private permissions: %v", err)
			}
			crocoAssertNoSnapshotTemps(t, path)
		})
	}
}

func TestCrocoSnapshotFinalizationAtomicSuccess(t *testing.T) {
	path, file, snap, _ := crocoAtomicSnapshotFixture(t)
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	snap.Verified = true
	snap.After["x"] = "normalized-after"
	if err := crocoWriteFieldSnapshot(file, snap); err != nil {
		t.Fatal(err)
	}
	loaded, err := crocoReadFieldSnapshot(path)
	if err != nil || !loaded.Verified || loaded.Before["x"] != "original-before" || loaded.After["x"] != "normalized-after" {
		t.Fatalf("verified snapshot readback failed: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil || after.Mode().Perm() != 0600 {
		t.Fatalf("finalized snapshot is not private: %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatal("finalization rewrote the original inode instead of replacing it")
	}
	crocoAssertNoSnapshotTemps(t, path)
}

func TestCrocoSnapshotFinalizationRejectsPathReplacement(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		for _, timing := range []string{"before", "during"} {
			t.Run(kind+"-"+timing, func(t *testing.T) {
				path, file, snap, original := crocoAtomicSnapshotFixture(t)
				retained := filepath.Join(filepath.Dir(path), "retained-original.json")
				target := filepath.Join(filepath.Dir(path), "unrelated.json")
				other := []byte("unrelated file must survive")
				if err := os.WriteFile(target, other, 0600); err != nil {
					t.Fatal(err)
				}
				replace := func() {
					if err := os.Rename(path, retained); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						if err := os.Symlink(target, path); err != nil {
							t.Skipf("symlink unavailable: %v", err)
						}
					} else if err := os.WriteFile(path, other, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if timing == "before" {
					replace()
				}
				snap.Verified = true
				contents, err := json.Marshal(snap)
				if err != nil {
					t.Fatal(err)
				}
				err = crocoFinalizeFieldSnapshot(file, contents, func(temporary *os.File, contents []byte) error {
					if err := crocoWriteSnapshotBytes(temporary, contents); err != nil {
						return err
					}
					if timing == "during" {
						replace()
					}
					return nil
				})
				if err == nil {
					t.Fatal("replaced snapshot path accepted")
				}
				kept, err := os.ReadFile(retained)
				if err != nil || !bytes.Equal(kept, original) {
					t.Fatalf("original backup was modified: %v", err)
				}
				replacement, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(replacement, other) {
					t.Fatalf("replacement was overwritten: %v", err)
				}
				untouched, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(untouched, other) {
					t.Fatalf("symlink target was overwritten: %v", err)
				}
				crocoAssertNoSnapshotTemps(t, path)
			})
		}
	}
}
