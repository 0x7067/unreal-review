package main

type entry struct {
	name  string
	value int
}

var table = map[string]entry{
	"alpha": {name: "alpha", value: 1},
	"beta":  {name: "beta", value: 2},
}

func lookup(name string) *entry {
	e, ok := table[name]
	if !ok {
		return &entry{name: name}
	}
	return &e
}
