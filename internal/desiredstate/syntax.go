package desiredstate

type Kind uint8

const (
	DocumentKind Kind = iota + 1
	MappingKind
	SequenceKind
	ScalarKind
	AliasKind
)

type Style uint8

const (
	PlainStyle Style = iota
	DoubleQuotedStyle
	SingleQuotedStyle
	LiteralStyle
	FoldedStyle
	FlowStyle
)

type Node struct {
	Kind        Kind
	Value       string
	Tag         string
	Style       Style
	ExplicitTag bool
	Anchor      string
	Line        int
	Column      int
	Content     []*Node
}

type Document struct {
	Path  string
	Index int
	Root  *Node
}

func (d Document) IsEmpty() bool {
	if d.Root == nil || d.Root.Kind != DocumentKind || len(d.Root.Content) != 1 {
		return false
	}
	n := d.Root.Content[0]
	return n.Kind == ScalarKind && n.Tag == "!!null" && n.Value == "" &&
		n.Style == PlainStyle && !n.ExplicitTag && n.Anchor == ""
}
