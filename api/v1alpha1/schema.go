package v1alpha1

type Field struct {
	Name     string
	Shape    *Shape
	Required bool
	Default  Value
}

type Shape struct {
	Type          ValueType
	Fields        []Field
	Element       *Shape
	Alternatives  []*Shape
	Open          bool
	KindDefaults  bool
	Atomic        bool
	Rule          string
	Enums         []string
	Minimum       string
	Maximum       string
	MinLength     int
	Unique        bool
	NameKey       string
	Reference     []Kind
	SecretTypes   []string
	Discriminator string
	Arms          []string
	AllowEmpty    bool
	InertArms     []string
	ArmValues     map[string][]string
	Suppress      []Suppression
}

type Suppression struct {
	Field  string
	Value  Value
	Fields []string
}

func (s *Shape) Field(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func text() *Shape                        { return &Shape{Type: String} }
func nonempty() *Shape                    { return &Shape{Type: String, MinLength: 1} }
func boolean() *Shape                     { return &Shape{Type: Boolean} }
func integer(min, max string) *Shape      { return &Shape{Type: Integer, Minimum: min, Maximum: max} }
func number(min, max string) *Shape       { return &Shape{Type: Number, Minimum: min, Maximum: max} }
func enumeration(values ...string) *Shape { return &Shape{Type: String, Enums: values} }
func record(fields ...Field) *Shape       { return &Shape{Type: Mapping, Fields: fields} }
func array(element *Shape) *Shape         { return &Shape{Type: Sequence, Element: element, Atomic: true} }
func set(element *Shape) *Shape {
	return &Shape{Type: Sequence, Element: element, Unique: true, Atomic: true}
}
func named(element *Shape) *Shape {
	return &Shape{Type: Sequence, Element: element, NameKey: "name", Atomic: true}
}
func native() *Shape                       { return &Shape{Type: Mapping, Open: true, Atomic: true} }
func stringsMap() *Shape                   { return &Shape{Type: Mapping, Open: true, Element: text(), Atomic: true} }
func field(name string, s *Shape) Field    { return Field{Name: name, Shape: s} }
func required(name string, s *Shape) Field { return Field{Name: name, Shape: s, Required: true} }
func defaulted(name string, s *Shape, value Value) Field {
	return Field{Name: name, Shape: s, Default: value}
}
func ref(kind ...Kind) *Shape { return &Shape{Type: String, MinLength: 1, Reference: kind} }
func secret(types ...string) *Shape {
	return &Shape{Type: String, MinLength: 1, Reference: []Kind{Secret}, SecretTypes: types}
}
func choice(arms ...Field) *Shape {
	s := record(arms...)
	for _, f := range arms {
		s.Arms = append(s.Arms, f.Name)
	}
	return s
}
func atomic(s *Shape) *Shape        { s.Atomic = true; return s }
func nonemptyArray(s *Shape) *Shape { s.MinLength = 1; return s }
func lexical(rule string) *Shape    { return &Shape{Type: String, Rule: rule, MinLength: 1} }
func dns() *Shape                   { return lexical("dns") }
func name() *Shape                  { return lexical("name") }
func ip() *Shape                    { return lexical("ip") }
func cidr() *Shape                  { return lexical("cidr") }
func url() *Shape                   { return lexical("http-url") }
func duration() *Shape              { return lexical("duration") }
func port() *Shape                  { return integer("1", "65535") }
func image() *Shape                 { return lexical("image") }

var schemas = map[Kind]func() *Shape{
	Environment: environmentSchema,
	Secret:      secretSchema,
	Entitlement: entitlementSchema,
}

func Schema(kind Kind) *Shape {
	if factory, ok := schemas[kind]; ok {
		return factory()
	}
	return nil
}

func register(kind Kind, factory func() *Shape) {
	if _, exists := schemas[kind]; exists {
		panic("duplicate API schema registration")
	}
	schemas[kind] = factory
}
