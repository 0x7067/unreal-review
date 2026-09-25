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

const ledgerBase = `package evalledger

import "sync"

type Ledger struct {
	mu      sync.Mutex
	entries map[string]int
}

func NewLedger() *Ledger {
	return &Ledger{entries: make(map[string]int)}
}

func (l *Ledger) Record(key string, amount int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[key] += amount
}

func (l *Ledger) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sumLocked()
}

func (l *Ledger) sumLocked() int {
	total := 0
	for _, v := range l.entries {
		total += v
	}
	return total
}
`

const ledgerChange = `package evalledger

import "sync"

type Ledger struct {
	mu      sync.Mutex
	entries map[string]int
}

func NewLedger() *Ledger {
	return &Ledger{entries: make(map[string]int)}
}

func (l *Ledger) Record(key string, amount int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[key] += amount
}

func (l *Ledger) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Sum()
}

func (l *Ledger) Sum() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, v := range l.entries {
		total += v
	}
	return total
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

const usersBase = `package evalusers

import (
	"errors"
	"fmt"
)

var ErrUnknownUser = errors.New("unknown user")

func Lookup(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("lookup %q: %w", id, ErrUnknownUser)
	}
	return id + "@example.com", nil
}

func DisplayName(id string) string {
	name, err := Lookup(id)
	if errors.Is(err, ErrUnknownUser) {
		return "guest"
	}
	if err != nil {
		return "unknown"
	}
	return name
}
`

const usersChange = `package evalusers

import (
	"errors"
	"fmt"
)

var ErrUnknownUser = errors.New("unknown user")

func Lookup(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("lookup %q: %v", id, ErrUnknownUser)
	}
	return id + "@example.com", nil
}

func DisplayName(id string) string {
	name, err := Lookup(id)
	if errors.Is(err, ErrUnknownUser) {
		return "guest"
	}
	if err != nil {
		return "unknown"
	}
	return name
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

const fetchBase = `package evalfetch

import (
	"io"
	"net/http"
)

func Head(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
`

const fetchChange = `package evalfetch

import (
	"io"
	"net/http"
)

func Head(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
`

const poolBase = `package evalpool

import (
	"net"
	"time"
)

func Dial(addr string) (*Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: conn}, nil
}

type Conn struct {
	conn net.Conn
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func Shared(addr string) (*Conn, error) {
	return Dial(addr)
}
`

const poolChange = `package evalpool

import (
	"net"
	"sync"
	"time"
)

func Dial(addr string) (*Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: conn}, nil
}

type Conn struct {
	conn net.Conn
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

var (
	shared     *Conn
	sharedErr  error
	sharedOnce sync.Once
)

func Shared(addr string) (*Conn, error) {
	sharedOnce.Do(func() {
		shared, sharedErr = Dial(addr)
	})
	return shared, sharedErr
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
		Class:  "concurrency",
		Base:   map[string]string{"cache.go": cacheBase},
		Change: map[string]string{"cache.go": cacheChange},
		Gold:   []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}},
	},
	{
		Name:   "deadlock",
		Class:  "concurrency",
		Base:   map[string]string{"ledger.go": ledgerBase},
		Change: map[string]string{"ledger.go": ledgerChange},
		Gold:   []Gold{{Path: "ledger.go", StartLine: 20, EndLine: 34, Severity: findings.SeverityError}},
	},
	{
		Name:   "nil-deref",
		Class:  "error-handling",
		Base:   map[string]string{"config.go": configBase},
		Change: map[string]string{"config.go": configChange},
		Gold:   []Gold{{Path: "config.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}},
	},
	{
		Name:   "swallowed-error",
		Class:  "error-handling",
		Base:   map[string]string{"configs.go": configsBase},
		Change: map[string]string{"configs.go": configsChange},
		Gold:   []Gold{{Path: "configs.go", StartLine: 16, EndLine: 26, Severity: findings.SeverityWarning}},
	},
	{
		Name:   "wrap-break",
		Class:  "error-handling",
		Base:   map[string]string{"users.go": usersBase},
		Change: map[string]string{"users.go": usersChange},
		Gold:   []Gold{{Path: "users.go", StartLine: 8, EndLine: 26, Severity: findings.SeverityError}},
	},
	{
		Name:   "bounds",
		Class:  "memory-safety",
		Base:   map[string]string{"stats.go": statsBase},
		Change: map[string]string{"stats.go": statsChange},
		Gold:   []Gold{{Path: "stats.go", StartLine: 6, EndLine: 10, Severity: findings.SeverityError}},
	},
	{
		Name:   "sql-injection",
		Class:  "security",
		Base:   map[string]string{"store.go": storeBase},
		Change: map[string]string{"store.go": storeChange},
		Gold:   []Gold{{Path: "store.go", StartLine: 10, EndLine: 17, Severity: findings.SeverityError}},
	},
	{
		Name:   "channel-leak",
		Class:  "resources",
		Base:   map[string]string{"server.go": serverBase},
		Change: map[string]string{"server.go": serverChange},
		Gold:   []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}},
	},
	{
		Name:   "body-leak",
		Class:  "api-contract",
		Base:   map[string]string{"fetch.go": fetchBase},
		Change: map[string]string{"fetch.go": fetchChange},
		Gold:   []Gold{{Path: "fetch.go", StartLine: 8, EndLine: 18, Severity: findings.SeverityWarning}},
	},
	{
		Name:   "once-failure",
		Class:  "api-contract",
		Base:   map[string]string{"pool.go": poolBase},
		Change: map[string]string{"pool.go": poolChange},
		Gold:   []Gold{{Path: "pool.go", StartLine: 25, EndLine: 36, Severity: findings.SeverityError}},
	},
	{
		Name:   "clean",
		Class:  "control",
		Base:   map[string]string{"math.go": mathBase},
		Change: map[string]string{"math.go": mathChange},
	},
}
