package v1alpha1

import (
	"slices"
	"testing"
)

func unregisteredLexicalRules(shape *Shape, path string, seen map[*Shape]bool) []string {
	if shape == nil || seen[shape] {
		return nil
	}
	seen[shape] = true
	var found []string
	if _, registered := lexicalRules[shape.Rule]; shape.Rule != "" && !registered {
		found = append(found, path+": "+string(shape.Rule))
	}
	for _, field := range shape.Fields {
		found = append(found, unregisteredLexicalRules(field.Shape, path+"."+field.Name, seen)...)
	}
	found = append(found, unregisteredLexicalRules(shape.Element, path+"[]", seen)...)
	for _, alternative := range shape.Alternatives {
		found = append(found, unregisteredLexicalRules(alternative, path+"|", seen)...)
	}
	return found
}

func TestEverySchemaShapeRuleIsRegistered(t *testing.T) {
	walked := 0
	for _, kind := range Kinds() {
		shape := Schema(kind)
		if shape == nil {
			continue
		}
		walked++
		for _, finding := range unregisteredLexicalRules(shape, string(kind), map[*Shape]bool{}) {
			t.Errorf("schema rule is not in the lexical registry: %s", finding)
		}
	}
	if walked == 0 {
		t.Fatal("walked no schema")
	}
}

func TestTheSchemaRuleWalkFindsAPlantedUnregisteredRule(t *testing.T) {
	planted := &Shape{Type: Mapping, Fields: []Field{
		{Name: "known", Shape: lexical(LexicalName)},
		{Name: "list", Shape: &Shape{Type: Sequence, Element: &Shape{Alternatives: []*Shape{lexical(LexicalDNS), lexical("nmae")}}}},
	}}
	found := unregisteredLexicalRules(planted, "Planted", map[*Shape]bool{})
	if !slices.Equal(found, []string{"Planted.list[]|: nmae"}) {
		t.Fatalf("found %q, want the one planted rule", found)
	}
}
