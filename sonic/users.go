package sonic

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const humanUID = 1000

func Users() ([]string, error) {
	return users(0)
}

// HumanUsers returns the users which are not system ones.
func HumanUsers() ([]string, error) {
	return users(humanUID)
}

func users(minUID int) ([]string, error) {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	names := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 3 {
			continue
		}

		name := fields[0]
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("failed to parse UID of %s: %w", name, err)
		}

		if uid < minUID || name == "nobody" {
			continue
		}
		names = append(names, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read /etc/passwd: %w", err)
	}

	return names, nil
}

type UserParameters struct {
	Password   string
	PublicKeys []string
	Groups     []string
}

func ConfigureUser(name string, params UserParameters) error {
	if params.Password == "" && len(params.PublicKeys) == 0 {
		return fmt.Errorf("user '%s' must have either a password or a SSH key", name)
	}

	args := []string{"-m", "-s", "/bin/bash"}
	if len(params.Groups) > 0 {
		args = append(args, "-G", strings.Join(params.Groups, ","))
	}
	args = append(args, name)

	if _, err := run("useradd", args...); err != nil {
		return err
	}

	if params.Password != "" {
		if err := setPassword(name, params.Password); err != nil {
			return err
		}
	}

	if len(params.PublicKeys) > 0 {
		if err := setPublicKeys(name, params.PublicKeys); err != nil {
			return err
		}
	}

	return nil
}

func RemoveUser(name string) error {
	_, err := run("userdel", "-r", name)
	return err
}

func setPassword(name, password string) error {
	hash, err := run("openssl", "passwd", "-1", password)
	if err != nil {
		return err
	}

	cmd := exec.Command("chpasswd", "-e")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", name, strings.TrimSpace(hash)))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chpasswd failed: %w", err)
	}

	return nil
}

func setPublicKeys(name string, keys []string) error {
	account, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("user '%s' not found: %w", name, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return fmt.Errorf("invalid UID of '%s': %w", name, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return fmt.Errorf("invalid GID of '%s': %w", name, err)
	}

	sshDir := filepath.Join(account.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", sshDir, err)
	}

	authorizedKeys := filepath.Join(sshDir, "authorized_keys")
	content := strings.Join(keys, "\n") + "\n"
	if err := os.WriteFile(authorizedKeys, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", authorizedKeys, err)
	}

	for _, path := range []string{sshDir, authorizedKeys} {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("failed to set owner of %s: %w", path, err)
		}
	}

	return nil
}
