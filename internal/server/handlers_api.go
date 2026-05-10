package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tronbyt-server/internal/apps"
	"tronbyt-server/internal/data"
	"tronbyt-server/internal/renderer"

	securejoin "github.com/cyphar/filepath-securejoin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --- API Handlers ---

// DeviceUpdate represents the updatable fields for a device via API.
type DeviceUpdate struct {
	Brightness          *int    `json:"brightness"`
	IntervalSec         *int    `json:"intervalSec"`
	NightModeEnabled    *bool   `json:"nightModeEnabled"`
	NightModeActive     *bool   `json:"nightModeActive"`
	NightModeApp        *string `json:"nightModeApp"`
	NightModeBrightness *int    `json:"nightModeBrightness"`
	NightModeStartTime  *string `json:"nightModeStartTime"`
	NightModeEndTime    *string `json:"nightModeEndTime"`
	DimModeActive       *bool   `json:"dimModeActive"`
	DimModeStartTime    *string `json:"dimModeStartTime"`
	DimModeBrightness   *int    `json:"dimModeBrightness"`
	PinnedApp           *string `json:"pinnedApp"`
	AutoDim             *bool   `json:"autoDim"` // Legacy
}

// DevicePayload represents the full device data returned via API.
type DevicePayload struct {
	ID           string          `json:"id"`
	Type         data.DeviceType `json:"type"`
	DisplayName  string          `json:"displayName"`
	Notes        string          `json:"notes"`
	IntervalSec  int             `json:"intervalSec"`
	Brightness   int             `json:"brightness"`
	NightMode    NightMode       `json:"nightMode"`
	DimMode      DimMode         `json:"dimMode"`
	PinnedApp    *string         `json:"pinnedApp"`
	Interstitial Interstitial    `json:"interstitial"`
	LastSeen     *string         `json:"lastSeen"`
	Info         DeviceInfo      `json:"info"`
	AutoDim      bool            `json:"autoDim"`
}

// NightMode represents night mode settings in the API payload.
type NightMode struct {
	Enabled       bool    `json:"enabled"`
	Active        bool    `json:"active"`
	App           string  `json:"app"`
	StartTime     string  `json:"startTime"`
	EndTime       string  `json:"endTime"`
	Brightness    int     `json:"brightness"`
	OverrideUntil *string `json:"overrideUntil,omitempty"`
}

// DimMode represents dim mode settings in the API payload.
type DimMode struct {
	Enabled       bool    `json:"enabled"`
	Active        bool    `json:"active"`
	StartTime     *string `json:"startTime"`
	Brightness    *int    `json:"brightness"`
	OverrideUntil *string `json:"overrideUntil,omitempty"`
}

// Interstitial represents interstitial app settings in the API payload.
type Interstitial struct {
	Enabled bool    `json:"enabled"`
	App     *string `json:"app"`
}

// DeviceInfo represents device firmware and protocol information in the API payload.
type DeviceInfo struct {
	FirmwareVersion    string  `json:"firmwareVersion"`
	FirmwareType       string  `json:"firmwareType"`
	ProtocolVersion    *int    `json:"protocolVersion,omitempty"`
	MACAddress         string  `json:"macAddress"`
	ProtocolType       string  `json:"protocolType"`
	SSID               *string `json:"ssid,omitempty"`
	WifiPowerSave      *int    `json:"wifiPowerSave,omitempty"`
	SkipDisplayVersion *bool   `json:"skipDisplayVersion,omitempty"`
	SkipBootAnimation  *bool   `json:"skipBootAnimation,omitempty"`
	APMode             *bool   `json:"apMode,omitempty"`
	PreferIPv6         *bool   `json:"preferIPv6,omitempty"`
	SwapColors         *bool   `json:"swapColors,omitempty"`
	ImageURL           *string `json:"imageUrl,omitempty"`
	Hostname           *string `json:"hostname,omitempty"`
	SNTPServer         *string `json:"sntpServer,omitempty"`
	SyslogAddr         *string `json:"syslogAddr,omitempty"`
}

// toDevicePayload converts a data.Device model to a DevicePayload for API responses.
func (s *Server) toDevicePayload(d *data.Device) DevicePayload {
	now := deviceTimeNow(d)
	info := DeviceInfo{
		FirmwareVersion:    d.Info.FirmwareVersion,
		FirmwareType:       d.Info.FirmwareType,
		ProtocolVersion:    d.Info.ProtocolVersion,
		MACAddress:         d.Info.MACAddress,
		ProtocolType:       string(d.Info.ProtocolType),
		SSID:               d.Info.SSID,
		WifiPowerSave:      d.Info.WifiPowerSave,
		SkipDisplayVersion: d.Info.SkipDisplayVersion,
		SkipBootAnimation:  d.Info.SkipBootAnimation,
		APMode:             d.Info.APMode,
		PreferIPv6:         d.Info.PreferIPv6,
		SwapColors:         d.Info.SwapColors,
		ImageURL:           d.Info.ImageURL,
		Hostname:           d.Info.Hostname,
		SNTPServer:         d.Info.SNTPServer,
		SyslogAddr:         d.Info.SyslogAddr,
	}

	var lastSeen *string
	if d.LastSeen != nil {
		iso := d.LastSeen.Format(time.RFC3339)
		lastSeen = &iso
	}

	var dimBrightnessPtr *int
	if d.DimBrightness != nil {
		val := int(*d.DimBrightness)
		dimBrightnessPtr = &val
	}

	var nightModeOverrideUntil *string
	if d.GetNightModeOverrideActiveAt(now) && d.NightModeOverrideUntil != nil {
		formatted := d.NightModeOverrideUntil.In(now.Location()).Format(time.RFC3339)
		nightModeOverrideUntil = &formatted
	}
	var dimModeOverrideUntil *string
	if d.GetDimModeOverrideActiveAt(now) && d.DimModeOverrideUntil != nil {
		formatted := d.DimModeOverrideUntil.In(now.Location()).Format(time.RFC3339)
		dimModeOverrideUntil = &formatted
	}

	return DevicePayload{
		ID:          d.ID,
		Type:        d.Type,
		DisplayName: d.Name,
		Notes:       d.Notes,
		IntervalSec: d.DefaultInterval,
		Brightness:  int(d.Brightness),
		NightMode: NightMode{
			Enabled:       d.NightModeEnabled,
			Active:        d.GetNightModeIsActive(),
			App:           d.NightModeApp,
			StartTime:     d.NightStart,
			EndTime:       d.NightEnd,
			Brightness:    int(d.NightBrightness),
			OverrideUntil: nightModeOverrideUntil,
		},
		DimMode: DimMode{
			Enabled:       d.DimModeEnabled,
			Active:        d.GetDimModeIsActive(),
			StartTime:     d.DimTime,
			Brightness:    dimBrightnessPtr,
			OverrideUntil: dimModeOverrideUntil,
		},
		PinnedApp: d.PinnedApp,
		Interstitial: Interstitial{
			Enabled: d.InterstitialEnabled,
			App:     d.InterstitialApp,
		},
		LastSeen: lastSeen,
		Info:     info,
		AutoDim:  d.NightModeEnabled,
	}
}

