package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	secretsFDEnv   = "UNREAL_REVIEW_SECRETS_FD"
	maxSecretBytes = 4096
)

var secretNames = []string{"OPENROUTER_API_KEY", "UNREAL_HARNESS_LLM_API_KEY", "GH_TOKEN", "GITHUB_TOKEN"}

var secrets = map[string]string{}

func secret(name string) string {
	return secrets[name]
}

func isolateSecrets() error {
	if raw, ok := os.LookupEnv(secretsFDEnv); ok {
		return receiveSecrets(raw)
	}
	held := map[string]string{}
	for _, name := range secretNames {
		if value, ok := os.LookupEnv(name); ok {
			held[name] = value
		}
	}
	if len(held) == 0 {
		return nil
	}
	return reexecWithout(held)
}

func receiveSecrets(raw string) error {
	if err := os.Unsetenv(secretsFDEnv); err != nil {
		return err
	}
	fd, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", secretsFDEnv, err)
	}
	file := os.NewFile(uintptr(fd), "secrets")
	defer func() { _ = file.Close() }()
	encoded, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read secrets: %w", err)
	}
	return json.Unmarshal(encoded, &secrets)
}

func reexecWithout(held map[string]string) error {
	encoded, err := json.Marshal(held)
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	if len(encoded) > maxSecretBytes {
		return fmt.Errorf("credentials in %s total %d bytes; at most %d fit in the startup pipe", strings.Join(secretNames, ", "), len(encoded), maxSecretBytes)
	}
	read, write, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("secrets pipe: %w", err)
	}
	if _, err := write.Write(encoded); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	if err := write.Close(); err != nil {
		return fmt.Errorf("close secrets pipe: %w", err)
	}
	fd := int(read.Fd())
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		return fmt.Errorf("pass secrets pipe: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	env := []string{secretsFDEnv + "=" + strconv.Itoa(fd)}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, isSecret := held[name]; !isSecret {
			env = append(env, entry)
		}
	}
	return syscall.Exec(self, os.Args, env)
}
