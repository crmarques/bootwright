package comparisonlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && strings.Contains(os.Args[1], "/"+scratchPrefix) {
		os.Exit(recordInvocation())
	}
	os.Exit(m.Run())
}

func recordInvocation() int {
	var record strings.Builder
	for _, argument := range os.Args[1:] {
		data, err := os.ReadFile(argument)
		if err != nil {
			data = []byte("unreadable")
		}
		record.WriteString(filepath.Base(filepath.Dir(argument)) + "/" + filepath.Base(argument) + "=" + string(data) + ";")
	}
	environment := os.Environ()
	slices.Sort(environment)
	record.WriteString(strings.Join(environment, ",") + "\n")
	handle, err := os.OpenFile(filepath.Join(os.Getenv("HOME"), "log"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return 99
	}
	handle.WriteString(record.String())
	handle.Close()
	return 7
}

type recordingTrees struct {
	resolved  string
	published string
	files     []rendering.TreeFile
}

func (r *recordingTrees) Resolve(path string) (string, error) {
	r.resolved = path
	return path, nil
}

func (r *recordingTrees) PublishTree(_ context.Context, directory string, files []rendering.TreeFile) (string, error) {
	r.published, r.files = directory, files
	for _, file := range files {
		target := filepath.Join(directory, file.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, file.Data, 0o600); err != nil {
			return "", err
		}
	}
	return directory, nil
}

type forbiddenTrees struct{ t *testing.T }

func (f forbiddenTrees) Resolve(string) (string, error) {
	f.t.Fatal("root resolved a path the invoking account names")
	return "", nil
}

func (f forbiddenTrees) PublishTree(context.Context, string, []rendering.TreeFile) (string, error) {
	f.t.Fatal("root wrote a tree the invoking account owns")
	return "", nil
}

func sameAccount(home string) func(context.Context) (Account, error) {
	return func(context.Context) (Account, error) {
		return Account{UID: os.Geteuid(), GID: os.Getegid(), Home: home}, nil
	}
}

var synthetic = Account{UID: 60001, GID: 60002, Groups: []uint32{60002, 60003}, Home: "/home/synthetic"}

func refusalCode(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 {
		t.Fatalf("error %#v", err)
	}
	return found[0]
}

func TestWriteTreesResolvesTheAbsolutePathThenPublishes(t *testing.T) {
	t.Chdir(t.TempDir())
	trees := &recordingTrees{}
	want, err := filepath.Abs("cmp")
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Trees: trees, Account: sameAccount(t.TempDir())}
	directory, err := service.WriteTrees(context.Background(), "cmp", []rendering.TreeFile{{Path: "notes.txt"}})
	if err != nil || directory != want || trees.resolved != want || trees.published != want || len(trees.files) != 1 {
		t.Fatalf("directory %q, error %v, trees %+v", directory, err, trees)
	}
}