// AppPayload represents the API response for an app installation.
type AppPayload struct {
	ID                string `json:"id"`
	AppID             string `json:"appID"`
	Enabled           bool   `json:"enabled"`
	Pinned            bool   `json:"pinned"`
	Pushed            bool   `json:"pushed"`
	RenderIntervalMin int    `json:"renderIntervalMin"`
	DisplayTimeSec    int    `json:"displayTimeSec"`
	LastRenderAt      int64  `json:"lastRenderAt"`
	IsInactive        bool   `json:"isInactive"`

	// Schedule fields
	StartTime *string  `json:"startTime"`
	EndTime   *string  `json:"endTime"`
	Days      []string `json:"days"`

	// Recurrence fields
	UseCustomRecurrence bool                `json:"useCustomRecurrence"`
	RecurrenceType      data.RecurrenceType `json:"recurrenceType"`
	RecurrenceInterval  int                 `json:"recurrenceInterval"`
	RecurrencePattern   map[string]any      `json:"recurrencePattern"`
	RecurrenceStartDate *string             `json:"recurrenceStartDate"`
	RecurrenceEndDate   *string             `json:"recurrenceEndDate"`
}

func (s *Server) toAppPayload(device *data.Device, app *data.App) AppPayload {
	pinned := device.PinnedApp != nil && *device.PinnedApp == app.Iname
	return AppPayload{
		ID:                app.Iname,
		AppID:             app.Name,
		Enabled:           app.Enabled,
		Pinned:            pinned,
		Pushed:            app.Pushed,
		RenderIntervalMin: app.UInterval,
		DisplayTimeSec:    app.DisplayTime,
		LastRenderAt:      app.LastRender.Unix(),
		IsInactive:        app.EmptyLastRender,

		StartTime: app.StartTime,
		EndTime:   app.EndTime,
		Days:      app.Days,

		UseCustomRecurrence: app.UseCustomRecurrence,
		RecurrenceType:      app.RecurrenceType,
		RecurrenceInterval:  app.RecurrenceInterval,
		RecurrencePattern:   app.RecurrencePattern,
		RecurrenceStartDate: app.RecurrenceStartDate,
		RecurrenceEndDate:   app.RecurrenceEndDate,
	}
}

// ListDevicesPayload represents the response for listing devices.
type ListDevicesPayload struct {
	Devices []DevicePayload `json:"devices"`
}

