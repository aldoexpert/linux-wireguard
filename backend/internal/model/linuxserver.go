package model

import "time"

// LinuxServerSettings holds the SSH connection details used to push the
// server + peer configuration to a remote Linux server running wg-quick,
// replacing (when Enabled) the local wg0.conf apply step.
//
// Auth is always by SSH private key (never password) -- PrivateKeyPEM is
// an OpenSSH/PEM-formatted private key, stored like the other secrets in
// this app (0600 file permissions, see library.ManagePerms).
// Interface is no longer part of this struct: the wg-quick interface name
// to sync now comes from each WgInterface.WgName, since one Linux server
// can host several WireGuard interfaces.
type LinuxServerSettings struct {
	Enabled       bool      `json:"enabled"`
	Host          string    `json:"host"`
	Port          int       `json:"port,string"`
	Username      string    `json:"username"`
	PrivateKeyPEM string    `json:"private_key_pem"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// LinuxServerSettingsView is what the API returns for GET -- it never
// echoes the private key back to the browser.
type LinuxServerSettingsView struct {
	Enabled   bool      `json:"enabled"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Username  string    `json:"username"`
	HasKey    bool      `json:"has_key"`
	UpdatedAt time.Time `json:"updated_at"`
}
