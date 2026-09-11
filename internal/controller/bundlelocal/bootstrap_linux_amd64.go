//go:build linux && amd64

package bundlelocal

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"debug/elf"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"golang.org/x/sys/unix"
)

const pipBootstrap = `import sys, os, runpy
assert sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode
root = os.path.dirname(os.path.dirname(sys.executable))
minor = str(sys.version_info.major) + '.' + str(sys.version_info.minor)
stdlib = root + '/lib/python' + minor
sys.path[:] = [root + '/lib/python' + minor.replace('.', '') + '.zip', stdlib, stdlib + '/lib-dynload', stdlib + '/site-packages']
sys.argv = ['pip'] + sys.argv[1:]
runpy.run_module('pip', run_name='__main__')
`

func resolveBootstrapWheels(ctx context.Context, projected *projection, value prerequisites.BootstrapDefinition, egress prerequisites.SetupEgress) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	trust, err := qualifiedTrustPEM(bounded)
	if err != nil {
		return nil, err
	}
	root, cleanup, err := stageBootstrap(bounded, projected, value, trust)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	broker, err := newPublisherBroker(bounded, egress, trust)
	if err != nil {
		return nil, err
	}
	defer broker.close()
	arguments := []string{"--inhibit-cache", "--glibc-hwcaps-mask", "", "--library-path", "/python/lib", "--preload", strings.Join(value.Execution.Preload, ":"), "/" + value.PythonExecutable, "-I", "-B", "-S", "-c", pipBootstrap,
		"--isolated", "--disable-pip-version-check", "--no-input", "--no-cache-dir", "--proxy", broker.endpoint(), "--cert", "/ca.pem", "--use-deprecated=legacy-certs",
		"install", "--dry-run", "--ignore-installed", "--only-binary=:all:", "--report", "-", "--quiet", "--index-url", "https://pypi.org/simple", "ansible-core==" + value.AnsibleVersion, "urllib3"}
	command := exec.CommandContext(bounded, value.Execution.Loader, arguments...)
	command.Dir = "/"
	command.Env = []string{"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "HOME=/home", "TMPDIR=/tmp", "PIP_CONFIG_FILE=/dev/null", "OPENSSL_CONF=/dev/null", "PATH=/nonexistent"}
	command.SysProcAttr = bootstrapProcessAttributes(root)
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 2 * time.Second
	output := &boundedOutput{maximum: 8 << 20}
	diagnostic := &boundedOutput{maximum: 64 << 10}
	command.Stdout, command.Stderr = output, diagnostic
	if err := command.Run(); err != nil || output.exceeded || diagnostic.exceeded {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, bundleFailure("isolated wheel resolution failed; compatible binary wheels and unprivileged Linux namespaces are required")
	}
	return slices.Clone(output.Bytes()), nil
}

func bootstrapProcessAttributes(root string) *syscall.SysProcAttr {
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 {
		uid, gid = 65534, 65534
	}
	return &syscall.SysProcAttr{
		Chroot: root, Cloneflags: unix.CLONE_NEWUSER | unix.CLONE_NEWNS | unix.CLONE_NEWPID,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: uid, Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: gid, Size: 1}}, GidMappingsEnableSetgroups: false,
		Credential: &syscall.Credential{Uid: 0, Gid: 0}, Setpgid: true, Pdeathsig: syscall.SIGKILL,
	}
}

