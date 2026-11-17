package library

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aldoexpert/linux-wireguard/internal/model"
)

// quoteRouterOS renders a Go string as a RouterOS script string literal.
func quoteRouterOS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}

// BuildMikrotikClientSetupCommands returns the RouterOS terminal commands
// that configure a client as a WireGuard *client* on a Mikrotik router
// (e.g. a branch/home router), instead of a phone or laptop. It's the
// RouterOS equivalent of the wg-quick .conf BuildClientConfig produces for
// those -- RouterOS has no importer for that file format, so an operator
// pastes this into that router's own terminal instead. It mirrors
// BuildClientConfig's fields exactly: the client's own [Interface] (its
// private key and address) and a [Peer] for our server, reached at
// iface.EndpointAddress -- this is independent of whichever backend
// pushes our own server's config (local wg-quick or a remote Linux server
// over SSH), since both expose the same WgInterface fields.
//
// Re-pasting this on that router is always safe, even if the client's
// address, keys or AllowedIPs changed since the last paste.
//
// The interface is the one exception to "tear down, then recreate": other
// configuration on that router (firewall rules, NAT, bridge ports, ...)
// that references "interface=wireguard1" ties to that object's internal
// ID, not just its name, so removing and re-adding it -- even under the
// same name -- would silently detach all of that, forcing it to be
// reconfigured by hand. So the interface is only added if missing, and
// otherwise left untouched entirely (no remove, no update-in-place
// either). That still can't produce a duplicate: RouterOS rejects a
// second interface under the same name outright. The existence check
// itself has no else branch -- it only skips an add, never decides
// between two actions to take -- which keeps it reliable even where
// :if/:len-based add-vs-update branching isn't.
//
// Address/peer/route have no such external-reference concern, so they're
// tagged with the same "mtwg-client:<id>" comment and torn down by that
// tag before being recreated -- simpler, and correct even when a field
// changed, which a plain "add only if missing" can't guarantee for them.
func BuildMikrotikClientSetupCommands(iface model.WgInterface, c model.Client) string {
	const ifName = "wireguard1"
	tag := "mtwg-client:" + c.ID
	var script strings.Builder

	ifArgs := fmt.Sprintf("name=%s private-key=%s comment=%s", quoteRouterOS(ifName), quoteRouterOS(c.PrivateKey), quoteRouterOS(tag))
	if iface.MTU > 0 {
		ifArgs += fmt.Sprintf(" mtu=%d", iface.MTU)
	}
	fmt.Fprintf(&script, ":if ([:len [/interface wireguard find where name=%s]] = 0) do={\n  /interface wireguard add %s\n}\n", quoteRouterOS(ifName), ifArgs)

	fmt.Fprintf(&script, "/ip route remove [find where comment=%s]\n", quoteRouterOS(tag))
	fmt.Fprintf(&script, "/interface wireguard peers remove [find where comment=%s]\n", quoteRouterOS(tag))
	fmt.Fprintf(&script, "/ip address remove [find where comment=%s]\n", quoteRouterOS(tag))

	for _, addr := range c.AllocatedIPs {
		fmt.Fprintf(&script, "/ip address add interface=%s address=%s comment=%s\n", quoteRouterOS(ifName), quoteRouterOS(addr), quoteRouterOS(tag))
	}

	// Same host/port resolution as BuildClientConfig's peerEndpoint.
	host := iface.EndpointAddress
	port := iface.Config.ListenPort
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		if n, err := strconv.Atoi(host[idx+1:]); err == nil {
			host, port = host[:idx], n
		}
	}

	allowed := append(append([]string{}, c.AllowedIPs...), c.ExtraAllowedIPs...)
	peerArgs := fmt.Sprintf("interface=%s public-key=%s allowed-address=%s endpoint-address=%s endpoint-port=%d comment=%s",
		quoteRouterOS(ifName), quoteRouterOS(iface.KeyPair.PublicKey), quoteRouterOS(strings.Join(allowed, ",")), quoteRouterOS(host), port, quoteRouterOS(tag))
	if c.PresharedKey != "" {
		peerArgs += " preshared-key=" + quoteRouterOS(c.PresharedKey)
	}
	if iface.PersistentKeepalive > 0 {
		peerArgs += fmt.Sprintf(" persistent-keepalive=%ds", iface.PersistentKeepalive)
	}
	fmt.Fprintf(&script, "/interface wireguard peers add %s\n", peerArgs)

	for _, net := range allowed {
		fmt.Fprintf(&script, "/ip route add dst-address=%s gateway=%s comment=%s\n", quoteRouterOS(net), quoteRouterOS(ifName), quoteRouterOS(tag))
	}

	return strings.TrimRight(script.String(), "\n")
}
