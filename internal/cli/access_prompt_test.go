package cli

import (
	"bytes"
	"testing"
)

func TestAccessPromptOpensOneGroupAndIndentsItsPrompts(t *testing.T) {
	var out bytes.Buffer
	prompt := NewAccessPrompt(WithPresentation(&out, PresentationOptions{}))
	first := prompt.Line("SSH password for operator on Machine/fixture: ")
	second := prompt.Line("Sudo password for operator on Machine/fixture (Enter reuses the SSH password): ")
	group := "\n  First access\n    Typing stays hidden. Passwords are kept in memory for this run only.\n\n"
	if out.String() != group {
		t.Fatalf("group = %q, want %q", out.String(), group)
	}
	if first != "    SSH password for operator on Machine/fixture: " ||
		second != "    Sudo password for operator on Machine/fixture (Enter reuses the SSH password): " {
		t.Fatalf("prompts = %q and %q", first, second)
	}
}

func TestAccessPromptMutesOnlyItsHintOnAColorTerminal(t *testing.T) {
	var out bytes.Buffer
	prompt := NewAccessPrompt(WithPresentation(&out, PresentationOptions{Color: "always"}))
	line := prompt.Line("Sudo password for operator on Machine/fixture (Enter reuses the SSH password): ")
	want := "    Sudo password for operator on Machine/fixture\x1b[38;5;245m (Enter reuses the SSH password)\x1b[0m: "
	if line != want {
		t.Fatalf("prompt = %q, want %q", line, want)
	}
	if plain := prompt.Line("SSH password for operator on Machine/fixture: "); plain != "    SSH password for operator on Machine/fixture: " {
		t.Fatalf("a prompt without a hint = %q", plain)
	}
}

func TestAccessPromptLeavesStructuredOutputUntouched(t *testing.T) {
	var out bytes.Buffer
	prompt := NewAccessPrompt(WithPresentation(&out, PresentationOptions{Format: "json"}))
	if line := prompt.Line("SSH password for operator on Machine/fixture: "); line != "SSH password for operator on Machine/fixture: " || out.Len() != 0 {
		t.Fatalf("structured prompt = %q after writing %q", line, out.String())
	}
}
