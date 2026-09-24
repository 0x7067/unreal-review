package eval

const cacheBase = `package evalrace

import "sync"

type Cache struct {
	mu     sync.Mutex
	values map[string]int
}

func NewCache() *Cache {
	return &Cache{values: make(map[string]int)}
}

func (c *Cache) Set(key string, value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *Cache) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[key]
	return value, ok
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.values)
}
`

const cacheChange = `package evalrace

import "sync"

type Cache struct {
	mu     sync.Mutex
	values map[string]int
}

func NewCache() *Cache {
	return &Cache{values: make(map[string]int)}
}

func (c *Cache) Set(key string, value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *Cache) Get(key string) (int, bool) {
	value, ok := c.values[key]
	return value, ok
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.values)
}
`

const configBase = `package evalconfig

import (
	"encoding/json"
	"net/http"
)

type Config struct {
	Server string
}

func Load(r *http.Request) (*Config, error) {
	var config Config
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func ServerName(r *http.Request) string {
	config, err := Load(r)
	if err != nil {
		return "unknown"
	}
	return config.Server
}
`

const configChange = `package evalconfig

import (
	"encoding/json"
	"net/http"
)

type Config struct {
	Server string
}

func Load(r *http.Request) (*Config, error) {
	var config Config
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func ServerName(r *http.Request) string {
	config, _ := Load(r)
	return config.Server
}
`

const mathBase = `package evalmath

// Total returns the sum of values.
func Total(values []int) int {
	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum
}

func Average(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return Total(values) / len(values)
}
`

const mathChange = `package evalmath

// Total returns the sum of values.
func Total(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

func Average(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return Total(values) / len(values)
}
`

var Corpus = []Case{
	{
		Name:   "race",
		Base:   map[string]string{"cache.go": cacheBase},
		Change: map[string]string{"cache.go": cacheChange},
		Gold:   []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}},
	},
	{
		Name:   "nil-deref",
		Base:   map[string]string{"config.go": configBase},
		Change: map[string]string{"config.go": configChange},
		Gold:   []Gold{{Path: "config.go", StartLine: 20, EndLine: 23}},
	},
	{
		Name:   "clean",
		Base:   map[string]string{"math.go": mathBase},
		Change: map[string]string{"math.go": mathChange},
	},
}
