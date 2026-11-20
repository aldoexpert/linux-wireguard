// Package linuxserver pushes this app's WireGuard server + client (peer)
// configuration to a remote Linux server over SSH, replacing the local
// wg0.conf apply step when a LinuxServerSettings record is Enabled. The
// remote server is expected to run wg-quick with systemd (the
// wg-quick@<ifName> unit), the same way this app's own README documents
// installing WireGuard locally.
//
// Auth is always by SSH private key (ssh.PublicKeys), never password --
// the key is generated once by the operator and installed for a
// dedicated, least-privilege user on that server (see README.md "Linux
// Server Integration"). Every command this package runs is prefixed with
// sudo, since that user is not expected to be root -- the operator grants
// it passwordless sudo for exactly the commands below (see README.md),
// not blanket root access.
//
// Unlike a RouterOS device (see this project's sibling mikrotik-wireguard
// project, which this one was forked from), a Linux wg-quick interface has
// no incremental "add this one peer" command and no per-peer comment
// field to tag entries with -- the whole interface is defined by one
// config file. So instead of diffing individual peers, ApplySync always
// renders and pushes the *complete* desired config for the interface.
//
// How that gets applied to the live device depends on whether it already
// exists (checked via `ip link show`, not systemd's unit state -- a unit
// can be stuck "failed" from a previous bad restart while the device
// itself is still up and passing traffic, which is exactly the case this
// matters for): if it does, `wg syncconf` reloads it live, with no
// PreDown/PostUp re-run and no peer disconnects. A full
// `systemctl restart` was tried here first, but that runs the interface's
// PreDown before PostUp -- and if PreDown fails (observed in production:
// an iptables rule referencing Docker's DOCKER-USER chain failing to
// delete because the chain state had already changed), wg-quick aborts
// before reaching `ip link delete`, so the device survives the "stop" but
// the subsequent "start" then fails with "wg0' already exists" and keeps
// failing that way on every later apply -- never reapplying the new
// config, yet never dropping the tunnel either. Reusing an already-up
// device instead of tearing it down avoids that failure mode entirely.
package linuxserver

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/aldoexpert/linux-wireguard/internal/model"
)

