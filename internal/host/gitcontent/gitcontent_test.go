package gitcontent

import "testing"

func TestSchemeClassifiesSupportedURLs(t *testing.T) {
	cases := map[string]string{
		"https://git.example/infra.git":    SchemeHTTPS,
		"ssh://git@git.example/infra.git":  SchemeSSH,
		"git@git.example:infra/os.git":     SchemeSSH,
		"file:///srv/git/os-hardening.git": SchemeLocal,
		"/srv/git/os-hardening.git":        SchemeLocal,
	}
	for raw, want := range cases {
		got, err := Scheme(raw)
		if err != nil {
			t.Fatalf("Scheme(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("Scheme(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSchemeRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://git.example/infra.git",
		"ext::sh -c whoami",
		"ftp://git.example/infra.git",
		"relative/path.git",
	} {
		if _, err := Scheme(raw); err == nil {
			t.Fatalf("Scheme(%q) should have been rejected", raw)
		}
	}
}

func TestValidateSubdirRejectsEscapes(t *testing.T) {
	if err := ValidateSubdir("bootwright"); err != nil {
		t.Fatalf("plain subdir rejected: %v", err)
	}
	if err := ValidateSubdir(""); err != nil {
		t.Fatalf("empty subdir rejected: %v", err)
	}
	for _, bad := range []string{"/abs", "../escape", ".."} {
		if err := ValidateSubdir(bad); err == nil {
			t.Fatalf("ValidateSubdir(%q) should have been rejected", bad)
		}
	}
}

func TestCommitReMatchesOnlyFullSHA(t *testing.T) {
	if !CommitRe.MatchString("5f2c9a1e8b4d7c06a2f31b9e4d8c5a70f1e2b3c4") {
		t.Fatal("40-hex commit should match")
	}
	for _, bad := range []string{"v1.4.0", "main", "5f2c9a1"} {
		if CommitRe.MatchString(bad) {
			t.Fatalf("%q should not be treated as a commit", bad)
		}
	}
}

func TestLocalPathStripsFileScheme(t *testing.T) {
	if got := LocalPath("file:///srv/git/os.git"); got != "/srv/git/os.git" {
		t.Fatalf("LocalPath = %q", got)
	}
	if got := LocalPath("/srv/git/os.git"); got != "/srv/git/os.git" {
		t.Fatalf("LocalPath = %q", got)
	}
}