// PushAppData represents the data for pushing an app configuration.
type PushAppData struct {
	Config            map[string]any `json:"config"`
	AppID             string         `json:"app_id"`
	InstallationID    string         `json:"installationID"`
	InstallationIDAlt string         `json:"installationId"`
	Background        bool           `json:"background"`
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)

	// If using an API key associated with a specific device, this endpoint might not make sense
	// or should return only that device. The legacy behavior (Python) returns all devices for the user.
	// Since APIAuthMiddleware populates user with all devices preloaded, we can just use that.

	devicePayloads := make([]DevicePayload, 0, len(user.Devices))
	for i := range user.Devices {
		devicePayloads = append(devicePayloads, s.toDevicePayload(&user.Devices[i]))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ListDevicesPayload{Devices: devicePayloads}); err != nil {
		slog.Error("Failed to encode devices JSON", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handlePushApp(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	device := GetDevice(r)

	var dataReq PushAppData
	if err := json.NewDecoder(r.Body).Decode(&dataReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Determine installationID
	installationID := dataReq.InstallationID
	if installationID == "" {
		installationID = dataReq.InstallationIDAlt
	}

	// Look up existing app if installationID is provided to get Path, DisplayTime and filters
	var existingApp *data.App
	var appPath string
	if installationID != "" {
		existingApp = device.GetApp(installationID)
		if existingApp != nil && existingApp.Path != nil && *existingApp.Path != "" {
			// Infer app path from existing installation
			appPath, _ = securejoin.SecureJoin(s.DataDir, *existingApp.Path)
		}
	}

	// If we couldn't get appPath from installation, look it up by app_id
	if appPath == "" {
		if dataReq.AppID == "" {
			http.Error(w, "app_id is required when no valid installationID is provided", http.StatusBadRequest)
			return
		}

		// 1. Check System Apps
		for _, app := range s.ListSystemApps() {
			if app.ID == dataReq.AppID {
				appPath = filepath.Join(s.DataDir, app.Path)
				break
			}
		}

		// 2. Check User Apps
		if appPath == "" && user != nil {
			userApps := apps.ListUserApps(s.DataDir, user.Username)
			for _, app := range userApps {
				if app.ID == dataReq.AppID { // AppID for user apps is folder name
					appPath = filepath.Join(s.DataDir, app.Path)
					break
				}
			}
		}

		if appPath == "" {
			http.Error(w, "App not found", http.StatusNotFound)
			return
		}
	}

	// For pushed apps (existing app with "pushed:" path), skip rendering and push existing image
	if existingApp != nil && existingApp.Pushed {
		if existingApp.Path != nil && strings.HasPrefix(*existingApp.Path, "pushed:") {
			installationID := strings.TrimPrefix(*existingApp.Path, "pushed:")
			pushedImagePath, err := securejoin.SecureJoin(filepath.Join(s.DataDir, "webp", device.ID, "pushed"), installationID+".webp")
			if err != nil {
				slog.Error("Failed to resolve pushed image path", "error", err)
				http.Error(w, "Image not found", http.StatusNotFound)
				return
			}
			if imgBytes, err := os.ReadFile(pushedImagePath); err == nil {
				// Notify device via Websocket (unless background)
				if !dataReq.Background {
					s.Broadcaster.Notify(device.ID, imgBytes)
				}
				if err := s.ensurePushedApp(r.Context(), device.ID, installationID); err != nil {
					slog.Error("Error adding pushed app", "error", err)
				}
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte("App pushed.")); err != nil {
					slog.Error("Failed to write response", "error", err)
				}
				return
			}
		}
	}

	imgBytes, _, err := s.RenderApp(r.Context(), device, existingApp, appPath, dataReq.Config)
	if err != nil {
		slog.Error("Failed to render app", "error", err)
		http.Error(w, "Rendering failed", http.StatusInternalServerError)
		return
	}

	if len(imgBytes) == 0 {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("Empty image, not pushing")); err != nil {
			slog.Error("Failed to write empty image response", "error", err)
		}
		return
	}

	if installationID != "" {
		// Ensure app record exists
		if err := s.ensurePushedApp(r.Context(), device.ID, installationID); err != nil {
			slog.Error("Failed to ensure pushed app", "error", err)
		}
	}

	// Notify device via Websocket only if this is a foreground push
	sent := false
	if !dataReq.Background {
		sent = s.Broadcaster.Notify(device.ID, imgBytes)
	}

	if !sent || installationID != "" {
		if err := s.savePushedImage(device.ID, installationID, imgBytes); err != nil {
			http.Error(w, "Failed to save image", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("App pushed.")); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.toDevicePayload(device)); err != nil {
		slog.Error("Failed to encode device JSON", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleListInstallations(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)

	installations := make([]AppPayload, 0, len(device.Apps))
	for i := range device.Apps {
		installations = append(installations, s.toAppPayload(device, device.Apps[i]))
	}

	response := map[string]any{
		"installations": installations,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode installations JSON", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleGetInstallation(w http.ResponseWriter, r *http.Request) {
	iname := r.PathValue("iname")

	device := GetDevice(r)

	app := device.GetApp(iname)
	if app == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.toAppPayload(device, app)); err != nil {
		slog.Error("Failed to encode app JSON", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// PushData represents the data for pushing an image to a device.
type PushData struct {
	InstallationID    string `json:"installationID"`
	InstallationIDAlt string `json:"installationId"`
	Image             string `json:"image"`
	Background        bool   `json:"background"`
}

func (s *Server) handlePushImage(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)

	var dataReq PushData
	if err := json.NewDecoder(r.Body).Decode(&dataReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	installID := dataReq.InstallationID
	if installID == "" {
		installID = dataReq.InstallationIDAlt
	}

	imgBytes, err := base64.StdEncoding.DecodeString(dataReq.Image)
	if err != nil {
		http.Error(w, "Invalid Base64 Image", http.StatusBadRequest)
		return
	}

	if installID != "" {
		if err := s.ensurePushedApp(r.Context(), device.ID, installID); err != nil {
			slog.Error("Error adding pushed app", "error", err)
		}
	}

	// Notify device via Websocket only if this is a foreground push
	sent := false
	if !dataReq.Background {
		sent = s.Broadcaster.Notify(device.ID, imgBytes)
	}

	if !sent || installID != "" {
		if err := s.savePushedImage(device.ID, installID, imgBytes); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save image: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("WebP received.")); err != nil {
		slog.Error("Failed to write WebP received message", "error", err)
		// Non-fatal, response already 200
	}
}

func (s *Server) savePushedImage(deviceID, installID string, data []byte) error {
	dir, err := s.ensureDeviceImageDir(deviceID)
	if err != nil {
		return fmt.Errorf("failed to get device webp directory: %w", err)
	}

	dir = filepath.Join(dir, "pushed")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var filename string
	if installID != "" {
		filename = installID + ".webp"
	} else {
		filename = fmt.Sprintf("__%d.webp", time.Now().UnixNano())
	}

	path, err := securejoin.SecureJoin(dir, filename)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (s *Server) ensurePushedApp(ctx context.Context, deviceID, installID string) error {
	// Check if app exists by matching on installID (for pushed apps, we need to look up by installID)
	// Since installID might be non-numeric (e.g., "pushed:hasssolarlocal1"), we check via path/file
	count, err := gorm.G[data.App](s.DB).Where("device_id = ? AND pushed = ? AND path = ?", deviceID, true, "pushed:"+installID).Count(ctx, "*")
	if err != nil {
		slog.Error("Failed to check if app exists for image push", "error", err)
		return err
	}
	if count > 0 {
		return nil
	}

	// Generate a numeric iname for the pushed app (same as regular apps)
	newIname, err := generateUniqueIname(s.DB, deviceID)
	if err != nil {
		slog.Error("Failed to generate iname for pushed app", "error", err)
		return err
	}

	// Store installID in path so we can match on it later
	installPath := "pushed:" + installID

	newApp := data.App{
		DeviceID:    deviceID,
		Iname:       newIname,
		Name:        "pushed",
		UInterval:   10,
		DisplayTime: 0,
		Enabled:     true,
		Pushed:      true,
		Path:        &installPath,
	}

	maxOrder, err := getMaxAppOrder(s.DB, deviceID)
	if err != nil {
		slog.Error("Failed to get max app order", "error", err)
		// Non-fatal, default to 0 for order (if maxOrder is 0)
	}
	newApp.Order = maxOrder + 1

	return gorm.G[data.App](s.DB).Create(ctx, &newApp)
}

func (s *Server) handlePatchDevice(w http.ResponseWriter, r *http.Request) {
	// Auth handled by middleware, get device
	device := GetDevice(r)

	var update DeviceUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if update.Brightness != nil {
		device.Brightness = data.Brightness(*update.Brightness)
	}
	if update.IntervalSec != nil {
		device.DefaultInterval = *update.IntervalSec
	}
	nightModeWasEnabled := device.NightModeEnabled
	nightStartWas := device.NightStart
	nightEndWas := device.NightEnd
	dimModeWasEnabled := device.DimModeEnabled
	var dimTimeWas string
	if device.DimTime != nil {
		dimTimeWas = *device.DimTime
	}
	if update.NightModeEnabled != nil {
		device.NightModeEnabled = *update.NightModeEnabled
	}
	if update.AutoDim != nil {
		device.NightModeEnabled = *update.AutoDim
	}
	if update.NightModeApp != nil {
		if *update.NightModeApp != "" {
			if device.GetApp(*update.NightModeApp) == nil {
				http.Error(w, "Night mode app not found", http.StatusBadRequest)
				return
			}
		}
		device.NightModeApp = *update.NightModeApp
	}
	if update.NightModeBrightness != nil {
		device.NightBrightness = data.Brightness(*update.NightModeBrightness)
	}
	if update.PinnedApp != nil {
		if *update.PinnedApp != "" {
			if device.GetApp(*update.PinnedApp) == nil {
				http.Error(w, "Pinned app not found", http.StatusBadRequest)
				return
			}
		}
		if *update.PinnedApp == "" {
			device.PinnedApp = nil
		} else {
			device.PinnedApp = update.PinnedApp
		}
	}

	if update.NightModeStartTime != nil {
		device.NightStart = *update.NightModeStartTime
	}
	if update.NightModeEndTime != nil {
		device.NightEnd = *update.NightModeEndTime
	}
	if update.DimModeStartTime != nil {
		device.DimTime = update.DimModeStartTime
	}
	if update.DimModeBrightness != nil {
		val := data.Brightness(*update.DimModeBrightness)
		device.DimBrightness = &val
	}

	if !device.NightModeEnabled || nightModeWasEnabled != device.NightModeEnabled || nightStartWas != device.NightStart || nightEndWas != device.NightEnd {
		clearNightModeOverride(device)
	}
	if update.NightModeActive != nil {
		if _, err := setNightModeOverride(device, *update.NightModeActive); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	currentDimTime := ""
	if device.DimTime != nil {
		currentDimTime = *device.DimTime
	}
	if !device.DimModeEnabled || dimModeWasEnabled != device.DimModeEnabled || dimTimeWas != currentDimTime {
		clearDimModeOverride(device)
	}
	if update.DimModeActive != nil {
		if _, err := setDimModeOverride(device, *update.DimModeActive); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := s.DB.Omit("Apps").Save(device).Error; err != nil {
		http.Error(w, "Failed to update device", http.StatusInternalServerError)
		return
	}

	// Notify Dashboard
	user := GetUser(r)
	s.notifyDashboard(user.Username, WSEvent{Type: "apps_changed", DeviceID: device.ID})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.toDevicePayload(device)); err != nil {
		slog.Error("Failed to encode device", "error", err)
	}
}

// InstallationUpdate represents the updatable fields for an app installation via API.
type InstallationUpdate struct {
	Enabled           *bool `json:"enabled"`
	Pinned            *bool `json:"pinned"`
	RenderIntervalMin *int  `json:"renderIntervalMin"`
	DisplayTimeSec    *int  `json:"displayTimeSec"`

	// Schedule fields
	StartTime *string   `json:"startTime"`
	EndTime   *string   `json:"endTime"`
	Days      *[]string `json:"days"`

	// Recurrence fields
	UseCustomRecurrence *bool                `json:"useCustomRecurrence"`
	RecurrenceType      *data.RecurrenceType `json:"recurrenceType"`
	RecurrenceInterval  *int                 `json:"recurrenceInterval"`
	RecurrencePattern   *map[string]any      `json:"recurrencePattern"`
	RecurrenceStartDate *string              `json:"recurrenceStartDate"`
	RecurrenceEndDate   *string              `json:"recurrenceEndDate"`
}

func (s *Server) handlePatchInstallation(w http.ResponseWriter, r *http.Request) {
	iname := r.PathValue("iname")

	device := GetDevice(r)

	app := device.GetApp(iname)
	if app == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	var update InstallationUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if update.Enabled != nil {
		app.Enabled = *update.Enabled
		if !app.Enabled {
			// Delete associated webp files when app is disabled
			webpDir, err := s.ensureDeviceImageDir(device.ID)
			if err != nil {
				slog.Error("Failed to get device webp directory for app disable cleanup", "device_id", device.ID, "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			matches, _ := filepath.Glob(filepath.Join(webpDir, fmt.Sprintf("*-%s.webp", app.Iname)))
			for _, match := range matches {
				if err := os.Remove(match); err != nil {
					slog.Error("Failed to remove webp file on app disable", "path", match, "error", err)
				}
			}
			// Also check for pushed webp files
			pushedWebpPath := filepath.Join(webpDir, "pushed", fmt.Sprintf("%s.webp", app.Iname))
			if _, err := os.Stat(pushedWebpPath); err == nil {
				if err := os.Remove(pushedWebpPath); err != nil {
					slog.Error("Failed to remove pushed webp file on app disable", "path", pushedWebpPath, "error", err)
				}
			}
		} else {
			// Reset LastRender when app is enabled
			app.LastRender = time.Time{}
		}
	}
	if update.RenderIntervalMin != nil {
		app.UInterval = *update.RenderIntervalMin
	}
	if update.DisplayTimeSec != nil {
		app.DisplayTime = *update.DisplayTimeSec
	}
	if update.Pinned != nil {
		if *update.Pinned {
			device.PinnedApp = &app.Iname
		} else if device.PinnedApp != nil && *device.PinnedApp == app.Iname {
			device.PinnedApp = nil
		}
		// Save device for pinned change
		if err := s.DB.Omit("Apps").Save(device).Error; err != nil {
			http.Error(w, "Failed to update device pin status", http.StatusInternalServerError)
			return
		}
	}

	// Schedule fields
	if update.StartTime != nil {
		if *update.StartTime == "" {
			app.StartTime = nil
		} else {
			parsed, err := parseTimeInput(*update.StartTime)
			if err != nil {
				http.Error(w, fmt.Sprintf("Invalid startTime: %v", err), http.StatusBadRequest)
				return
			}
			app.StartTime = &parsed
		}
	}
	if update.EndTime != nil {
		if *update.EndTime == "" {
			app.EndTime = nil
		} else {
			parsed, err := parseTimeInput(*update.EndTime)
			if err != nil {
				http.Error(w, fmt.Sprintf("Invalid endTime: %v", err), http.StatusBadRequest)
				return
			}
			app.EndTime = &parsed
		}
	}
	if update.Days != nil {
		for _, day := range *update.Days {
			switch day {
			case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
				// valid
			default:
				http.Error(w, fmt.Sprintf("Invalid day: %s", day), http.StatusBadRequest)
				return
			}
		}
		app.Days = *update.Days
	}

	// Recurrence fields
	if update.UseCustomRecurrence != nil {
		app.UseCustomRecurrence = *update.UseCustomRecurrence
	}
	if update.RecurrenceType != nil {
		switch *update.RecurrenceType {
		case data.RecurrenceDaily, data.RecurrenceWeekly, data.RecurrenceMonthly, data.RecurrenceYearly:
			app.RecurrenceType = *update.RecurrenceType
		default:
			http.Error(w, "Invalid recurrenceType", http.StatusBadRequest)
			return
		}
	}
	if update.RecurrenceInterval != nil {
		app.RecurrenceInterval = *update.RecurrenceInterval
	}
	if update.RecurrencePattern != nil {
		app.RecurrencePattern = *update.RecurrencePattern
	}
	if update.RecurrenceStartDate != nil {
		if *update.RecurrenceStartDate == "" {
			app.RecurrenceStartDate = nil
		} else {
			if _, err := time.Parse("2006-01-02", *update.RecurrenceStartDate); err != nil {
				http.Error(w, "Invalid recurrenceStartDate: must be YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			app.RecurrenceStartDate = update.RecurrenceStartDate
		}
	}
	if update.RecurrenceEndDate != nil {
		if *update.RecurrenceEndDate == "" {
			app.RecurrenceEndDate = nil
		} else {
			if _, err := time.Parse("2006-01-02", *update.RecurrenceEndDate); err != nil {
				http.Error(w, "Invalid recurrenceEndDate: must be YYYY-MM-DD", http.StatusBadRequest)
				return
			}
			app.RecurrenceEndDate = update.RecurrenceEndDate
		}
	}

	if err := s.DB.Save(app).Error; err != nil {
		http.Error(w, "Failed to update app", http.StatusInternalServerError)
		return
	}

	// Notify Dashboard
	user := GetUser(r)
	s.notifyDashboard(user.Username, WSEvent{Type: "apps_changed", DeviceID: device.ID})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(app); err != nil {
		slog.Error("Failed to encode app", "error", err)
	}
}

func (s *Server) handleDeleteInstallationAPI(w http.ResponseWriter, r *http.Request) {
	installID := filepath.Base(r.PathValue("iname"))

	device := GetDevice(r)

	// First try to find the app by iname (server-generated ID)
	app, err := gorm.G[data.App](s.DB).Where("device_id = ? AND iname = ?", device.ID, installID).First(r.Context())
	if err != nil {
		// If not found by iname, try to find by installationID (stored in path as "pushed:{installationID}")
		app, err = gorm.G[data.App](s.DB).Where("device_id = ? AND path = ?", device.ID, "pushed:"+installID).First(r.Context())
		if err != nil {
			http.Error(w, "App not found", http.StatusNotFound)
			return
		}
	}

	// Delete the app
	if _, err := gorm.G[data.App](s.DB).Where("id = ?", app.ID).Delete(r.Context()); err != nil {
		http.Error(w, "Failed to delete app", http.StatusInternalServerError)
		return
	}

	// Clean up files using the actual iname
	webpDir, err := s.ensureDeviceImageDir(device.ID)
	if err != nil {
		slog.Error("Failed to get device webp directory for app delete cleanup", "device_id", device.ID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Clean up pushed app image if applicable
	if app.Pushed && app.Path != nil && len(*app.Path) > 7 && (*app.Path)[:7] == "pushed:" {
		pushedID := (*app.Path)[7:]
		pushedWebpPath := filepath.Join(webpDir, "pushed", pushedID+".webp")
		if err := os.Remove(pushedWebpPath); err != nil && !os.IsNotExist(err) {
			slog.Error("Failed to remove pushed webp file", "path", pushedWebpPath, "error", err)
		}
	}

	matches, _ := filepath.Glob(filepath.Join(webpDir, fmt.Sprintf("*-%s.webp", app.Iname)))
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			slog.Error("Failed to remove webp file", "path", match, "error", err)
		}
	}

	// Notify Dashboard
	user := GetUser(r)
	s.notifyDashboard(user.Username, WSEvent{Type: "apps_changed", DeviceID: device.ID})

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("App deleted.")); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}

func (s *Server) handleRebootDeviceAPI(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)

	if err := s.sendRebootCommand(device.ID); err != nil {
		slog.Error("Failed to send reboot command", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Reboot command sent.")); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}

// FirmwareSettingsUpdate represents the updatable firmware settings via API.
type FirmwareSettingsUpdate struct {
	SkipDisplayVersion *bool   `json:"skipDisplayVersion"`
	SkipBootAnimation  *bool   `json:"skipBootAnimation"`
	PreferIPv6         *bool   `json:"preferIPv6"`
	APMode             *bool   `json:"apMode"`
	SwapColors         *bool   `json:"swapColors"`
	WifiPowerSave      *int    `json:"wifiPowerSave"`
	ImageURL           *string `json:"imageUrl"`
	Hostname           *string `json:"hostname"`
	SNTPServer         *string `json:"sntpServer"`
	SyslogAddr         *string `json:"syslogAddr"`
}

func (s *Server) handleUpdateFirmwareSettingsAPI(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)

	var update FirmwareSettingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	payload := make(map[string]any)

	if update.SkipDisplayVersion != nil {
		payload["skip_display_version"] = *update.SkipDisplayVersion
	}
	if update.SkipBootAnimation != nil {
		payload["skip_boot_animation"] = *update.SkipBootAnimation
	}
	if update.PreferIPv6 != nil {
		payload["prefer_ipv6"] = *update.PreferIPv6
	}
	if update.APMode != nil {
		payload["ap_mode"] = *update.APMode
	}
	if update.SwapColors != nil {
		payload["swap_colors"] = *update.SwapColors
	}
	if update.WifiPowerSave != nil {
		payload["wifi_power_save"] = *update.WifiPowerSave
	}
	if update.ImageURL != nil {
		payload["image_url"] = *update.ImageURL
	}
	if update.Hostname != nil {
		payload["hostname"] = *update.Hostname
	}
	if update.SNTPServer != nil {
		payload["sntp_server"] = *update.SNTPServer
	}
	if update.SyslogAddr != nil {
		payload["syslog_addr"] = *update.SyslogAddr
	}

	if len(payload) == 0 {
		http.Error(w, "No settings provided", http.StatusBadRequest)
		return
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal firmware settings payload", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	s.Broadcaster.Notify(device.ID, DeviceCommandMessage{Payload: jsonPayload})

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Firmware settings updated.")); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}

// writeAPIError writes a JSON error response: {"error": "..."} with the given status code.
// /v0 endpoints added for the iOS client must always return JSON; the iOS client crashes on
// non-JSON responses.
func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Error("Failed to encode API error", "error", err)
	}
}

// writeAPIJSON writes value as JSON with the given status code.
func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("Failed to encode API JSON", "error", err)
	}
}

// AppCatalogEntry represents an entry in the apps catalog returned by the iOS client.
// JSON keys are snake_case to match the existing iOS Codable expectations.
type AppCatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Installed   bool   `json:"installed"`
}

// AppCatalogResponse is the response shape for /v0/apps and /v0/devices/{id}/apps.
type AppCatalogResponse struct {
	SystemApps []AppCatalogEntry `json:"system_apps"`
	CustomApps []AppCatalogEntry `json:"custom_apps"`
}

func toCatalogEntry(meta apps.AppMetadata) AppCatalogEntry {
	desc := meta.Summary
	if desc == "" {
		desc = meta.Desc
	}
	return AppCatalogEntry{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: desc,
		Path:        meta.Path,
		Installed:   meta.IsInstalled,
	}
}

// buildAppCatalog returns system + custom app catalogs for the given user. If device is
// non-nil the Installed flag is populated against that device's installations.
func (s *Server) buildAppCatalog(user *data.User, device *data.Device) AppCatalogResponse {
	systemApps := s.ListSystemApps()
	customApps := apps.ListUserApps(s.DataDir, user.Username)
	if device != nil {
		s.markInstalledApps(device, systemApps, customApps)
	}

	resp := AppCatalogResponse{
		SystemApps: make([]AppCatalogEntry, 0, len(systemApps)),
		CustomApps: make([]AppCatalogEntry, 0, len(customApps)),
	}
	for _, m := range systemApps {
		resp.SystemApps = append(resp.SystemApps, toCatalogEntry(m))
	}
	for _, m := range customApps {
		resp.CustomApps = append(resp.CustomApps, toCatalogEntry(m))
	}
	return resp
}

// findAppByID looks up an app by its catalog ID across system apps and the user's custom apps.
// Returns nil if not found.
func (s *Server) findAppByID(user *data.User, appID string) *apps.AppMetadata {
	for _, m := range s.ListSystemApps() {
		if m.ID == appID {
			meta := m
			return &meta
		}
	}
	for _, m := range apps.ListUserApps(s.DataDir, user.Username) {
		if m.ID == appID {
			meta := m
			return &meta
		}
	}
	return nil
}

func (s *Server) handleListAppsAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	writeAPIJSON(w, http.StatusOK, s.buildAppCatalog(user, nil))
}

func (s *Server) handleListDeviceAppsAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	device := GetDevice(r)
	writeAPIJSON(w, http.StatusOK, s.buildAppCatalog(user, device))
}

func (s *Server) handleGetAppSchemaAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	appID := r.PathValue("appId")

	meta := s.findAppByID(user, appID)
	if meta == nil {
		writeAPIError(w, http.StatusNotFound, "App not found")
		return
	}

	appPath, err := securejoin.SecureJoin(s.DataDir, meta.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid app path")
		return
	}

	var schemaBytes []byte
	if !strings.HasSuffix(strings.ToLower(appPath), ".webp") {
		schemaBytes, err = renderer.GetSchema(r.Context(), appPath, 64, 32, false)
		if err != nil {
			slog.Error("Failed to get app schema", "appID", appID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Failed to get schema")
			return
		}
	}
	if len(schemaBytes) == 0 {
		schemaBytes = []byte("{}")
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"schema":`)); err != nil {
		slog.Error("Failed to write schema response", "error", err)
		return
	}
	if _, err := w.Write(schemaBytes); err != nil {
		slog.Error("Failed to write schema body", "error", err)
		return
	}
	if _, err := w.Write([]byte(`}`)); err != nil {
		slog.Error("Failed to write schema response", "error", err)
	}
}

type schemaHandlerRequest struct {
	Param  string         `json:"param"`
	Config map[string]any `json:"config"`
}

// runSchemaHandler executes a Pixlet schema handler at appPath and writes the JSON result.
func (s *Server) runSchemaHandler(w http.ResponseWriter, r *http.Request, appPath string, supports2x bool, config map[string]any) {
	handler := r.PathValue("handler")

	var payload schemaHandlerRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if payload.Config != nil {
		config = payload.Config
	}

	// Pixlet's CallSchemaHandler can panic on apps that don't declare the requested handler in
	// their schema. Recover locally so the iOS client always sees a JSON envelope.
	var (
		result string
		err    error
	)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Schema handler panicked", "handler", handler, "panic", rec)
				err = fmt.Errorf("handler panicked: %v", rec)
			}
		}()
		result, err = renderer.CallSchemaHandler(
			r.Context(),
			appPath,
			config,
			64, 32,
			supports2x,
			handler,
			payload.Param,
		)
	}()
	if err != nil {
		slog.Error("Schema handler failed", "handler", handler, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Schema handler failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(result)); err != nil {
		slog.Error("Failed to write schema handler response", "error", err)
	}
}

func (s *Server) handleSchemaHandlerAppAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	appID := r.PathValue("appId")

	meta := s.findAppByID(user, appID)
	if meta == nil {
		writeAPIError(w, http.StatusNotFound, "App not found")
		return
	}
	appPath, err := securejoin.SecureJoin(s.DataDir, meta.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid app path")
		return
	}
	s.runSchemaHandler(w, r, appPath, false, nil)
}

func (s *Server) handleSchemaHandlerInstallationAPI(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)
	iname := r.PathValue("iname")
	app := device.GetApp(iname)
	if app == nil {
		writeAPIError(w, http.StatusNotFound, "Installation not found")
		return
	}
	if app.Path == nil || *app.Path == "" {
		writeAPIError(w, http.StatusBadRequest, "App path not set")
		return
	}
	appPath, err := securejoin.SecureJoin(s.DataDir, *app.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid app path")
		return
	}
	s.runSchemaHandler(w, r, appPath, device.Type.Supports2x(), app.Config)
}

// renderAndWriteWebP renders the given app at appPath with the provided config and writes
// image/webp to w.
func (s *Server) renderAndWriteWebP(w http.ResponseWriter, r *http.Request, device *data.Device, app *data.App, appPath string, config map[string]any) {
	imgBytes, _, err := s.RenderApp(r.Context(), device, app, appPath, config)
	if err != nil {
		slog.Error("Preview render failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Render failed")
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := w.Write(imgBytes); err != nil {
		slog.Error("Failed to write preview image bytes", "error", err)
	}
}

func parsePreviewConfig(r *http.Request) (map[string]any, bool, error) {
	configParam := r.URL.Query().Get("config")
	if configParam == "" {
		return nil, false, nil
	}
	var configData map[string]any
	if err := json.Unmarshal([]byte(configParam), &configData); err != nil {
		return nil, false, err
	}
	return configData, true, nil
}

func (s *Server) handlePreviewInstallationAPI(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)
	iname := r.PathValue("iname")
	app := device.GetApp(iname)
	if app == nil {
		writeAPIError(w, http.StatusNotFound, "Installation not found")
		return
	}

	override, hasOverride, err := parsePreviewConfig(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid config JSON")
		return
	}

	// Pushed apps: serve the saved image (no override possible).
	if app.Pushed && app.Path != nil && strings.HasPrefix(*app.Path, "pushed:") {
		installationID := strings.TrimPrefix(*app.Path, "pushed:")
		webpDir, err := s.ensureDeviceImageDir(device.ID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		path, err := securejoin.SecureJoin(filepath.Join(webpDir, "pushed"), installationID+".webp")
		if err == nil {
			if _, err := os.Stat(path); err == nil {
				http.ServeFile(w, r, path)
				return
			}
		}
		writeAPIError(w, http.StatusNotFound, "Image not found")
		return
	}

	if app.Path == nil || *app.Path == "" {
		writeAPIError(w, http.StatusBadRequest, "App path not set")
		return
	}
	appPath, err := securejoin.SecureJoin(s.DataDir, *app.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid app path")
		return
	}

	config := app.Config
	if hasOverride {
		config = override
	}
	s.renderAndWriteWebP(w, r, device, app, appPath, config)
}

func (s *Server) handlePreviewAppAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	device := GetDevice(r)
	appID := r.PathValue("appId")

	meta := s.findAppByID(user, appID)
	if meta == nil {
		writeAPIError(w, http.StatusNotFound, "App not found")
		return
	}
	appPath, err := securejoin.SecureJoin(s.DataDir, meta.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid app path")
		return
	}

	override, _, err := parsePreviewConfig(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid config JSON")
		return
	}
	s.renderAndWriteWebP(w, r, device, nil, appPath, override)
}

// CreateDevicePayload represents the JSON body for POST /v0/devices.
type CreateDevicePayload struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Brightness      *int   `json:"brightness"`
	DefaultInterval *int   `json:"default_interval"`
	Notes           string `json:"notes"`
}

func (s *Server) handleCreateDeviceAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)

	var payload CreateDevicePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if payload.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "name is required")
		return
	}
	for _, d := range user.Devices {
		if d.Name == payload.Name {
			writeAPIError(w, http.StatusConflict, "name already exists")
			return
		}
	}

	deviceType := data.DeviceOther
	if payload.Type != "" {
		if t, ok := data.StringToDeviceType[payload.Type]; ok {
			deviceType = t
		} else {
			writeAPIError(w, http.StatusBadRequest, "invalid type")
			return
		}
	}

	deviceID, err := generateSecureToken(8)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Failed to generate device ID")
		return
	}
	apiKey, err := generateSecureToken(32)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Failed to generate API key")
		return
	}

	brightness := data.Brightness(20)
	if payload.Brightness != nil {
		brightness = data.Brightness(*payload.Brightness)
	}
	defaultInterval := 15
	if payload.DefaultInterval != nil {
		defaultInterval = *payload.DefaultInterval
	}
	defaultColorFilter := data.ColorFilterNone

	newDevice := data.Device{
		ID:              deviceID,
		Username:        user.Username,
		Name:            payload.Name,
		Type:            deviceType,
		APIKey:          apiKey,
		RequireAPIKey:   true,
		Notes:           payload.Notes,
		Brightness:      brightness,
		DefaultInterval: defaultInterval,
		ColorFilter:     &defaultColorFilter,
	}
	newDevice.ImgURL = s.getImageURLWithKey(r, newDevice.ID, apiKey)
	newDevice.WsURL = s.getWebsocketURLWithKey(r, newDevice.ID, apiKey)

	if err := gorm.G[data.Device](s.DB).Create(r.Context(), &newDevice); err != nil {
		slog.Error("Failed to create device via API", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to save device")
		return
	}
	if _, err := s.ensureDeviceImageDir(newDevice.ID); err != nil {
		slog.Error("Failed to create device webp dir", "device_id", newDevice.ID, "error", err)
	}

	writeAPIJSON(w, http.StatusCreated, s.toDevicePayload(&newDevice))
}

// CreateInstallationPayload represents the JSON body for POST /v0/devices/{id}/installations.
// The iOS client sends snake_case keys (uinterval, display_time, app_name) so accept those.
type CreateInstallationPayload struct {
	AppName     string         `json:"app_name"`
	Enabled     *bool          `json:"enabled"`
	UInterval   *int           `json:"uinterval"`
	DisplayTime *int           `json:"display_time"`
	Notes       string         `json:"notes"`
	Config      map[string]any `json:"config"`
}

func (s *Server) handleCreateInstallationAPI(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	device := GetDevice(r)

	var payload CreateInstallationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if payload.AppName == "" {
		writeAPIError(w, http.StatusBadRequest, "app_name is required")
		return
	}

	meta := s.findAppByID(user, payload.AppName)
	if meta == nil {
		writeAPIError(w, http.StatusNotFound, "App not found")
		return
	}

	iname, err := generateUniqueIname(s.DB, device.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Failed to generate installation ID")
		return
	}

	uinterval := 10
	if payload.UInterval != nil && *payload.UInterval > 0 {
		uinterval = *payload.UInterval
	} else if meta.RecommendedInterval > 0 {
		uinterval = meta.RecommendedInterval
	}
	displayTime := 0
	if payload.DisplayTime != nil {
		displayTime = *payload.DisplayTime
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	pathCopy := meta.Path
	newApp := data.App{
		DeviceID:    device.ID,
		Iname:       iname,
		Name:        meta.ID,
		UInterval:   uinterval,
		DisplayTime: displayTime,
		Notes:       payload.Notes,
		Enabled:     enabled,
		Path:        &pathCopy,
		Config:      data.JSONMap(payload.Config),
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if user.AddAppsToTop {
			if _, err := gorm.G[data.App](tx).
				Where("device_id = ?", device.ID).
				Update(r.Context(), "order", gorm.Expr("? + 1", clause.Column{Name: "order"})); err != nil {
				return err
			}
			newApp.Order = 0
		} else {
			maxOrder, err := getMaxAppOrder(tx, device.ID)
			if err != nil {
				return err
			}
			newApp.Order = maxOrder + 1
		}
		return gorm.G[data.App](tx).Create(r.Context(), &newApp)
	})
	if err != nil {
		slog.Error("Failed to create installation via API", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to save installation")
		return
	}

	// Trigger initial render (mirrors handleAddAppPost).
	s.possiblyRender(r.Context(), &newApp, device, user)

	s.notifyDashboard(user.Username, WSEvent{Type: "apps_changed", DeviceID: device.ID})

	writeAPIJSON(w, http.StatusCreated, s.toAppPayload(device, &newApp))
}

func (s *Server) handleDeleteDeviceAPI(w http.ResponseWriter, r *http.Request) {
	device := GetDevice(r)
	user := GetUser(r)

	deviceWebpDir, err := s.ensureDeviceImageDir(device.ID)
	if err != nil {
		slog.Error("Failed to get device webp directory for deletion", "device_id", device.ID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if err := os.RemoveAll(deviceWebpDir); err != nil {
		slog.Error("Failed to remove device webp directory", "device_id", device.ID, "error", err)
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[data.App](tx).Where("device_id = ?", device.ID).Delete(r.Context()); err != nil {
			return err
		}
		if _, err := gorm.G[data.Device](tx).Where("id = ?", device.ID).Delete(r.Context()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		slog.Error("Failed to delete device via API", "device_id", device.ID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to delete device")
		return
	}

	s.notifyDashboard(user.Username, WSEvent{Type: "device_deleted", DeviceID: device.ID})

	writeAPIJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) SetupAPIRoutes() {
	// API v0 Group - authenticated with Middleware
	s.Router.Handle("GET /v0/devices", s.APIAuthMiddleware(http.HandlerFunc(s.handleListDevices)))
	s.Router.Handle("POST /v0/devices", s.APIAuthMiddleware(http.HandlerFunc(s.handleCreateDeviceAPI)))
	s.Router.Handle("GET /v0/devices/{id}", s.APIAuthMiddleware(s.RequireDevice(s.handleGetDevice)))
	s.Router.Handle("DELETE /v0/devices/{id}", s.APIAuthMiddleware(s.RequireDevice(s.handleDeleteDeviceAPI)))
	s.Router.Handle("POST /v0/devices/{id}/push", s.APIAuthMiddleware(s.RequireDevice(s.handlePushImage)))
	s.Router.Handle("POST /v0/devices/{id}/push_app", s.APIAuthMiddleware(s.RequireDevice(s.handlePushApp)))
	s.Router.Handle("POST /v0/devices/{id}/update_firmware_settings", s.APIAuthMiddleware(s.RequireDevice(s.handleUpdateFirmwareSettingsAPI)))
	s.Router.Handle("POST /v0/devices/{id}/reboot", s.APIAuthMiddleware(s.RequireDevice(s.handleRebootDeviceAPI)))
	s.Router.Handle("GET /v0/devices/{id}/installations", s.APIAuthMiddleware(s.RequireDevice(s.handleListInstallations)))
	s.Router.Handle("POST /v0/devices/{id}/installations", s.APIAuthMiddleware(s.RequireDevice(s.handleCreateInstallationAPI)))
	s.Router.Handle("GET /v0/devices/{id}/installations/{iname}", s.APIAuthMiddleware(s.RequireDevice(s.handleGetInstallation)))
	s.Router.Handle("PATCH /v0/devices/{id}", s.APIAuthMiddleware(s.RequireDevice(s.handlePatchDevice)))
	s.Router.Handle("PATCH /v0/devices/{id}/installations/{iname}", s.APIAuthMiddleware(s.RequireDevice(s.handlePatchInstallation)))
	s.Router.Handle("DELETE /v0/devices/{id}/installations/{iname}", s.APIAuthMiddleware(s.RequireDevice(s.handleDeleteInstallationAPI)))
	s.Router.Handle("GET /v0/devices/{id}/installations/{iname}/preview", s.APIAuthMiddleware(s.RequireDevice(s.handlePreviewInstallationAPI)))
	s.Router.Handle("POST /v0/devices/{id}/installations/{iname}/schema_handler/{handler}", s.APIAuthMiddleware(s.RequireDevice(s.handleSchemaHandlerInstallationAPI)))
	s.Router.Handle("GET /v0/devices/{id}/apps", s.APIAuthMiddleware(s.RequireDevice(s.handleListDeviceAppsAPI)))
	s.Router.Handle("GET /v0/devices/{id}/apps/{appId}/preview", s.APIAuthMiddleware(s.RequireDevice(s.handlePreviewAppAPI)))
	s.Router.Handle("GET /v0/apps", s.APIAuthMiddleware(http.HandlerFunc(s.handleListAppsAPI)))
	s.Router.Handle("GET /v0/apps/{appId}/schema", s.APIAuthMiddleware(http.HandlerFunc(s.handleGetAppSchemaAPI)))
	s.Router.Handle("POST /v0/apps/{appId}/schema_handler/{handler}", s.APIAuthMiddleware(http.HandlerFunc(s.handleSchemaHandlerAppAPI)))
}