// ifNameRe matches a safe Linux network interface name: this is
// interpolated directly into shell commands and a systemd unit name, so
// it's validated up front rather than merely escaped -- the kernel itself
// also rejects anything longer than 15 bytes (IFNAMSIZ-1).
var ifNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,15}$`)

// Dial opens an SSH connection to the remote Linux server.
func Dial(settings model.LinuxServerSettings) (*ssh.Client, error) {
	if settings.Host == "" {
		return nil, fmt.Errorf("no Linux server host configured")
	}
	if settings.PrivateKeyPEM == "" {
		return nil, fmt.Errorf("no SSH private key configured")
	}

	signer, err := ssh.ParsePrivateKey([]byte(settings.PrivateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("invalid SSH private key: %w", err)
	}

	port := settings.Port
	if port == 0 {
		port = 22
	}

	cfg := &ssh.ClientConfig{
		User:    settings.Username,
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: 10 * time.Second,
		// This server is reached over an operator-controlled network path
		// (not the public CA system); pinning the host key fingerprint
		// instead of this is a worthwhile future hardening step, tracked
		// in README.md.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := fmt.Sprintf("%s:%d", settings.Host, port)
	return ssh.Dial("tcp", addr, cfg)
}

// run executes a shell script (one or more statements, newline separated)
// in a single SSH session and returns its combined stdout.
func run(client *ssh.Client, script string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(script); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// TestConnection verifies the stored host/credentials actually reach the
// server and that sudo works non-interactively, returning its hostname.
func TestConnection(settings model.LinuxServerSettings) (string, error) {
	client, err := Dial(settings)
	if err != nil {
		return "", err
	}
	defer client.Close()

	out, err := run(client, "sudo -n true && hostname")
	if err != nil {
		return "", fmt.Errorf("connected, but: %s (check that this user has passwordless sudo configured -- see README.md)", err)
	}
	return strings.TrimSpace(out), nil
}

// shellQuote wraps a string in single quotes for safe interpolation into
// a shell command, escaping any embedded single quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ApplySync pushes the complete wg-quick config for iface (rendered the
// same way as the local wg0.conf, via library.RenderWireGuardServerConfig)
// to the remote Linux server, then restarts its wg-quick@<ifName> systemd
// unit so the change takes effect.
//
// content is the fully-rendered config text (see
// library.RenderWireGuardServerConfig) -- this package only pushes it, it
// doesn't render it, to avoid an import cycle with the library package.
func ApplySync(settings model.LinuxServerSettings, iface model.WgInterface, content string) error {
	ifName := iface.WgName
	if !ifNameRe.MatchString(ifName) {
		return fmt.Errorf("interface name %q is not a safe Linux interface name (letters, digits, - and _ only, max 15 chars)", ifName)
	}

	client, err := Dial(settings)
	if err != nil {
		return err
	}
	defer client.Close()

	remotePath := fmt.Sprintf("/etc/wireguard/%s.conf", ifName)
	unit := fmt.Sprintf("wg-quick@%s", ifName)
	overrideDir := fmt.Sprintf("/etc/systemd/system/%s.service.d", unit)
	overridePath := overrideDir + "/linux-wireguard-ordering.conf"

	// The heredoc delimiter is quoted ('EOF') so the shell does zero
	// expansion on the config content in between -- none of its $, `, or
	// \ characters (e.g. inside a base64 key) are treated specially.
	var script strings.Builder
	fmt.Fprintf(&script, "sudo mkdir -p /etc/wireguard\n")
	fmt.Fprintf(&script, "sudo tee %s > /dev/null <<'LXWG_CONF_EOF'\n%s\nLXWG_CONF_EOF\n", shellQuote(remotePath), content)
	fmt.Fprintf(&script, "sudo chmod 600 %s\n", shellQuote(remotePath))

	// A PostUp/PreDown script that touches Docker's DOCKER-USER chain (a
	// common pattern on a host that also runs containers) fails this unit
	// at boot if wg-quick starts before Docker does -- systemd gives no
	// ordering guarantee between unrelated units by default. This drop-in
	// is harmless even without Docker installed: "After"/"Wants" on a unit
	// that doesn't exist is simply a no-op, never an error or a hang.
	fmt.Fprintf(&script, "sudo mkdir -p %s\n", shellQuote(overrideDir))
	fmt.Fprintf(&script, "sudo tee %s > /dev/null <<'LXWG_OVERRIDE_EOF'\n[Unit]\nAfter=docker.service\nWants=docker.service\nLXWG_OVERRIDE_EOF\n", shellQuote(overridePath))
	fmt.Fprintf(&script, "sudo systemctl daemon-reload\n")

	fmt.Fprintf(&script, "sudo systemctl enable %s\n", shellQuote(unit))

	// If the device is already up, reload it live instead of restarting
	// the unit -- see the package doc comment for why a full restart
	// (PreDown then PostUp) is unsafe to rely on here. `ip link show` is
	// checked directly rather than the unit's systemd state, since a unit
	// can be stuck "failed" from a previous bad restart while the device
	// itself is still up.
	//
	// The stripped config is written to a temp file rather than piped via
	// `<(...)` process substitution: that needs bash specifically (not
	// POSIX sh), and more importantly a sudoers NOPASSWD rule for
	// "bash -c <arbitrary string>" can't be scoped tightly -- it's
	// effectively unrestricted shell access. A plain pipe between two
	// fixed, narrowly-scoped sudo commands avoids both problems.
	syncconfTmpPath := fmt.Sprintf("/etc/wireguard/.%s.syncconf.tmp", ifName)
	fmt.Fprintf(&script, "if ip link show %s > /dev/null 2>&1; then\n", shellQuote(ifName))
	fmt.Fprintf(&script, "  sudo systemctl reset-failed %s 2>/dev/null || true\n", shellQuote(unit))
	fmt.Fprintf(&script, "  sudo wg-quick strip %s | sudo tee %s > /dev/null\n", shellQuote(ifName), shellQuote(syncconfTmpPath))
	fmt.Fprintf(&script, "  sudo wg syncconf %s %s\n", shellQuote(ifName), shellQuote(syncconfTmpPath))
	fmt.Fprintf(&script, "else\n")
	fmt.Fprintf(&script, "  sudo systemctl restart %s\n", shellQuote(unit))
	fmt.Fprintf(&script, "fi\n")

	_, err = run(client, script.String())
	return err
}

// PeerStatus is one WireGuard peer's live state, as shown by `wg show
// <ifName> dump` on the remote server.
type PeerStatus struct {
	PublicKey     string
	AllowedIPs    string // comma separated, as reported by wg itself
	Endpoint      string // "host:port", or "" if never connected
	RxBytes       int64
	TxBytes       int64
	LastHandshake time.Time // zero value if never
}

// GetPeerStatuses lists the live state of every peer on ifName (a Linux
// wg-quick interface, i.e. WgInterface.WgName), used by the Status page
// instead of the local wgctrl device when the Linux Server integration
// is Enabled.
func GetPeerStatuses(settings model.LinuxServerSettings, ifName string) ([]PeerStatus, error) {
	if !ifNameRe.MatchString(ifName) {
		return nil, fmt.Errorf("interface name %q is not a safe Linux interface name (letters, digits, - and _ only, max 15 chars)", ifName)
	}

	client, err := Dial(settings)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	out, err := run(client, fmt.Sprintf("sudo wg show %s dump", shellQuote(ifName)))
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return nil, nil
	}
	// The first line describes the interface itself (private-key,
	// public-key, listen-port, fwmark), not a peer -- skip it.
	peerLines := lines[1:]

	peers := make([]PeerStatus, 0, len(peerLines))
	for _, line := range peerLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// public-key  preshared-key  endpoint  allowed-ips  latest-handshake  rx  tx  persistent-keepalive
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		p := PeerStatus{
			PublicKey:  f[0],
			AllowedIPs: f[3],
			RxBytes:    parseInt64(f[5]),
			TxBytes:    parseInt64(f[6]),
		}
		if f[2] != "(none)" {
			p.Endpoint = f[2]
		}
		if secs := parseInt64(f[4]); secs > 0 {
			p.LastHandshake = time.Unix(secs, 0)
		}
		peers = append(peers, p)
	}
	return peers, nil
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
