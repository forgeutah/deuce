package workspacegit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// gitWorkspace sets up a fresh git repo at $tmp/<name>/content and points
// DEVPOD_AGENT_CONTENT_DIR at $tmp so workspacepath.Resolve(name) finds it.
// Returns the content dir path. Uses t.TempDir so cleanup is automatic.
func gitWorkspace(t *testing.T, name string) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("DEVPOD_AGENT_CONTENT_DIR", base)
	content := filepath.Join(base, name, "content")
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = content
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(content, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "hello.txt")
	run("commit", "-q", "-m", "initial")
	return content
}

func TestCaptureHead_ReturnsSHA(t *testing.T) {
	content := gitWorkspace(t, "ws1")
	sha, err := CaptureHead(context.Background(), "ws1")
	if err != nil {
		t.Fatalf("CaptureHead: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len=%d)", sha, len(sha))
	}
	// Sanity: it should match what git itself reports.
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = content
	out, _ := cmd.Output()
	if got := string(out); got[:40] != sha {
		t.Errorf("SHA mismatch: helper=%q git=%q", sha, got)
	}
}

func TestCaptureHead_MissingWorkspace(t *testing.T) {
	t.Setenv("DEVPOD_AGENT_CONTENT_DIR", t.TempDir())
	_, err := CaptureHead(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("expected ErrWorkspaceNotFound, got %v", err)
	}
}

func TestCaptureHead_NonGitDirectory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("DEVPOD_AGENT_CONTENT_DIR", base)
	if err := os.MkdirAll(filepath.Join(base, "plain", "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CaptureHead(context.Background(), "plain")
	if err == nil {
		t.Error("expected error for non-git directory, got nil")
	}
}

func TestDiffSince_NoChanges(t *testing.T) {
	gitWorkspace(t, "ws2")
	sha, err := CaptureHead(context.Background(), "ws2")
	if err != nil {
		t.Fatal(err)
	}
	files, fc, hc, err := DiffSince(context.Background(), "ws2", sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || fc != 0 || hc != 0 {
		t.Errorf("expected empty diff, got files=%d fc=%d hc=%d", len(files), fc, hc)
	}
}

func TestDiffSince_OneFileModified(t *testing.T) {
	content := gitWorkspace(t, "ws3")
	sha, err := CaptureHead(context.Background(), "ws3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "hello.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, fc, hc, err := DiffSince(context.Background(), "ws3", sha)
	if err != nil {
		t.Fatal(err)
	}
	if fc != 1 || hc != 1 {
		t.Errorf("expected file_count=1 hunk_count=1, got fc=%d hc=%d", fc, hc)
	}
	if len(files) != 1 || files[0].Path != "hello.txt" {
		t.Errorf("expected one file 'hello.txt', got %+v", files)
	}
}

func TestDiffSince_NewFileAdded(t *testing.T) {
	content := gitWorkspace(t, "ws4")
	sha, err := CaptureHead(context.Background(), "ws4")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// `git diff <sha>` against the working tree won't show untracked files;
	// stage it to make the addition visible.
	cmd := exec.Command("git", "add", "new.txt")
	cmd.Dir = content
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	files, fc, hc, err := DiffSince(context.Background(), "ws4", sha)
	if err != nil {
		t.Fatal(err)
	}
	if fc != 1 || hc != 1 {
		t.Errorf("added file: expected fc=1 hc=1, got fc=%d hc=%d", fc, hc)
	}
	if len(files) != 1 || files[0].Path != "new.txt" {
		t.Errorf("expected one file 'new.txt', got %+v", files)
	}
}


func TestParseUnifiedDiff_Empty(t *testing.T) {
	got := parseUnifiedDiff("")
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d files", len(got))
	}
}

func TestParseUnifiedDiff_SingleFileSingleHunk(t *testing.T) {
	diff := `diff --git a/hello.go b/hello.go
index abc..def 100644
--- a/hello.go
+++ b/hello.go
@@ -1,3 +1,4 @@
 package main
-import "fmt"
+import (
+	"fmt"
+)
`
	got := parseUnifiedDiff(diff)
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got))
	}
	if got[0].Path != "hello.go" {
		t.Errorf("path: got %q, want %q", got[0].Path, "hello.go")
	}
	if len(got[0].Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(got[0].Hunks))
	}
	h := got[0].Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 {
		t.Errorf("hunk header: got %+v, want oldStart=1 oldLines=3 newStart=1 newLines=4", h)
	}
}

func TestParseUnifiedDiff_TwoFilesTwoHunks(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -10,3 +10,4 @@
 line10
-old
+new1
+new2
@@ -50,2 +51,2 @@
-removed
+added
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1 +1 @@
-x
+y
`
	got := parseUnifiedDiff(diff)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
	if got[0].Path != "a.go" || got[1].Path != "b.go" {
		t.Errorf("paths: got %q,%q want a.go,b.go", got[0].Path, got[1].Path)
	}
	if len(got[0].Hunks) != 2 {
		t.Errorf("a.go: expected 2 hunks, got %d", len(got[0].Hunks))
	}
	if len(got[1].Hunks) != 1 {
		t.Errorf("b.go: expected 1 hunk, got %d", len(got[1].Hunks))
	}
}

func TestParseUnifiedDiff_AddedFile(t *testing.T) {
	diff := `diff --git a/newfile.go b/newfile.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,2 @@
+package main
+
`
	got := parseUnifiedDiff(diff)
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got))
	}
	if got[0].Path != "newfile.go" {
		t.Errorf("added-file path: got %q, want newfile.go", got[0].Path)
	}
	if len(got[0].Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(got[0].Hunks))
	}
	if got[0].Hunks[0].OldStart != 0 || got[0].Hunks[0].OldLines != 0 {
		t.Errorf("added-file hunk: old range should be 0,0; got %+v", got[0].Hunks[0])
	}
}

func TestParseUnifiedDiff_DeletedFile(t *testing.T) {
	diff := `diff --git a/old.go b/old.go
deleted file mode 100644
index 1234567..0000000
--- a/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package main
-
`
	got := parseUnifiedDiff(diff)
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got))
	}
	if got[0].Path != "old.go" {
		t.Errorf("deleted-file path: got %q, want old.go", got[0].Path)
	}
}

func TestParseHunkHeader_SingleLine(t *testing.T) {
	h, ok := parseHunkHeader("@@ -10 +12 @@")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if h.OldStart != 10 || h.OldLines != 1 || h.NewStart != 12 || h.NewLines != 1 {
		t.Errorf("single-line range default: got %+v", h)
	}
}

func TestParseHunkHeader_WithContext(t *testing.T) {
	// `@@ -X,Y +A,B @@ function name` is a valid hunk header with trailing context.
	h, ok := parseHunkHeader("@@ -42,5 +42,7 @@ func foo() {")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if h.OldStart != 42 || h.OldLines != 5 || h.NewStart != 42 || h.NewLines != 7 {
		t.Errorf("with-context range: got %+v", h)
	}
}

func TestParseUnifiedDiff_HunkLineContent(t *testing.T) {
	diff := `diff --git a/x b/x
--- a/x
+++ b/x
@@ -1,3 +1,3 @@
 context
-removed
+added
`
	got := parseUnifiedDiff(diff)
	wantLines := []string{" context", "-removed", "+added"}
	if !reflect.DeepEqual(got[0].Hunks[0].Lines, wantLines) {
		t.Errorf("hunk lines: got %#v, want %#v", got[0].Hunks[0].Lines, wantLines)
	}
}