func stageBootstrap(ctx context.Context, projected *projection, value prerequisites.BootstrapDefinition, trust []byte) (string, func(), error) {
	if !validExecutionRequirement(value.Execution) || value.Execution.PythonExecutable != value.PythonExecutable {
		return "", nil, bundleFailure("bootstrap execution foundation is invalid")
	}
	root, err := os.MkdirTemp("/tmp", "bootwright-resolver-")
	if err != nil {
		return "", nil, bundleFailure("disposable bootstrap workspace cannot be created")
	}
	cleanup := func() { os.RemoveAll(root) }
	fail := func(err error) (string, func(), error) { cleanup(); return "", nil, err }
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 {
		uid, gid = 65534, 65534
	}
	// Parents and code stay read-only to the resolver. Only the explicit home,
	// temporary and null-file paths are writable; all are disposable.
	write := func(name string, data []byte, mode os.FileMode) error {
		if !validPath(name) {
			return bundleFailure("bootstrap staging path is invalid")
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, name), data, mode)
	}
	for name, file := range projected.files {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		mode := os.FileMode(0444)
		if file.executable {
			mode = 0555
		}
		if err := write(name, file.data, mode); err != nil {
			return fail(bundleFailure("bootstrap staging projection could not be written"))
		}
	}
	for _, file := range value.Execution.Files {
		stream, err := os.Open(file.Path)
		if err != nil {
			return fail(bundleFailure("provided bootstrap library is unavailable"))
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, (128<<20)+1))
		stream.Close()
		digest := sha256.Sum256(data)
		if readErr != nil || len(data) > 128<<20 || hex.EncodeToString(digest[:]) != file.SHA256 {
			return fail(bundleFailure("provided bootstrap library differs from its authenticated execution foundation"))
		}
		if err := write(strings.TrimPrefix(file.Path, "/"), data, 0555); err != nil {
			return fail(bundleFailure("bootstrap library staging failed"))
		}
	}
	for _, link := range value.Execution.Links {
		name := strings.TrimPrefix(link.Path, "/")
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0755); err != nil {
			return fail(err)
		}
		if err := os.Symlink(link.Target, filepath.Join(root, name)); err != nil {
			return fail(bundleFailure("bootstrap library alias staging failed"))
		}
	}
	if err := qualifyBootstrapELF(projected, value.Execution, root); err != nil {
		return fail(err)
	}
	if err := write("ca.pem", trust, 0444); err != nil {
		return fail(err)
	}
	if err := write("dev/null", nil, 0600); err != nil {
		return fail(err)
	}
	for _, directory := range []string{"home", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0700); err != nil {
			return fail(err)
		}
		if os.Getuid() == 0 {
			if err := os.Chown(filepath.Join(root, directory), uid, gid); err != nil {
				return fail(err)
			}
		}
	}
	if os.Getuid() == 0 {
		if err := os.Chown(filepath.Join(root, "dev/null"), uid, gid); err != nil {
			return fail(err)
		}
		// Retain root ownership of the code tree. The mapped identity receives
		// traversal, never authority to replace the staged loader or Python.
		if err := os.Chmod(root, 0755); err != nil {
			return fail(err)
		}
	}
	return root, cleanup, nil
}

