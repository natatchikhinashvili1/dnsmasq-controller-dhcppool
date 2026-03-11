package util

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func WriteConfig(orig, dest string, data []byte) (bool, error) {
	// If file exists check hash
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		hasher := sha256.New()
		f, err := os.Open(orig)
		if err != nil {
			return false, err
		}
		defer f.Close()
		if _, err := io.Copy(hasher, f); err != nil {
			return false, err
		}
		oldHash := hasher.Sum(nil)

		hasher = sha256.New()
		hasher.Write(data)
		newHash := hasher.Sum(nil)

		if bytes.Equal(oldHash, newHash) {
			return false, nil
		}
		f.Close()
	}

	err := os.WriteFile(dest, data, 0644)
	if err != nil {
		return false, err
	}
	return true, nil
}

func TestConfig(f string) error {
	var stderr bytes.Buffer
	dnsmasqBinary, err := exec.LookPath("dnsmasq")
	if err != nil {
		return err
	}
	cmd := &exec.Cmd{
		Path:   dnsmasqBinary,
		Args:   []string{"dnsmasq", "--test", "--conf-file=" + f},
		Stderr: &stderr,
	}
	err = cmd.Run()
	if err != nil {
		err = fmt.Errorf("%s", stderr.Bytes())
	}
	return err
}
