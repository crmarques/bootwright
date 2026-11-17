package v1alpha1

type ContextName string

func ParseContextName(name string) (ContextName, bool) {
	return ContextName(name), ValidLexical(LexicalName, name)
}
