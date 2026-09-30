//go:build linux && amd64

package ansiblerunner

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// outputsDirectory holds the files a run may leave for the runner to read
// back. It lives inside the job, so removing the job removes every output.
const outputsDirectory = "outputs"

// checkOutputs refuses a declared output before anything runs. Only a run on
// the controller writes into this host's job, so no other placement can leave
// one; each output is named once, by a safe name, and reaches the adapter
// under a variable.
func checkOutputs(request lifecycle.RunRequest) error {
	if len(request.Outputs) == 0 {
		return nil
	}
	if !request.Placement.Local() {
		return failure("lifecycle.state", "an adapter output is read only from a run on the controller", "")
	}
	names := make(map[string]bool, len(request.Outputs))
	for _, output := range request.Outputs {
		if !reconciliation.ValidSegment(output.Name) || output.Variable == "" || names[output.Name] {
			return failure("lifecycle.state", "an adapter output is named twice, or by an invalid name or variable", "")
		}
		names[output.Name] = true
	}
	return nil
}

// prepareOutputs creates the private directory a run's outputs are left in and
// names, under each output's variable, the path the adapter writes it to. A
// request that declares none gets neither, so no other adapter's variables
// change.
func prepareOutputs(job string, request lifecycle.RunRequest) (map[string]any, error) {
	if len(request.Outputs) == 0 {
		return nil, nil
	}
	directory := filepath.Join(job, outputsDirectory)
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, failure("lifecycle.state", "private adapter output storage is unavailable", "")
	}
	paths := make(map[string]any, len(request.Outputs))
	for _, output := range request.Outputs {
		paths[output.Variable] = filepath.Join(directory, output.Name)
	}
	return paths, nil
}

// readOutputs reads back what a completed run left, through the held job
// parent and the same checks a sweep makes: each output is a private regular
// file of the owner with one link, the entry listed, of mode 0600 and of one
// byte up to the part bound, read exactly. An output the run did not leave
// produces nothing; anything else clears what was read and fails the run.
func (r Runner) readOutputs(job string, outputs []lifecycle.OutputFile, remediation string) ([]lifecycle.Produced, error) {
	dirs, err := r.openRunDirectories()
	if err != nil {
		return nil, unsafeOutput(remediation)
	}
	defer dirs.close()
	root, _, ok := dirs.directory(dirs.jobs, filepath.Base(job))
	if !ok {
		return nil, unsafeOutput(remediation)
	}
	defer root.Close()
	directory, _, ok := dirs.directory(root, outputsDirectory)
	if !ok {
		return nil, unsafeOutput(remediation)
	}
	defer directory.Close()
	produced := []lifecycle.Produced{}
	for _, output := range outputs {
		value, present, err := dirs.output(directory, output.Name, remediation)
		if err != nil {
			lifecycle.ClearProduced(produced)
			return nil, err
		}
		if !present {
			continue
		}
		produced = append(produced, lifecycle.Produced{Name: output.Name, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value})})
		clear(value)
	}
	return produced, nil
}

func (d runDirectories) output(directory *os.Root, name, remediation string) ([]byte, bool, error) {
	if _, err := directory.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	file, info, ok := d.file(directory, name)
	if !ok {
		return nil, false, unsafeOutput(remediation)
	}
	defer file.Close()
	if info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > secrets.MaxPartBytes {
		return nil, false, unsafeOutput(remediation)
	}
	value := make([]byte, info.Size())
	if _, err := io.ReadFull(file, value); err != nil {
		clear(value)
		return nil, false, unsafeOutput(remediation)
	}
	var extra [1]byte
	read, err := file.Read(extra[:])
	clear(extra[:])
	if read != 0 || !errors.Is(err, io.EOF) {
		clear(value)
		return nil, false, unsafeOutput(remediation)
	}
	return value, true, nil
}

func unsafeOutput(remediation string) error {
	return failure("lifecycle.state", "an adapter output is not a private regular file of the expected size", remediation)
}
