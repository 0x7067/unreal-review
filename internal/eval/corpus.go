package eval

import "unreal-review/internal/findings"

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

const statsBase = `package evalstats

import "sort"

// Median returns the middle value of values.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}
`

const statsChange = `package evalstats

import "sort"

// Median returns the middle value of values.
func Median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}
`

const storeBase = `package evalstore

import "database/sql"

type User struct {
	ID   int
	Name string
}

func UserByName(db *sql.DB, name string) (*User, error) {
	row := db.QueryRow("SELECT id, name FROM users WHERE name = ?", name)
	var user User
	if err := row.Scan(&user.ID, &user.Name); err != nil {
		return nil, err
	}
	return &user, nil
}
`

const storeChange = `package evalstore

import "database/sql"

type User struct {
	ID   int
	Name string
}

func UserByName(db *sql.DB, name string) (*User, error) {
	row := db.QueryRow("SELECT id, name FROM users WHERE name = '" + name + "'")
	var user User
	if err := row.Scan(&user.ID, &user.Name); err != nil {
		return nil, err
	}
	return &user, nil
}
`

const serverBase = `package evalserver

type Server struct {
	events chan string
}

func NewServer() *Server {
	return &Server{events: make(chan string, 8)}
}

func (s *Server) Events() <-chan string {
	return s.events
}

func (s *Server) Handle(entry string) {
	select {
	case s.events <- entry:
	default:
	}
}
`

const serverChange = `package evalserver

type Server struct {
	events chan string
}

func NewServer() *Server {
	return &Server{events: make(chan string, 8)}
}

func (s *Server) Events() <-chan string {
	return s.events
}

func (s *Server) Handle(entry string) {
	go func() {
		s.events <- entry
	}()
}
`

const configsBase = `package evalconfigs

import "fmt"

type Config struct {
	Name string
}

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	return &Config{Name: path}, nil
}

func LoadAll(paths []string) ([]*Config, error) {
	configs := make([]*Config, 0, len(paths))
	for _, path := range paths {
		config, err := Load(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		configs = append(configs, config)
	}
	return configs, nil
}
`

const configsChange = `package evalconfigs

import "fmt"

type Config struct {
	Name string
}

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	return &Config{Name: path}, nil
}

func LoadAll(paths []string) ([]*Config, error) {
	configs := make([]*Config, 0, len(paths))
	for _, path := range paths {
		config, err := Load(path)
		if err != nil {
			continue
		}
		configs = append(configs, config)
	}
	return configs, nil
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
		Gold:   []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}},
	},
	{
		Name:   "nil-deref",
		Base:   map[string]string{"config.go": configBase},
		Change: map[string]string{"config.go": configChange},
		Gold:   []Gold{{Path: "config.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}},
	},
	{
		Name:   "bounds",
		Base:   map[string]string{"stats.go": statsBase},
		Change: map[string]string{"stats.go": statsChange},
		Gold:   []Gold{{Path: "stats.go", StartLine: 6, EndLine: 10, Severity: findings.SeverityError}},
	},
	{
		Name:   "sql-injection",
		Base:   map[string]string{"store.go": storeBase},
		Change: map[string]string{"store.go": storeChange},
		Gold:   []Gold{{Path: "store.go", StartLine: 10, EndLine: 17, Severity: findings.SeverityError}},
	},
	{
		Name:   "channel-leak",
		Base:   map[string]string{"server.go": serverBase},
		Change: map[string]string{"server.go": serverChange},
		Gold:   []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}},
	},
	{
		Name:   "swallowed-error",
		Base:   map[string]string{"configs.go": configsBase},
		Change: map[string]string{"configs.go": configsChange},
		Gold:   []Gold{{Path: "configs.go", StartLine: 16, EndLine: 26, Severity: findings.SeverityWarning}},
	},
	{
		Name:   "clean",
		Base:   map[string]string{"math.go": mathBase},
		Change: map[string]string{"math.go": mathChange},
	},
}
