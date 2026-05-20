/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sshexec_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/faroshq/faros-kedge/pkg/linuxmcp"
	"github.com/faroshq/faros-kedge/pkg/linuxmcp/sshexec"
)

// startTestSSHServer starts a minimal SSH server on a free localhost port and
// returns its address.  The server accepts any auth and executes commands via
// `/bin/sh -c`.  It is *not* safe for any use outside this test file.
//
// Returns a stop func that closes the listener.
func startTestSSHServer(t *testing.T) (addr string, stop func()) {
	t.Helper()

	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	cfg := &gossh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg)
		}
	}()

	return l.Addr().String(), func() { _ = l.Close() }
}

func serveConn(c net.Conn, cfg *gossh.ServerConfig) {
	sc, chans, reqs, err := gossh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sc.Close() //nolint:errcheck
	go gossh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(gossh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go serveSession(ch, chReqs)
	}
}

func serveSession(ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer ch.Close() //nolint:errcheck
	for req := range reqs {
		switch req.Type {
		case "exec":
			// First 4 bytes are length-prefixed; rest is the command string.
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				return
			}
			cmd := string(req.Payload[4:])
			_ = req.Reply(true, nil)
			runRemoteCmd(ch, cmd)
			return
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func runRemoteCmd(ch gossh.Channel, cmd string) {
	c := exec.Command("/bin/sh", "-c", cmd)
	stdout, _ := c.StdoutPipe()
	stderr, _ := c.StderrPipe()
	if err := c.Start(); err != nil {
		sendExit(ch, 127)
		return
	}
	// Sync the pipe copies before sending exit-status: c.Wait() returns as
	// soon as the process exits, but the goroutines copying stdout/stderr
	// to the SSH channel may not have flushed yet.  Without this WaitGroup
	// the client may see exit-status (and channel close) before all bytes
	// have traversed the channel, producing empty stdout/stderr.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(ch, stdout) }()
	go func() { defer wg.Done(); _, _ = io.Copy(ch.Stderr(), stderr) }()
	err := c.Wait()
	wg.Wait()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		exit = 1
	}
	sendExit(ch, exit)
}

// sendExit sends an SSH exit-status message and closes the channel.
func sendExit(ch gossh.Channel, code int) {
	msg := struct{ Code uint32 }{Code: uint32(code)}
	_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(msg))
}

// providerFor builds a linuxmcp.Provider whose OpenSession dials our test
// server.  The edge is always named "test-edge".
func providerFor(t *testing.T, addr string) *linuxmcp.Provider {
	t.Helper()
	return linuxmcp.NewProvider(linuxmcp.Config{
		Cluster:        "root",
		EdgeNames:      []string{"test-edge"},
		CommandTimeout: 5 * time.Second,
		MaxOutputBytes: 4096,
		OpenSession: func(_ context.Context, _ string) (*gossh.Client, error) {
			cfg := &gossh.ClientConfig{
				User:            "test",
				Auth:            []gossh.AuthMethod{gossh.Password("")},
				HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // test only
				Timeout:         2 * time.Second,
			}
			return gossh.Dial("tcp", addr, cfg)
		},
	})
}

func TestRun_HappyPath(t *testing.T) {
	addr, stop := startTestSSHServer(t)
	defer stop()

	p := providerFor(t, addr)
	res, err := sshexec.Run(context.Background(), p, "", "echo hello && echo err >&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Errorf("stdout: got %q, want to contain %q", res.Stdout, "hello")
	}
	if !strings.Contains(res.Stderr, "err") {
		t.Errorf("stderr: got %q, want to contain %q", res.Stderr, "err")
	}
	if res.Target != "test-edge" {
		t.Errorf("target defaulting: got %q, want %q", res.Target, "test-edge")
	}
}

func TestRun_NonZeroExitIsNotError(t *testing.T) {
	addr, stop := startTestSSHServer(t)
	defer stop()

	p := providerFor(t, addr)
	res, err := sshexec.Run(context.Background(), p, "", "exit 7")
	if err != nil {
		t.Fatalf("Run returned error for non-zero exit: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code: got %d, want 7", res.ExitCode)
	}
}

func TestRun_OutputCapping(t *testing.T) {
	addr, stop := startTestSSHServer(t)
	defer stop()

	// Use a tiny cap to guarantee truncation.
	p := linuxmcp.NewProvider(linuxmcp.Config{
		Cluster:        "root",
		EdgeNames:      []string{"test-edge"},
		CommandTimeout: 5 * time.Second,
		MaxOutputBytes: 16,
		OpenSession: func(_ context.Context, _ string) (*gossh.Client, error) {
			cfg := &gossh.ClientConfig{
				User:            "test",
				Auth:            []gossh.AuthMethod{gossh.Password("")},
				HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
				Timeout:         2 * time.Second,
			}
			return gossh.Dial("tcp", addr, cfg)
		},
	})

	res, err := sshexec.Run(context.Background(), p, "", "printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true with 16-byte cap")
	}
	if len(res.Stdout) > 16 {
		t.Errorf("stdout length: got %d, want <= 16", len(res.Stdout))
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"with'quote", `'with'\''quote'`},
		{"", "''"},
	}
	for _, c := range cases {
		if got := sshexec.ShellQuote(c.in); got != c.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
