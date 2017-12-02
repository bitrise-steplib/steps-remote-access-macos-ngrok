package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"time"

	"github.com/bitrise-io/go-utils/colorstring"

	"github.com/bitrise-io/go-utils/fileutil"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/pkg/errors"
)

const (
	authorizedKeysFilePath = "$HOME/.ssh/authorized_keys"
	kickstart              = "/System/Library/CoreServices/RemoteManagement/ARDAgent.app/Contents/Resources/kickstart"
	zipFile                = "ngrok.zip"
	dir                    = "/usr/local/bin"
	ngrokFile              = "/tmp/ngrok-config.yml"
)

var (
	isDebugMode = false
)

// NgrokTunnelConfig ...
type NgrokTunnelConfig struct {
	Addr  int    `json:"addr,omitempty"`
	Proto string `json:"proto,omitempty"`
}

// NgrokConfig ...
type NgrokConfig struct {
	Authtoken string                       `json:"authtoken,omitempty"`
	Tunnels   map[string]NgrokTunnelConfig `json:"tunnels,omitempty"`
}

// AddAuthorizedKey ...
func AddAuthorizedKey(sshKey string) error {
	f, err := os.OpenFile(os.ExpandEnv(authorizedKeysFilePath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("Can't open file (%s), error: %v", authorizedKeysFilePath, err)
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

// EnableRemoteDesktop ...
func EnableRemoteDesktop(password string) error {
	args := []string{kickstart, "-activate", "-configure", "-access", "-on", "-clientopts", "-setvnclegacy", "-vnclegacy", "yes", "-clientopts", "-setvncpw", "-vncpw", password, "-restart", "-agent", "-privs", "-all"}
	cmd := command.New("sudo", args...)
	if isDebugMode {
		log.Infof("\n$ %s\n", cmd.PrintableCommandArgs())
	}
	return cmd.Run()
}

// ChangeUserPassword ...
func ChangeUserPassword(changePasswordTo string) error {
	user, err := user.Current()
	if err != nil {
		return errors.WithStack(err)
	}

	log.Printf(" (!) Changing password of user: %s", user.Username)

	cmd := command.New("sudo", "dscl", ".", "-passwd", "/Users/"+user.Username, changePasswordTo)
	if isDebugMode {
		log.Infof("\n$ %s\n", cmd.PrintableCommandArgs())
	}
	return cmd.Run()
}

func createNgrokConf(authToken string) error {
	ngrokConfig := NgrokConfig{
		Authtoken: authToken,
		Tunnels: map[string]NgrokTunnelConfig{
			"ssh": NgrokTunnelConfig{
				Addr:  22,
				Proto: "tcp",
			},
			"vnc": NgrokTunnelConfig{
				Addr:  5900,
				Proto: "tcp",
			},
		},
	}

	ngrokConfigBytes, err := json.Marshal(ngrokConfig)
	if err != nil {
		errors.WithStack(err)
	}

	return errors.WithStack(fileutil.WriteBytesToFile(ngrokFile, ngrokConfigBytes))
}

func startNgrokAsync() error {
	cmd := command.New("ngrok", "start", "--all", "--config", ngrokFile)
	log.Infof("\n$ %s\n", cmd.PrintableCommandArgs())
	return cmd.GetCmd().Start()
}

func fetchAndPrintAcessInfosFromNgrok() error {
	// fetch ngrok tunnel infos via its localhost api
	client := &http.Client{Timeout: 10 * time.Second}
	r, err := client.Get("http://localhost:4040/api/tunnels")
	if err != nil {
		return errors.WithStack(err)
	}
	defer r.Body.Close()

	ngrokTunnels := struct {
		Tunnels []struct {
			Name      string `json:"name"`
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}{}

	if err := json.NewDecoder(r.Body).Decode(&ngrokTunnels); err != nil {
		return errors.WithStack(err)
	}

	var sshURL, vncURL *url.URL
	for _, aTunnel := range ngrokTunnels.Tunnels {
		switch aTunnel.Name {
		case "ssh":
			u, err := url.Parse(aTunnel.PublicURL)
			if err != nil {
				return errors.WithStack(err)
			}
			sshURL = u
		case "vnc":
			u, err := url.Parse(aTunnel.PublicURL)
			if err != nil {
				return errors.WithStack(err)
			}
			vncURL = u
		default:
			return errors.Errorf("Unexpected tunnel found: %+v", aTunnel)
		}
	}

	user, err := user.Current()
	if err != nil {
		return errors.WithStack(err)
	}
	currentUserUsername := user.Username

	fmt.Println()
	fmt.Println("--- Remote Access configs ---")
	fmt.Println("Remote Access is now configured and enabled. ")

	fmt.Println()
	fmt.Println("SSH:")
	fmt.Println("To SSH into this host:")
	fmt.Println(" * First ensure that the SSH key you specified is activated (e.g. run: `ssh-add -D && ssh-add /path/to/ssh/private-key`")
	fmt.Printf(" * Then ssh with: `ssh %s@%s -p %s`\n", currentUserUsername, sshURL.Hostname(), sshURL.Port())

	fmt.Println()
	fmt.Println("VNC (Screen Sharing):")
	fmt.Println("To VNC / Screen Share / Remote Desktop into this host run the following command in your Terminal:")
	fmt.Printf("    open vnc://%s@%s:%s\n", currentUserUsername, vncURL.Hostname(), vncURL.Port())
	fmt.Println(colorstring.Yellow("Note: the password for the login is the password you specified for this step!"))
	fmt.Println()
	fmt.Println("------------------------------")
	fmt.Println()

	return nil
}

func doMain() error {
	configs := createConfigsModelFromEnvs()
	configs.print()
	if err := configs.validate(); err != nil {
		return errors.Wrap(err, "Issue with input")
	}
	isDebugMode = configs.IsStepDebugMode

	log.Printf("Add authorized key...")
	if err := AddAuthorizedKey(configs.SSHPublicKey); err != nil {
		return errors.Wrap(err, "Can't add authorized key")
	}

	log.Printf("Change user password...")
	if err := ChangeUserPassword(configs.PasswordToSet); err != nil {
		return errors.Wrap(err, "Can't change user password")
	}

	log.Printf("Enable remote desktop...")
	if err := EnableRemoteDesktop(configs.PasswordToSet); err != nil {
		return errors.Wrap(err, "Can't enable remote desktop")
	}

	log.Printf("Creating Ngrok config to %s", ngrokFile)
	if err := createNgrokConf(configs.NgrokAuthToken); err != nil {
		return errors.Wrap(err, "Failed to create Ngrok config")
	}

	log.Printf("Starting Ngrok...")
	if err := startNgrokAsync(); err != nil {
		return errors.Wrap(err, "Failed to start Ngrok")
	}
	time.Sleep(5 * time.Second)

	if err := fetchAndPrintAcessInfosFromNgrok(); err != nil {
		return errors.Wrap(err, "Failed to fetch access infos from ngrok")
	}

	// wait forever
	fmt.Println()
	fmt.Println("You can now connect, keeping the connection open ...")
	for {
		time.Sleep(10 * time.Second)
		fmt.Print(".")
	}

	return nil
}

func main() {
	if err := doMain(); err != nil {
		log.Errorf("ERROR: %+v", err)
		os.Exit(1)
	}
	log.Donef("\nSuccess")
}
