package abi

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
)

const (
	modeArgs  uint8 = iota // geth's mapArgNamesToStructFields (abi tags, then camel case)
	modeTopic              // geth's ParseTopics: FieldByName(ToCamelCase(name))

	maxPlansPerType = 8
)

// fieldPlan caches, for one struct type and one list of ABI names, the index path of
// the struct field each name maps to (nil when unmapped).
type fieldPlan struct {
	mode  uint8
	names []string
	index [][]int
}

// fieldPlans maps reflect.Type to []*fieldPlan (copy-on-write).
var fieldPlans sync.Map

// structFields returns the field plan of rt for the n names produced by name. It
// allocates only on the first use of a (type, names) pair.
func structFields(rt reflect.Type, mode uint8, n int, name func(int) string) (*fieldPlan, error) {
	var plans []*fieldPlan
	if v, ok := fieldPlans.Load(rt); ok {
		plans = v.([]*fieldPlan)
	next:
		for _, p := range plans {
			if p.mode != mode || len(p.names) != n {
				continue
			}
			for i, s := range p.names {
				if s != name(i) {
					continue next
				}
			}
			return p, nil
		}
	}

	names := make([]string, n)
	for i := range names {
		names[i] = name(i)
	}
	p := &fieldPlan{mode: mode, names: names, index: make([][]int, n)}
	if mode == modeTopic {
		for i, s := range names {
			if f, ok := rt.FieldByName(gethabi.ToCamelCase(s)); ok {
				p.index[i] = f.Index
			}
		}
	} else {
		abi2struct, err := mapArgNamesToStructFields(names, rt)
		if err != nil {
			return nil, err
		}
		for i, s := range names {
			if f, ok := rt.FieldByName(abi2struct[s]); ok && abi2struct[s] != "" {
				p.index[i] = f.Index
			}
		}
	}
	if len(plans) >= maxPlansPerType {
		plans = nil
	}
	fieldPlans.Store(rt, append(plans[:len(plans):len(plans)], p))
	return p, nil
}

// field returns the i-th planned field of struct v, or an invalid Value if unmapped.
func (p *fieldPlan) field(v reflect.Value, i int) (reflect.Value, error) {
	if p.index[i] == nil {
		return reflect.Value{}, nil
	}
	return v.FieldByIndexErr(p.index[i])
}

// mapArgNamesToStructFields is geth's function of the same name, on a reflect.Type.
func mapArgNamesToStructFields(argNames []string, typ reflect.Type) (map[string]string, error) {
	abi2struct := make(map[string]string)
	struct2abi := make(map[string]string)

	// first round: exported fields with an abi:"" tag
	for i := 0; i < typ.NumField(); i++ {
		structFieldName := typ.Field(i).Name
		if structFieldName[:1] != strings.ToUpper(structFieldName[:1]) {
			continue
		}
		tagName, ok := typ.Field(i).Tag.Lookup("abi")
		if !ok {
			continue
		}
		if tagName == "" {
			return nil, fmt.Errorf("struct: abi tag in '%s' is empty", structFieldName)
		}
		found := false
		for _, arg := range argNames {
			if arg == tagName {
				if abi2struct[arg] != "" {
					return nil, fmt.Errorf("struct: abi tag in '%s' already mapped", structFieldName)
				}
				abi2struct[arg] = structFieldName
				struct2abi[structFieldName] = arg
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("struct: abi tag '%s' defined but not found in abi", tagName)
		}
	}

	// second round: camel-cased argument names
	for _, argName := range argNames {
		structFieldName := gethabi.ToCamelCase(argName)
		if structFieldName == "" {
			return nil, errors.New("abi: purely underscored output cannot unpack to struct")
		}
		if abi2struct[argName] != "" {
			if abi2struct[argName] != structFieldName && struct2abi[structFieldName] == "" {
				if _, ok := typ.FieldByName(structFieldName); ok {
					return nil, fmt.Errorf("abi: multiple variables maps to the same abi field '%s'", argName)
				}
			}
			continue
		}
		if struct2abi[structFieldName] != "" {
			return nil, fmt.Errorf("abi: multiple outputs mapping to the same struct field '%s'", structFieldName)
		}
		if _, ok := typ.FieldByName(structFieldName); ok {
			abi2struct[argName] = structFieldName
		}
		struct2abi[structFieldName] = argName
	}
	return abi2struct, nil
}
