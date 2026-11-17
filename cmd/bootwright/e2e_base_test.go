package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2EBaseKitsAreNamedAndShared(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "e2e")
	kits, err := os.ReadDir(filepath.Join(root, "_base"))
	if err != nil {
		t.Fatal(err)
	}
	users := map[string]int{}
	for _, kit := range kits {
		users[kit.Name()] = 0
	}
	cases, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range cases {
		if !entry.IsDir() || entry.Name() == "_base" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "kits.txt"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, kit := range strings.Fields(string(data)) {
			if _, ok := users[kit]; !ok {
				t.Errorf("e2e/%s/kits.txt names %q, which is no directory of e2e/_base", entry.Name(), kit)
				continue
			}
			users[kit]++
			err := filepath.WalkDir(filepath.Join(root, "_base", kit), func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				relative, _ := filepath.Rel(filepath.Join(root, "_base", kit), path)
				if _, err := os.Stat(filepath.Join(root, entry.Name(), "templates", relative)); err == nil {
					t.Errorf("e2e/%s and kit %s both hold templates/%s", entry.Name(), kit, relative)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for kit, count := range users {
		if count < 2 {
			t.Errorf("kit %s is taken by %d cases; a base holds only what several cases repeat", kit, count)
		}
	}
}