func TestRunToolRunsOnceForEachPairInOrderWithOnlyItsEnvironmentAndIgnoresItsStatus(t *testing.T) {
	home, scratch := t.TempDir(), t.TempDir()
	trees := &recordingTrees{}
	files := []rendering.TreeFile{
		{Path: "desired/a.yaml", Data: []byte("d-a")}, {Path: "applied/a.yaml"},
		{Path: "desired/b.yaml"}, {Path: "applied/b.yaml", Data: []byte("a-b")},
	}
	pairs := []rendering.ComparisonPair{
		{Desired: "desired/a.yaml", Other: "applied/a.yaml"}, {Desired: "desired/b.yaml", Other: "applied/b.yaml"},
	}
	t.Setenv("BOOTWRIGHT_AMBIENT", "leaked")
	service := Service{Trees: trees, Account: sameAccount(home), Scratch: scratch, Terminal: "xterm-256color", Language: "pt_BR.UTF-8"}
	if err := service.RunTool(context.Background(), os.Args[0], files, pairs); err != nil {
		t.Fatalf("a program that exits 7 failed the command: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "log"))
	if err != nil {
		t.Fatal(err)
	}
	environment := "HOME=" + home + ",LANG=pt_BR.UTF-8,PATH=/usr/local/bin:/usr/bin:/bin,TERM=xterm-256color\n"
	want := "desired/a.yaml=d-a;applied/a.yaml=;" + environment + "desired/b.yaml=;applied/b.yaml=a-b;" + environment
	if string(data) != want {
		t.Fatalf("the program received\n%s\nwant\n%s", data, want)
	}
	if !strings.HasPrefix(trees.published, scratch+string(filepath.Separator)+scratchPrefix) {
		t.Fatalf("the trees were written to %q, not a private directory under the scratch parent", trees.published)
	}
	if left, _ := os.ReadDir(scratch); len(left) != 0 {
		t.Fatalf("the private directory was left behind: %v", left)
	}
}

func TestRunToolLooksABareNameUpOnTheFixedPathAlone(t *testing.T) {
	ambient := t.TempDir()
	if err := os.Symlink(os.Args[0], filepath.Join(ambient, "bootwright-comparison-probe")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ambient)
	scratch := t.TempDir()
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	err := (Service{Trees: &recordingTrees{}, Account: sameAccount(t.TempDir()), Scratch: scratch}).RunTool(context.Background(), "bootwright-comparison-probe", nil, pairs)
	found := refusalCode(t, err)
	if found.Code != "runtime.tool" || !strings.Contains(found.Message, "bootwright-comparison-probe was not found on /usr/local/bin:/usr/bin:/bin") || found.Remediation == "" {
		t.Fatalf("diagnostic %+v", found)
	}
	if left, _ := os.ReadDir(scratch); len(left) != 0 {
		t.Fatalf("a private directory was written for a program that cannot run: %v", left)
	}
}

func TestLookToolSearchesOnlyTheGivenDirectoriesInOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, file := range []struct {
		path string
		mode os.FileMode
	}{{filepath.Join(first, "cmp"), 0o644}, {filepath.Join(second, "cmp"), 0o755}, {filepath.Join(first, "both"), 0o755}, {filepath.Join(second, "both"), 0o755}} {
		if err := os.WriteFile(file.path, nil, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	for program, want := range map[string]string{"cmp": filepath.Join(second, "cmp"), "both": filepath.Join(first, "both")} {
		if got, err := lookTool(program, []string{first, second}); err != nil || got != want {
			t.Errorf("lookTool(%q) = %q, %v, want %q", program, got, err, want)
		}
	}
	if _, err := lookTool("absent", []string{first, second}); err != exec.ErrNotFound {
		t.Fatalf("an absent name resolved: %v", err)
	}
	if got, err := lookTool("./cmp", nil); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("a relative path resolved to %q, %v", got, err)
	}
}

func TestRunToolRefusesADirectoryThatIsNotAProgram(t *testing.T) {
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	scratch := t.TempDir()
	err := (Service{Trees: &recordingTrees{}, Account: sameAccount(t.TempDir()), Scratch: scratch}).RunTool(context.Background(), t.TempDir(), nil, pairs)
	if found := refusalCode(t, err); found.Code != "runtime.tool" {
		t.Fatalf("diagnostic %+v", found)
	}
	if left, _ := os.ReadDir(scratch); len(left) != 0 {
		t.Fatalf("the private directory was left behind: %v", left)
	}
}

func TestRunToolStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	err := (Service{Trees: &recordingTrees{}, Account: sameAccount(t.TempDir()), Scratch: t.TempDir()}).RunTool(ctx, "bootwright-no-such-program", nil, pairs)
	if err == nil || len(diagnostics.Of(err)) != 0 {
		t.Fatalf("error %v, want the context's", err)
	}
}

func TestANonRootProcessServingAnotherAccountRefuses(t *testing.T) {
	service := Service{Trees: forbiddenTrees{t}, Account: func(context.Context) (Account, error) { return synthetic, nil }, effective: func() int { return 1000 }}
	if _, err := service.WriteTrees(context.Background(), "/srv/cmp", nil); refusalCode(t, err).Code != "render.publish" {
		t.Fatalf("error %v", err)
	}
}

type accountHelper struct {
	t        *testing.T
	trees    *recordingTrees
	accounts []Account
	actions  []string
	reply    func(helperRequest) helperResponse
}

func (h *accountHelper) serve(ctx context.Context, account Account, data []byte) ([]byte, error) {
	var request helperRequest
	if err := json.Unmarshal(data, &request); err != nil {
		h.t.Fatal(err)
	}
	if request.UID != account.UID || request.GID != account.GID {
		h.t.Fatalf("the request names %d:%d for account %+v", request.UID, request.GID, account)
	}
	h.accounts, h.actions = append(h.accounts, account), append(h.actions, request.Action)
	var response helperResponse
	if h.reply != nil {
		response = h.reply(request)
	} else {
		directory, err := local(ctx, h.trees, request)
		response = helperResponse{Directory: directory, Failed: err != nil}
		if err != nil {
			response.Message = diagnostics.Of(err)[0].Message
		}
	}
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(response); err != nil {
		h.t.Fatal(err)
	}
	return out.Bytes(), nil
}

func rootServing(t *testing.T, helper *accountHelper) Service {
	return Service{
		Trees: forbiddenTrees{t}, Account: func(context.Context) (Account, error) { return synthetic, nil },
		effective: func() int { return 0 }, helper: helper.serve,
	}
}

func TestRootServingAnotherAccountWritesTheTreesThroughTheAccountHelper(t *testing.T) {
	helper := &accountHelper{t: t, trees: &recordingTrees{}}
	directory := filepath.Join(t.TempDir(), "cmp")
	got, err := rootServing(t, helper).WriteTrees(context.Background(), directory, []rendering.TreeFile{{Path: "notes.txt", Data: []byte("n")}})
	if err != nil || got != directory || helper.trees.published != directory || len(helper.trees.files) != 1 {
		t.Fatalf("directory %q, error %v, trees %+v", got, err, helper.trees)
	}
	if !slices.Equal(helper.actions, []string{actionPublish}) || len(helper.accounts) != 1 || helper.accounts[0].UID != synthetic.UID || helper.accounts[0].GID != synthetic.GID {
		t.Fatalf("helper calls %v as %+v", helper.actions, helper.accounts)
	}
}

func TestRootServingAnotherAccountReportsTheHelpersRefusal(t *testing.T) {
	for reply, want := range map[string]string{
		"/srv/cmp is not empty, so nothing is written into it": "/srv/cmp is not empty, so nothing is written into it",
		"forged\x1b[2Jline": "the comparison files cannot be written under the invoking account",
	} {
		helper := &accountHelper{t: t, reply: func(helperRequest) helperResponse {
			return helperResponse{Failed: true, Message: reply, Remediation: "pass a directory that does not exist yet or is empty"}
		}}
		_, err := rootServing(t, helper).WriteTrees(context.Background(), "/srv/cmp", nil)
		if found := refusalCode(t, err); found.Code != "render.publish" || found.Message != want {
			t.Errorf("diagnostic %+v, want %q", found, want)
		}
	}
}

func TestRootServingAnotherAccountRefusesAnUnusableHelperDirectory(t *testing.T) {
	for _, directory := range []string{"", "relative", "/srv/../etc"} {
		helper := &accountHelper{t: t, reply: func(helperRequest) helperResponse { return helperResponse{Directory: directory} }}
		if _, err := rootServing(t, helper).WriteTrees(context.Background(), "/srv/cmp", nil); refusalCode(t, err).Code != "render.publish" {
			t.Errorf("directory %q was accepted", directory)
		}
	}
}

func TestRootServingAnotherAccountRunsTheToolAsThatAccount(t *testing.T) {
	helper := &accountHelper{t: t, trees: &recordingTrees{}}
	service := rootServing(t, helper)
	service.Scratch, service.Terminal, service.Language = t.TempDir(), "xterm", "C.UTF-8"
	var commands []*exec.Cmd
	service.start = func(command *exec.Cmd) (bool, error) {
		commands = append(commands, command)
		return true, &exec.ExitError{}
	}
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	if err := service.RunTool(context.Background(), "/usr/bin/meld", []rendering.TreeFile{{Path: "desired/a.yaml"}, {Path: "applied/a.yaml"}}, pairs); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(helper.actions, []string{actionScratch, actionRemove}) || len(commands) != 1 {
		t.Fatalf("helper calls %v, %d commands", helper.actions, len(commands))
	}
	scratch := helper.trees.published
	command := commands[0]
	if !slices.Equal(command.Args, []string{"/usr/bin/meld", filepath.Join(scratch, "desired/a.yaml"), filepath.Join(scratch, "applied/a.yaml")}) || command.Dir != scratch {
		t.Fatalf("command %q in %q", command.Args, command.Dir)
	}
	if !slices.Equal(command.Env, []string{"TERM=xterm", "LANG=C.UTF-8", "HOME=/home/synthetic", "PATH=/usr/local/bin:/usr/bin:/bin"}) {
		t.Fatalf("environment %q", command.Env)
	}
	if credential := command.SysProcAttr.Credential; credential == nil || credential.Uid != 60001 || credential.Gid != 60002 || !slices.Equal(credential.Groups, []uint32{60002, 60003}) {
		t.Fatalf("credential %#v", credential)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("the private directory %q was left behind: %v", scratch, err)
	}
}

func TestServeHelperAcceptsOnlyItsOwnAccountsRequest(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the helper refuses to serve root")
	}
	trees := &recordingTrees{}
	directory := filepath.Join(t.TempDir(), "cmp")
	for _, test := range []struct {
		name    string
		request helperRequest
		code    int
	}{
		{"its own account", helperRequest{UID: os.Getuid(), GID: os.Getegid(), Action: actionPublish, Directory: directory}, 0},
		{"another account", helperRequest{UID: os.Getuid() + 1, GID: os.Getegid(), Action: actionPublish, Directory: directory}, 1},
		{"an unknown action", helperRequest{UID: os.Getuid(), GID: os.Getegid(), Action: "run", Directory: directory}, 0},
	} {
		data, err := json.Marshal(test.request)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if handled, code := ServeHelper(context.Background(), []string{HelperMode}, bytes.NewReader(data), &out, trees); !handled || code != test.code {
			t.Errorf("%s: handled %t, code %d", test.name, handled, code)
		}
		if test.code != 0 {
			continue
		}
		got, err := decodeResponse(test.request.Action, out.Bytes())
		if test.request.Action == actionPublish && (err != nil || got != directory) {
			t.Errorf("%s: directory %q, error %v", test.name, got, err)
		}
		if test.request.Action != actionPublish && err == nil {
			t.Errorf("%s was served", test.name)
		}
	}
	if handled, _ := ServeHelper(context.Background(), []string{"diff"}, nil, nil, trees); handled {
		t.Fatal("an ordinary invocation was taken for the helper")
	}
}

