package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
)

const (
	authorized_keys = "$HOME/.ssh/authorized_keys"
	kickstart       = "/System/Library/CoreServices/RemoteManagement/ARDAgent.app/Contents/Resources/kickstart"
	zipFile         = "ngrok.zip"
	dir             = "/usr/local/bin"
	ngrokFile       = "/tmp/ngrok-config.yml"
)

const ngrokConfig = `authtoken: $AUTHTOKEN
tunnels:
  ssh:
    addr: 22
    proto: tcp
  vnc:
    addr: 5900
    proto: tcp
`

func fail(format string, v ...interface{}) {
	log.Errorf(format, v...)
	os.Exit(1)
}

func AddAuthorizedKey(sshKey string) error {
	f, err := os.OpenFile(os.ExpandEnv(authorized_keys), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("Can't open file (%s), error: %v", authorized_keys, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
	}()

	if _, err = f.WriteString(fmt.Sprintf("\n%s\n", sshKey)); err != nil {
		return fmt.Errorf("Can't write SSH Public Key, error: %v", err)
	}
	return err
}

func EnableRemoteDesktop(password string) error {
	args := []string{kickstart, "-activate", "-configure", "-access", "-on", "-clientopts", "-setvnclegacy", "-vnclegacy", "yes", "-clientopts", "-setvncpw", "-vncpw", password, "-restart", "-agent", "-privs", "-all"}
	cmd := command.New("sudo", args...)
	log.Infof("\n$ %s\n", cmd.PrintableCommandArgs())
	return cmd.Run()
}

func downloadFromUrl(url string) error {
	output, err := os.Create(zipFile)
	if err != nil {
		return fmt.Errorf("Error while creating file (%s), error: %v\n", zipFile, err)
	}
	defer output.Close()

	response, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("Error while downloading url (%s), error: %v\n", url, err)
	}
	defer response.Body.Close()

	_, err = io.Copy(output, response.Body)
	if err != nil {
		return fmt.Errorf("Error while downloading url (%s), error: %v\n", url, err)
	}
	return nil
}

func createNgrokConf(authToken string) error {
	c, err := os.Create(ngrokFile)
	if err != nil {
		return fmt.Errorf("Error while creating file (%s), error: %v\n", ngrokFile, err)
	}
	defer c.Close()

	_, err = io.WriteString(c, strings.Replace(ngrokConfig, "$AUTHTOKEN", authToken, -1))
	if err != nil {
		return fmt.Errorf("Error while write to file (%s), error: %v\n", ngrokFile, err)
	}
	return nil
}

func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

func startNgrok() error {
	cmd := command.New("ngrok", "start", "--all", "--config", ngrokFile)
	log.Infof("\n$ %s\n", cmd.PrintableCommandArgs())
	return cmd.Run()
}

func main() {
	configs := createConfigsModelFromEnvs()
	configs.print()
	if err := configs.validate(); err != nil {
		fail("Issue with input: %v", err)
	}

	log.Printf("\nAdd authorized key...")
	if err := AddAuthorizedKey(configs.SSHPublicKey); err != nil {
		fail("Can't add authorized key, error: %v\n", err)
	}

	log.Printf("Enable remote desktop...")
	if err := EnableRemoteDesktop(configs.ScreenSharePW); err != nil {
		fail("Can't enable remote desktop, error: %v", err)
	}

	log.Printf("Unzip %s to %s", zipFile, dir)
	if err := Unzip(zipFile, dir); err != nil {
		fail("Error while unzip file (%s), error: %v\n", zipFile, err)
	}

	log.Printf("Creating Ngrok config to %s", ngrokFile)
	if err := createNgrokConf(configs.AuthToken); err != nil {
		fail("Failed to create Ngrok config, error: %v", err)
	}

	log.Printf("Starting Ngrok...")
	if err := startNgrok(); err != nil {
		fail("Failed to start Ngrok, error: %v", err)
	}

	log.Donef("\nSuccess")
}
