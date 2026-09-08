package desiredstate

type SourceFile struct {
	path string
	data []byte
}

func NewSourceFile(path string, data []byte) SourceFile {
	return SourceFile{path: path, data: append([]byte(nil), data...)}
}

func (f SourceFile) Path() string { return f.path }

func (f SourceFile) Bytes() []byte { return append([]byte(nil), f.data...) }

func (f SourceFile) Size() int { return len(f.data) }

type Sources struct {
	Files   []SourceFile
	Markers []SourceFile
	Roots   []string
}

const (
	MaxPathDepth      = 32
	MaxEntries        = 65536
	MaxFiles          = 4096
	MaxMarkers        = 4096
	MaxMarkerBytes    = 64
	MaxAllMarkerBytes = 262144
	MaxFileBytes      = 2097152
	MaxAllFileBytes   = 33554432
	MaxFileDocuments  = 256
	MaxDocuments      = 8192
	MaxDepth          = 64
	MaxDocumentNodes  = 100000
	MaxNodes          = 1000000
	MaxDiagnostics    = 1000
)