func rootAccount(context.Context) (Account, error) {
	return Account{UID: 0, GID: 0, Home: "/root"}, nil
}

func TestRunToolRefusesARootInvokingAccountBeforeWritingAnything(t *testing.T) {
	service := Service{Trees: forbiddenTrees{t}, Account: rootAccount, effective: func() int { return 0 }, Scratch: t.TempDir()}
	service.start = func(*exec.Cmd) (bool, error) {
		t.Fatal("a comparison program was started as root")
		return false, nil
	}
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	found := refusalCode(t, service.RunTool(context.Background(), "/usr/bin/meld", []rendering.TreeFile{{Path: "desired/a.yaml"}}, pairs))
	if found.Code != "runtime.tool" || !strings.Contains(found.Message, "/usr/bin/meld") || !strings.Contains(found.Message, "invoking account is root") || !strings.Contains(found.Remediation, "--output-dir") {
		t.Fatalf("diagnostic %+v", found)
	}
}

func TestWriteTreesServesARootInvokingAccount(t *testing.T) {
	trees := &recordingTrees{}
	directory := filepath.Join(t.TempDir(), "cmp")
	service := Service{Trees: trees, Account: rootAccount, effective: func() int { return 0 }}
	if got, err := service.WriteTrees(context.Background(), directory, []rendering.TreeFile{{Path: "notes.txt"}}); err != nil || got != directory {
		t.Fatalf("directory %q, error %v", got, err)
	}
}

