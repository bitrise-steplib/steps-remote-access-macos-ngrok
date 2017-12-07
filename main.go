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
	"github.com/bitrise-io/go-utils/retry"

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

func createNgrokConf(authToken string, isSSH, isVNC bool) error {
	tunnels := map[string]NgrokTunnelConfig{}
	if isSSH {
		tunnels["ssh"] = NgrokTunnelConfig{
			Addr:  22,
			Proto: "tcp",
		}
	}
	if isVNC {
		tunnels["vnc"] = NgrokTunnelConfig{
			Addr:  5900,
			Proto: "tcp",
		}
	}

	ngrokConfig := NgrokConfig{
		Authtoken: authToken,
		Tunnels:   tunnels,
	}

	ngrokConfigBytes, err := json.Marshal(ngrokConfig)
	if err != nil {
		errors.WithStack(err)
	}

	if isDebugMode {
		log.Warnf("ngrok config: %s", ngrokConfigBytes)
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
	var resp *http.Response
	err := retry.Times(3).Wait(5 * time.Second).Try(func(attempt uint) error {
		if attempt != 0 {
			if isDebugMode {
				log.Warnf("Attempt %d failed, retrying ...", attempt)
			}
		}
		var err error
		resp, err = client.Get("http://localhost:4040/api/tunnels")
		return errors.WithStack(err)
	})
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	ngrokTunnels := struct {
		Tunnels []struct {
			Name      string `json:"name"`
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&ngrokTunnels); err != nil {
		return errors.WithStack(err)
	}

	user, err := user.Current()
	if err != nil {
		return errors.WithStack(err)
	}
	currentUserUsername := user.Username

	fmt.Println()
	fmt.Println("--- Remote Access configs ---")
	fmt.Println("Remote Access is now configured and enabled. ")

	for _, aTunnel := range ngrokTunnels.Tunnels {
		switch aTunnel.Name {
		case "ssh":
			sshURL, err := url.Parse(aTunnel.PublicURL)
			if err != nil {
				return errors.WithStack(err)
			}

			fmt.Println()
			fmt.Println("SSH:")
			fmt.Println("To SSH into this host:")
			fmt.Println(" * First ensure that the SSH key you specified is activated (e.g. run: `ssh-add -D && ssh-add /path/to/ssh/private-key`")
			fmt.Printf(" * Then ssh with: `ssh %s@%s -p %s`\n", currentUserUsername, sshURL.Hostname(), sshURL.Port())
		case "vnc":
			vncURL, err := url.Parse(aTunnel.PublicURL)
			if err != nil {
				return errors.WithStack(err)
			}
			fmt.Println()
			fmt.Println("VNC (Screen Sharing):")
			fmt.Println("To VNC / Screen Share / Remote Desktop into this host run the following command in your Terminal:")
			fmt.Printf("    open vnc://%s@%s:%s\n", currentUserUsername, vncURL.Hostname(), vncURL.Port())
			fmt.Println(colorstring.Yellow("Note: the password for the login is the password you specified for this step!"))
		default:
			return errors.Errorf("Unexpected tunnel found: %+v", aTunnel)
		}
	}

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

	if configs.SSHPublicKey != "" {
		log.Printf("Add authorized key...")
		if err := AddAuthorizedKey(configs.SSHPublicKey); err != nil {
			return errors.Wrap(err, "Can't add authorized key")
		}
	} else {
		log.Warnf("No SSH public key specified, skipping SSH setup.")
	}

	if configs.PasswordToSet != "" {
		log.Printf("Change user password...")
		if err := ChangeUserPassword(configs.PasswordToSet); err != nil {
			return errors.Wrap(err, "Can't change user password")
		}

		log.Printf("Enable remote desktop...")
		if err := EnableRemoteDesktop(configs.PasswordToSet); err != nil {
			return errors.Wrap(err, "Can't enable remote desktop")
		}
	} else {
		log.Warnf("No (User & VNC) Password specified, skipping Remote Desktop / Screen Sharing setup.")
	}

	log.Printf("Creating Ngrok config to %s", ngrokFile)
	if err := createNgrokConf(configs.NgrokAuthToken, configs.SSHPublicKey != "", configs.PasswordToSet != ""); err != nil {
		return errors.Wrap(err, "Failed to create Ngrok config")
	}

	log.Printf("Starting Ngrok...")
	if err := startNgrokAsync(); err != nil {
		return errors.Wrap(err, "Failed to start Ngrok")
	}

	if err := fetchAndPrintAcessInfosFromNgrok(); err != nil {
		return errors.Wrap(err, "Failed to fetch access infos from ngrok")
	}

	// wait forever
	fmt.Println()
	fmt.Println("You can now connect, keeping the connection open ...")
	for {
		fmt.Print(".")
		time.Sleep(10 * time.Second)
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
