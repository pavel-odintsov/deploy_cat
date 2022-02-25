package main

import (
	"github.com/digitalocean/godo"

	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

type TokenSource struct {
	AccessToken string
}

func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

// Generate random symbol string of fixed length
func random_string(strlen int) string {
	rand.Seed(time.Now().UTC().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func create_digital_ocean_droplet() (string, error) {
	key_file_path := "/etc/do_api.key"
	pat_key, err := ioutil.ReadFile(key_file_path)

	if err != nil {
		return "", errors.Errorf("Could not read key from file: %s", key_file_path)
	}

	tokenSource := &TokenSource{
		AccessToken: strings.TrimSpace(string(pat_key)),
	}

	oauthClient := oauth2.NewClient(oauth2.NoContext, tokenSource)
	client := godo.NewClient(oauthClient)

	droplet_name := "fastnetmon-test-deployment-" + random_string(12)

	createRequest := &godo.DropletCreateRequest{
		Name:   droplet_name,
		Region: "fra1",
		Size:   "2gb",
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-20-04-x64",
		},
		SSHKeys: []godo.DropletCreateSSHKey{godo.DropletCreateSSHKey{Fingerprint: "99:ab:...:"}},
	}

	ctx := context.TODO()

	new_droplet, _, err := client.Droplets.Create(ctx, createRequest)

	if err != nil {
		log.Fatalf("Something bad happened: %s\n", err)
	}

	_ = new_droplet
	log.Printf("Correctly deployed new droplet with ID: %d", new_droplet.ID)

	// Short sleep before next command
	time.Sleep(1 * time.Second)

	// Here we will store IPv4 address of VM
	host_ip_address := ""

	// Wait for container creation
	for true {
		droplet, _, err := client.Droplets.Get(ctx, new_droplet.ID)

		if err != nil {
			log.Fatal("Could not get context for %d: %v", new_droplet.ID, err)
		}

		// If it locked we should wait more
		if droplet.Locked {
			log.Println("Waiting...")
			time.Sleep(3 * time.Second)
			continue
		} else {
			// Fill IP address
			host_ip_address = droplet.Networks.V4[0].IPAddress

			log.Printf("Container was created! We are ready to connect to it using %s", host_ip_address)
			return host_ip_address, nil
		}

		//log.Printf("Droplet get: %v", droplet)
	}

	return "", errors.Errorf("Reached impossible place")
}

func main() {
	host_ip_address, err := create_digital_ocean_droplet()

	if err != nil {
		log.Fatalf("VM creation failed: %v", err)
	}

	log.Fatal("It's debug. Stop processing")

	public_key_file := PublicKeyFile(".ssh/id_rsa")

	if public_key_file == nil {
		log.Fatal("Could not read certificate")
	}

	// Here we could start SSH operations
	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{public_key_file},
	}

	log.Println("Establish connection to server")

	var connection *ssh.Client

	// Attempt for 100 seconds
	attempt_limit := 10
	current_attempt := 0

	connected_correctly := false

	for current_attempt < attempt_limit {
		current_attempt++

		connection, err = ssh.Dial("tcp", host_ip_address+":22", sshConfig)
		if err != nil {
			log.Printf("Failed to dial: %s. Will try again...", err)
			time.Sleep(10 * time.Second)
			continue
		}

		connected_correctly = true
	}

	if !connected_correctly {
		log.Fatal("Could not connect correctly")
	}

	session, err := connection.NewSession()

	if err != nil {
		log.Fatalf("Failed to create session: %s", err)
	}

	// Full explanation what we are doing here: http://blog.ralch.com/tutorial/golang-ssh-connection/:w

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		session.Close()
		log.Fatalf("request for pseudo terminal failed: %s", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		log.Fatalf("Unable to setup stdin for session: %v", err)
	}
	go io.Copy(stdin, os.Stdin)

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Fatalf("Unable to setup stdout for session: %v", err)
	}
	go io.Copy(os.Stdout, stdout)

	stderr, err := session.StderrPipe()
	if err != nil {
		log.Fatalf("Unable to setup stderr for session: %v", err)
	}
	go io.Copy(os.Stderr, stderr)

	err = session.Run("wget http://install.fastnetmon.com/installer -Oinstaller; chmod +x installer; ./installer -do_not_check_license")

	if err != nil {
		log.Fatalf("Could not execute command: %v", err)
	}

	log.Println("Executed correctly")

	/*
		log.Println("Destroy droplet")
		_, err = client.Droplets.Delete(ctx, droplet_id)
		if err != nil {
			log.Fatalf("Droplet.Delete returned error: %v", err)
		}
	*/
}

func PublicKeyFile(file string) ssh.AuthMethod {
	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}