type countingTrees struct{ published int }

func (c *countingTrees) Resolve(path string) (string, error) { return path, nil }

func (c *countingTrees) PublishTree(_ context.Context, directory string, _ []rendering.TreeFile) (string, error) {
	c.published++
	return directory, nil
}

func TestTreeBytesIsTheEncodedSizeOfTheFiles(t *testing.T) {
	for _, files := range [][]rendering.TreeFile{
		nil, {}, {{Path: "a"}}, {{Path: "a", Data: []byte{}}},
		{{Path: "desired/<&>\"\\\x01\xffç.yaml", Data: []byte("x")}, {Path: "b", Data: []byte("yz")}, {Path: "c", Data: []byte("xyz")}},
	} {
		data, err := json.Marshal(files)
		if err != nil || treeBytes(files) != len(data) {
			t.Errorf("%q: treeBytes %d, encoded %d, error %v", files, treeBytes(files), len(data), err)
		}
	}
}

func TestTheTreeBoundIsTheSameWithAndWithoutTheAccountHelper(t *testing.T) {
	data := make([]byte, (maximumTree-24)/4*3)
	fitting, over := []rendering.TreeFile{{Path: "p", Data: data}}, []rendering.TreeFile{{Path: "pq", Data: data}}
	if treeBytes(fitting) != maximumTree || treeBytes(over) != maximumTree+1 {
		t.Fatalf("sizes %d and %d around %d", treeBytes(fitting), treeBytes(over), maximumTree)
	}
	directory := filepath.Join(t.TempDir(), "cmp")
	for _, test := range []struct {
		files []rendering.TreeFile
		fits  bool
	}{{fitting, true}, {over, false}} {
		trees := &countingTrees{}
		_, localErr := (Service{Trees: trees, Account: sameAccount(t.TempDir())}).WriteTrees(context.Background(), directory, test.files)
		helper := &accountHelper{t: t, reply: func(request helperRequest) helperResponse { return helperResponse{Directory: request.Directory} }}
		_, helperErr := rootServing(t, helper).WriteTrees(context.Background(), directory, test.files)
		if (localErr == nil) != test.fits || (helperErr == nil) != test.fits || trees.published != len(helper.actions) {
			t.Fatalf("fits %t: local %v (%d writes), helper %v (%d calls)", test.fits, localErr, trees.published, helperErr, len(helper.actions))
		}
		if !test.fits && (refusalCode(t, localErr).Message != refusalCode(t, helperErr).Message) {
			t.Fatalf("the refusals differ: %v and %v", localErr, helperErr)
		}
	}
}

type failingTrees struct{ countingTrees }

func (f *failingTrees) PublishTree(context.Context, string, []rendering.TreeFile) (string, error) {
	return "", refuse("the tree cannot be written", "report this")
}

func TestRunToolRemovesThePrivateDirectoryWhenTheTreeFails(t *testing.T) {
	scratch := t.TempDir()
	pairs := []rendering.ComparisonPair{{Desired: "desired/a.yaml", Other: "applied/a.yaml"}}
	err := (Service{Trees: &failingTrees{}, Account: sameAccount(t.TempDir()), Scratch: scratch}).RunTool(context.Background(), "/usr/bin/meld", nil, pairs)
	if refusalCode(t, err).Code != "render.publish" {
		t.Fatalf("error %v", err)
	}
	if left, _ := os.ReadDir(scratch); len(left) != 0 {
		t.Fatalf("the private directory was left behind: %v", left)
	}
}