// Every ELF dependency needed by the staged interpreter must be supplied by
// authenticated private bytes or the copied provided foundation. No system
// library cache, module search directory or ambient preload enters this root.
func qualifyBootstrapELF(projected *projection, requirement prerequisites.ExecutionRequirement, root string) error {
	type object struct {
		name                            string
		needed, rpath, runpath, sonames []string
	}
	decode := func(name string, data []byte) (object, bool, error) {
		result := object{name: name}
		if !bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
			return result, false, nil
		}
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return result, false, bundleFailure("bootstrap contains an invalid ELF object")
		}
		defer file.Close()
		if file.Machine != elf.EM_X86_64 || file.Class != elf.ELFCLASS64 {
			return result, false, bundleFailure("bootstrap ELF architecture is unsupported")
		}
		result.needed, err = file.ImportedLibraries()
		if err != nil {
			return result, false, err
		}
		result.sonames, err = file.DynString(elf.DT_SONAME)
		if err != nil {
			return result, false, err
		}
		result.rpath, err = file.DynString(elf.DT_RPATH)
		if err != nil {
			return result, false, err
		}
		result.runpath, err = file.DynString(elf.DT_RUNPATH)
		return result, true, err
	}
	provided := map[string]bool{}
	var objects, foundation []object
	for name, file := range projected.files {
		parsed, yes, err := decode("/"+name, file.data)
		if err != nil {
			return err
		}
		if yes {
			objects = append(objects, parsed)
		}
	}
	for _, file := range requirement.Files {
		name := file.Path
		if root != "" {
			name = filepath.Join(root, strings.TrimPrefix(name, "/"))
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return bundleFailure("provided bootstrap library changed during qualification")
		}
		parsed, yes, err := decode(file.Path, data)
		if err != nil || !yes {
			return bundleFailure("provided bootstrap object is invalid")
		}
		provided[filepath.Base(file.Path)] = true
		for _, name := range parsed.sonames {
			provided[name] = true
		}
		foundation = append(foundation, parsed)
	}
	for _, file := range foundation {
		for _, needed := range file.needed {
			if !provided[needed] {
				return bundleFailure("provided bootstrap libraries have an incomplete dependency closure")
			}
		}
	}
	private := func(name string) bool {
		if !strings.HasPrefix(name, "/python/") {
			return false
		}
		_, ok := projected.files[strings.TrimPrefix(name, "/")]
		return ok
	}
	expand := func(value, origin string) string {
		return filepath.Clean(strings.ReplaceAll(strings.ReplaceAll(value, "${ORIGIN}", origin), "$ORIGIN", origin))
	}
	for _, file := range objects {
		origin := filepath.Dir(file.name)
		for _, needed := range file.needed {
			if provided[needed] {
				continue
			}
			if strings.Contains(needed, "/") {
				if private(expand(needed, origin)) {
					continue
				}
				return bundleFailure("bootstrap ELF dependency path escapes its authenticated private projection")
			}
			found := false
			// RPATH precedes the fixed library path. An external RPATH is harmless
			// only for dependencies already explicitly preloaded above.
			if len(file.runpath) == 0 {
				for _, entry := range file.rpath {
					for _, directory := range strings.Split(entry, ":") {
						directory = expand(directory, origin)
						if !strings.HasPrefix(directory, "/python/") {
							return bundleFailure("bootstrap ELF search path could select an unqualified external dependency")
						}
						if private(filepath.Join(directory, needed)) {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			if !found && private("/python/lib/"+needed) {
				found = true
			}
			if !found {
				for _, entry := range file.runpath {
					for _, directory := range strings.Split(entry, ":") {
						directory = expand(directory, origin)
						if !strings.HasPrefix(directory, "/python/") {
							return bundleFailure("bootstrap ELF search path escapes its private projection")
						}
						if private(filepath.Join(directory, needed)) {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			if !found {
				return bundleFailure("selected Python or wheel ELF dependency is outside the authenticated bootstrap foundation")
			}
		}
	}
	return nil
}

func qualifyResolvedProjection(projected *projection, requirement prerequisites.ExecutionRequirement) error {
	return qualifyBootstrapELF(projected, requirement, "")
}

type boundedOutput struct {
	bytes.Buffer
	maximum  int
	exceeded bool
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > b.maximum-b.Len() {
		b.exceeded = true
		return 0, errors.New("bounded process output exceeded")
	}
	return b.Buffer.Write(data)
}

type publisherBroker struct {
	server      *http.Server
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	workers     sync.WaitGroup
	proxy       func(*http.Request) (*url.URL, error)
	pool        *x509.CertPool
	mu          sync.Mutex
	connections map[net.Conn]bool
	requests    int
	active      int
	closed      bool
	bytes       int64
}

func newPublisherBroker(ctx context.Context, egress prerequisites.SetupEgress, trust []byte) (*publisherBroker, error) {
	proxy, err := explicitProxy(egress)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(trust) {
		return nil, bundleFailure("resolver TLS trust is unavailable")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, bundleFailure("resolver publisher broker cannot start")
	}
	bounded, cancel := context.WithCancel(ctx)
	b := &publisherBroker{listener: listener, ctx: bounded, cancel: cancel, done: make(chan struct{}), proxy: proxy, pool: pool, connections: map[net.Conn]bool{}}
	b.server = &http.Server{Handler: b, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		defer close(b.done)
		// Listener closure is the normal owner-controlled termination path.
		_ = b.server.Serve(listener)
	}()
	return b, nil
}

func (b *publisherBroker) endpoint() string { return "http://" + b.listener.Addr().String() }
func (b *publisherBroker) close() {
	b.mu.Lock()
	b.closed = true
	b.cancel()
	for connection := range b.connections {
		connection.Close()
	}
	b.mu.Unlock()
	b.server.Close()
	<-b.done
	b.workers.Wait()
}

func (b *publisherBroker) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect || request.Host != "pypi.org:443" && request.Host != "files.pythonhosted.org:443" || request.URL.User != nil {
		http.Error(writer, "publisher route refused", http.StatusForbidden)
		return
	}
	b.mu.Lock()
	exceeded := b.closed || b.requests >= 256 || b.active >= 8
	if !exceeded {
		b.requests++
		b.active++
		b.workers.Add(1)
	}
	b.mu.Unlock()
	if exceeded {
		http.Error(writer, "publisher route limit", http.StatusTooManyRequests)
		return
	}
	defer func() {
		b.mu.Lock()
		b.active--
		b.mu.Unlock()
		b.workers.Done()
	}()
	upstream, err := b.connect(request.Host)
	if err != nil {
		http.Error(writer, "publisher connection failed", http.StatusBadGateway)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		upstream.Close()
		return
	}
	downstream, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		upstream.Close()
		downstream.Close()
		return
	}
	b.connections[upstream], b.connections[downstream] = true, true
	b.mu.Unlock()
	defer func() {
		upstream.Close()
		downstream.Close()
		b.mu.Lock()
		delete(b.connections, upstream)
		delete(b.connections, downstream)
		b.mu.Unlock()
	}()
	deadline := time.Now().Add(2 * time.Minute)
	upstream.SetDeadline(deadline)
	downstream.SetDeadline(deadline)
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if buffered.Flush() != nil {
		return
	}
	done := make(chan struct{}, 1)
	go func() {
		io.Copy(brokerBudgetWriter{b, upstream}, io.LimitReader(buffered, 128<<20))
		upstream.Close()
		done <- struct{}{}
	}()
	io.Copy(brokerBudgetWriter{b, downstream}, io.LimitReader(upstream, 128<<20))
	downstream.Close()
	<-done
}

type brokerBudgetWriter struct {
	broker *publisherBroker
	writer io.Writer
}

func (w brokerBudgetWriter) Write(data []byte) (int, error) {
	w.broker.mu.Lock()
	if int64(len(data)) > 512<<20-w.broker.bytes {
		w.broker.mu.Unlock()
		return 0, errors.New("resolver publisher transfer bound exceeded")
	}
	w.broker.bytes += int64(len(data))
	w.broker.mu.Unlock()
	return w.writer.Write(data)
}

func (b *publisherBroker) connect(authority string) (net.Conn, error) {
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: authority}}
	proxy, err := b.proxy(request)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if proxy == nil {
		return dialer.DialContext(b.ctx, "tcp", authority)
	}
	address := proxy.Host
	if proxy.Port() == "" {
		port := "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
		address = net.JoinHostPort(proxy.Hostname(), port)
	}
	connection, err := dialer.DialContext(b.ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	// Cancellation must also interrupt an upstream proxy that stalls after
	// accepting TCP, before the connection enters the tunnel registry.
	raw := connection
	stop := context.AfterFunc(b.ctx, func() { raw.Close() })
	defer stop()
	if proxy.Scheme == "https" {
		secure := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: b.pool, ServerName: proxy.Hostname()})
		if err := secure.HandshakeContext(b.ctx); err != nil {
			connection.Close()
			return nil, err
		}
		connection = secure
	}
	connection.SetDeadline(time.Now().Add(30 * time.Second))
	connectRequest := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: authority}, Host: authority, Header: make(http.Header)}
	if err := connectRequest.Write(connection); err != nil {
		connection.Close()
		return nil, err
	}
	reader := bufio.NewReader(io.LimitReader(connection, 16<<10))
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode != http.StatusOK || reader.Buffered() != 0 {
		connection.Close()
		return nil, errors.New("upstream proxy refused publisher")
	}
	return connection, nil
}
