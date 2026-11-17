package wireguard

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/aldoexpert/linux-wireguard/internal/core"
	"github.com/aldoexpert/linux-wireguard/internal/library/linuxserver"
	"github.com/aldoexpert/linux-wireguard/internal/library/store"
	"github.com/aldoexpert/linux-wireguard/internal/model"
)

type LinuxServerSettingsController struct {
	db store.IStore
}

func NewLinuxServerSettingsController(db store.IStore) *LinuxServerSettingsController {
	return &LinuxServerSettingsController{db: db}
}

func toView(s model.LinuxServerSettings) model.LinuxServerSettingsView {
	return model.LinuxServerSettingsView{
		Enabled:   s.Enabled,
		Host:      s.Host,
		Port:      s.Port,
		Username:  s.Username,
		HasKey:    s.PrivateKeyPEM != "",
		UpdatedAt: s.UpdatedAt,
	}
}

// Get returns the Linux server SSH connection settings. The private key
// is never echoed back -- HasKey just tells the UI whether one is stored.
func (c *LinuxServerSettingsController) Get(w http.ResponseWriter, r *http.Request, params []string) {
	settings, err := c.db.GetLinuxServerSettings()
	if err != nil {
		core.InternalError(w, "Could not get the Linux Server settings", err)
		return
	}
	core.Success(w, "", toView(settings))
}

type linuxServerSettingsRequest struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	PrivateKeyPEM string `json:"private_key_pem"` // optional; blank keeps the stored key
}

// settingsFromRequest fully replaces the stored settings with req -- used
// by Update, where the frontend always submits the whole form. An empty
// private_key_pem keeps whatever key is already stored.
func (c *LinuxServerSettingsController) settingsFromRequest(req linuxServerSettingsRequest) (model.LinuxServerSettings, error) {
	existing, err := c.db.GetLinuxServerSettings()
	if err != nil {
		return existing, err
	}

	existing.Enabled = req.Enabled
	existing.Host = req.Host
	existing.Port = req.Port
	existing.Username = req.Username
	if req.PrivateKeyPEM != "" {
		existing.PrivateKeyPEM = req.PrivateKeyPEM
	}
	return existing, nil
}

// settingsOverlay layers only the non-blank fields of req on top of the
// stored settings -- used by Test, whose body may be partial or empty
// (meaning "test with what's already saved").
func (c *LinuxServerSettingsController) settingsOverlay(req linuxServerSettingsRequest) (model.LinuxServerSettings, error) {
	existing, err := c.db.GetLinuxServerSettings()
	if err != nil {
		return existing, err
	}

	if req.Host != "" {
		existing.Host = req.Host
	}
	if req.Port != 0 {
		existing.Port = req.Port
	}
	if req.Username != "" {
		existing.Username = req.Username
	}
	if req.PrivateKeyPEM != "" {
		existing.PrivateKeyPEM = req.PrivateKeyPEM
	}
	return existing, nil
}

// Update edits the Linux server SSH connection settings. Sending an empty
// private_key_pem keeps whatever key is already stored.
func (c *LinuxServerSettingsController) Update(w http.ResponseWriter, r *http.Request, params []string) {
	var req linuxServerSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		core.BadRequest(w, "Invalid data")
		return
	}

	settings, err := c.settingsFromRequest(req)
	if err != nil {
		core.InternalError(w, "Could not get the current Linux Server settings", err)
		return
	}

	if settings.Enabled && (settings.Host == "" || settings.Username == "" || settings.PrivateKeyPEM == "") {
		core.BadRequest(w, "Host, username and an SSH private key are required to enable the Linux Server integration")
		return
	}

	settings.UpdatedAt = time.Now().UTC()
	if err := c.db.SaveLinuxServerSettings(settings); err != nil {
		core.InternalError(w, "Could not save the Linux Server settings", err)
		return
	}

	core.Success(w, "Linux Server settings updated successfully", nil)
}

// Test tries an SSH connection with the given (or, if omitted, the
// currently stored) settings and returns the server's hostname.
func (c *LinuxServerSettingsController) Test(w http.ResponseWriter, r *http.Request, params []string) {
	var req linuxServerSettingsRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // optional body; blank fields fall back to stored settings

	settings, err := c.settingsOverlay(req)
	if err != nil {
		core.InternalError(w, "Could not get the current Linux Server settings", err)
		return
	}

	hostname, err := linuxserver.TestConnection(settings)
	if err != nil {
		core.BadRequest(w, "Connection failed: "+err.Error())
		return
	}

	core.Success(w, "Connected successfully", map[string]string{"identity": hostname})
}
