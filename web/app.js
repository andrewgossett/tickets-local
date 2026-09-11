"use strict";

const app = {
  state: {
    settings: {
      organization: "Tickets Local",
      center_address: "",
      center_lat: 39.8283,
      center_lon: -98.5795,
      default_zoom: 4,
      incident_stale_minutes: 10,
      aprs: {
        enabled: false,
        mode: "internet",
        server: "rotate.aprs2.net:14580",
        login_callsign: "",
        extra_filter: "",
        watch_callsigns: [],
        stale_minutes: 15,
        trail_hours: 12,
        area_enabled: false,
        area_radius_miles: 25,
        passcode_configured: false,
        local: { decoder: "bundled", kiss_address: "127.0.0.1:8001", audio_device: "", audio_output_device: "", show_all: true, igate_enabled: false }
      }
    },
    incidents: [],
    responders: [],
    facilities: [],
    locations: [],
    overlays: [],
    assets: [],
    schedule: [],
    qualifications: [],
    messages: [],
    tracks: [],
    aprs_stations: [],
    aprs_status: { state: "disabled", message: "APRS tracking is off" },
    activity: []
  },
  network: {
    configured: { mode: "standalone", host_url: "", access_key: "", listen_port: 8787 },
    active_mode: "standalone",
    active_port: 8787,
    restart_required: false,
    host_urls: [],
    clients: [],
    message: "Standalone mode"
  },
  settingsFormDirty: {
    network: false,
    general: false,
    aprs: false,
    weather: false,
    water: false,
    integrations: false
  },
  connections: { checked_at: null, overall: "waiting", items: [] },
  weather: { state: "disabled", message: "Weather awareness is off", alerts: [] },
  water: { state: "disabled", message: "Water monitoring is off", gauges: [] },
  integrations: { state:"disabled", message:"External operational feeds are off", storm_reports:[], infrastructure:[], amateur_repeaters:[], gmrs_repeaters:[], meshcore_nodes:[], aredn_nodes:[], sensors:[] },
  mapPacks: [],
  page: "dashboard",
  incidentFilter: "active",
  severityFilter: "",
  search: "",
  refreshTimer: null,
  stateRequestSequence: 0,
  stateAppliedSequence: 0,
  resourceTabsEnabled: localStorage.getItem("tickets-local-resource-tabs") === "true",
  resourceTypeFilter: "all",
  messageChannelFilter: "all",
  aprsAudioDevices: [],
  mapWindowMode: new URLSearchParams(window.location.search).get("view") === "map",
  mobileMode: new URLSearchParams(window.location.search).get("view") === "mobile",
  mobileDevices: [],
  map: null
};

const severityRank = { critical: 0, high: 1, medium: 2, low: 3 };
const activeStatuses = new Set(["new", "assigned", "enroute", "onscene"]);
const pageTitles = {
  dashboard: "Situation",
  incidents: "Incidents",
  responders: "Responders",
  facilities: "Facilities",
  locations: "Locations",
  assets: "Assets",
  schedule: "Schedule",
  qualifications: "Qualifications",
  messages: "Messages",
  activity: "Activity",
  settings: "Settings"
};

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

document.addEventListener("DOMContentLoaded", init);

async function init() {
  if (app.mobileMode) {
    document.body.classList.add("mobile-companion-mode");
    $("#mobile-companion").hidden = false;
    applyTheme(localStorage.getItem("tickets-local-theme") || "dark");
    bindMobileCompanion();
    await redeemMobileEnrollment();
    await loadMobileState(false);
    app.refreshTimer = setInterval(() => loadMobileState(false), 10000);
    return;
  }
  if (app.mapWindowMode) document.body.classList.add("map-window-mode");
  applyTheme(localStorage.getItem("tickets-local-theme") || "dark");
  bindNavigation();
  bindMapPosition();
  bindActions();
  bindForms();
  bindDialogs();
  startClock();
  app.map = new SituationMap($("#situation-map"));
  await loadNetworkSettings();
  await loadConnections();
  await loadState(true);
  await loadMobileDevices();
  await loadWeather(true);
  await loadWater(true);
  await loadIntegrations(true);
  await loadMapPacks(true);
  connectEvents();
  startAPRSStatusPoll();
  startResponderPositionPoll();
  startLANStatePoll();
  startNetworkStatusPoll();
  startWeatherPoll();
}

function bindMobileCompanion() {
  const savedKey = localStorage.getItem("tickets-local-mobile-lan-key") || "";
  $("#mobile-access-key").value = savedKey;
  $("#mobile-connect").addEventListener("click", async () => {
    const key = $("#mobile-access-key").value.trim();
    if (key) localStorage.setItem("tickets-local-mobile-lan-key", key);
    else localStorage.removeItem("tickets-local-mobile-lan-key");
    await loadMobileState(true);
  });
  $("#mobile-refresh").addEventListener("click", () => loadMobileState(true));
  $("#mobile-responder-select").addEventListener("change", event => {
    localStorage.setItem("tickets-local-mobile-responder", event.target.value);
    renderMobileCompanion();
  });
  $("#mobile-status-grid").addEventListener("click", event => {
    const button = event.target.closest("[data-mobile-status]");
    if (button) updateMobileResponderStatus(button.dataset.mobileStatus, button);
  });
  $("#mobile-forget-key").addEventListener("click", () => {
    localStorage.removeItem("tickets-local-mobile-lan-key");
    localStorage.removeItem("tickets-local-mobile-device-key");
    localStorage.removeItem("tickets-local-mobile-responder");
    $("#mobile-access-key").value = "";
    $("#mobile-operations").hidden = true;
    $("#mobile-setup-card").hidden = false;
    setMobileConnection(false, "Access key removed");
  });
}

async function redeemMobileEnrollment() {
  const token = new URLSearchParams(window.location.search).get("enroll") || "";
  if (!token) return;
  setMobileConnection(false, "Enrolling");
  try {
    const result = await api("/api/mobile/enroll", { method: "POST", body: { token, device_name: navigator.platform || "Mobile browser" }, skipMobileAuth: true });
    localStorage.setItem("tickets-local-mobile-device-key", result.device_key);
    localStorage.setItem("tickets-local-mobile-responder", result.device.responder_id);
    localStorage.removeItem("tickets-local-mobile-lan-key");
    history.replaceState({}, "", `${location.pathname}?view=mobile`);
    toast("Phone enrolled", "This device now has its own revocable credential.");
  } catch (error) {
    $("#mobile-setup-message").textContent = error.message;
    setMobileConnection(false, "Enrollment failed");
  }
}

async function loadMobileState(showError = false) {
  setMobileConnection(false, "Connecting");
  try {
    const deviceKey = localStorage.getItem("tickets-local-mobile-device-key") || "";
    if (deviceKey) {
      const mobileState = await api("/api/mobile/state");
      app.state = { ...app.state, responders: [mobileState.responder], incidents: mobileState.incidents };
      localStorage.setItem("tickets-local-mobile-responder", mobileState.responder.id);
    } else {
      app.state = await api("/api/state");
    }
    $("#mobile-setup-card").hidden = true;
    $("#mobile-operations").hidden = false;
    setMobileConnection(true, "Live");
    renderMobileCompanion();
  } catch (error) {
    $("#mobile-setup-card").hidden = false;
    $("#mobile-operations").hidden = true;
    setMobileConnection(false, error.status === 401 ? "Access key required" : "Host unavailable");
    if (showError) toast("Could not connect", error.message, "error");
  }
}

function setMobileConnection(connected, message) {
  const state = $("#mobile-sync-state");
  state.classList.toggle("offline", !connected);
  state.innerHTML = `<span></span> ${html(message)}`;
}

function renderMobileCompanion() {
  if (!app.mobileMode) return;
  const select = $("#mobile-responder-select");
  const deviceEnrolled = Boolean(localStorage.getItem("tickets-local-mobile-device-key"));
  const savedID = localStorage.getItem("tickets-local-mobile-responder") || "";
  const responders = [...(app.state.responders || [])].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  select.innerHTML = `<option value="">Choose your responder record</option>${responders.map(item => `<option value="${attr(item.id)}">${html(displayResponder(item))}</option>`).join("")}`;
  select.value = responders.some(item => item.id === savedID) ? savedID : "";
  select.disabled = deviceEnrolled;
  const responder = responders.find(item => item.id === select.value);
  const current = $("#mobile-current-status");
  current.className = `mobile-current-status${responder ? ` status-${attr(responder.status)}` : ""}`;
  current.textContent = responder ? `${displayResponder(responder)} is ${label(responder.status)}` : "Choose a responder to update status.";
  $$("[data-mobile-status]").forEach(button => {
    button.disabled = !responder;
    button.classList.toggle("active", responder?.status === button.dataset.mobileStatus);
  });
  const assignment = responder ? activeIncidents().find(incident => (incident.assignments || []).some(item => item.responder_id === responder.id)) : null;
  $("#mobile-assignment-title").textContent = assignment ? `Incident #${assignment.number} · ${assignment.title}` : responder ? "No active assignment" : "No responder selected";
  $("#mobile-assignment-detail").innerHTML = assignment
    ? `<strong>${html(locationText(assignment))}</strong><span>${html(label(assignment.severity))} priority · ${html(label(assignment.status))}</span><p>${html(assignment.description || "No additional incident details.")}</p>`
    : `<div class="mobile-empty">${responder ? "This responder is not assigned to an active incident." : "Select your responder record above."}</div>`;
  const incidents = activeIncidents().sort(sortIncidents);
  $("#mobile-incident-list").innerHTML = incidents.length ? incidents.map(incident => {
    const assigned = (incident.assignments || []).map(item => responderByID(item.responder_id)).filter(Boolean);
    return `<article class="mobile-incident severity-${attr(incident.severity)}"><div><strong>#${incident.number} · ${html(incident.title)}</strong><span>${html(locationText(incident))}</span></div><span class="badge status-${attr(incident.status)}">${html(label(incident.status))}</span><small>${assigned.length ? `Assigned: ${html(assigned.map(displayResponder).join(", "))}` : "No responders assigned"}</small></article>`;
  }).join("") : `<div class="mobile-empty">No active incidents.</div>`;
}

async function updateMobileResponderStatus(status, button) {
  const responder = (app.state.responders || []).find(item => item.id === $("#mobile-responder-select").value);
  if (!responder) return;
  $$("[data-mobile-status]").forEach(item => item.disabled = true);
  try {
    const deviceEnrolled = Boolean(localStorage.getItem("tickets-local-mobile-device-key"));
    await api(deviceEnrolled ? "/api/mobile/status" : `/api/responders/${encodeURIComponent(responder.id)}`, { method: "PUT", body: deviceEnrolled ? {
      status,
      expected_updated_at: responder.updated_at
    } : {
      name: responder.name,
      callsign: responder.callsign,
      type: responder.type,
      status,
      phone: responder.phone,
      capabilities: responder.capabilities || [],
      latitude: responder.latitude,
      longitude: responder.longitude,
      aprs_enabled: Boolean(responder.aprs_enabled),
      map_label: responder.map_label || "",
      marker_color: responder.marker_color || "",
      notes: responder.notes,
      expected_updated_at: responder.updated_at
    }});
    await loadMobileState(false);
    toast("Status updated", `${displayResponder(responder)} · ${label(status)}`);
  } catch (error) {
    toast("Status was not updated", error.message, "error");
    await loadMobileState(false);
  } finally {
    button.blur();
  }
}

function bindNavigation() {
  $$(".nav-item[data-page]").forEach(button => {
    button.addEventListener("click", () => switchPage(button.dataset.page));
  });
  $$("[data-go-page]").forEach(button => {
    button.addEventListener("click", () => switchPage(button.dataset.goPage));
  });
  $("#menu-button").addEventListener("click", () => document.body.classList.add("nav-open"));
  $("#sidebar-scrim").addEventListener("click", () => document.body.classList.remove("nav-open"));

  $("#global-search").addEventListener("input", event => {
    app.search = event.target.value.trim().toLowerCase();
    renderCurrentPage();
  });
  document.addEventListener("keydown", event => {
    const target = event.target;
    const isTyping = target.matches("input, textarea, select") || target.isContentEditable;
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      $("#global-search").focus();
      return;
    }
    if (!isTyping && event.key.toLowerCase() === "n") {
      event.preventDefault();
      openIncident();
    }
  });
}

function bindMapPosition() {
  const select = $("[data-map-position]");
  if (!select) return;
  const saved = localStorage.getItem("tickets-local-map-position") || "standard";
  select.value = ["top", "higher", "standard"].includes(saved) ? saved : "standard";
  applyMapPosition(select.value);
  select.addEventListener("change", () => {
    localStorage.setItem("tickets-local-map-position", select.value);
    applyMapPosition(select.value);
  });
}

function applyMapPosition(position) {
  const panel = $("#situation-map-panel");
  const dashboard = $('[data-page-view="dashboard"]');
  const grid = $(".dashboard-grid", dashboard);
  if (!panel || !dashboard || !grid) return;
  if (position === "top") $(".stat-grid", dashboard).before(panel);
  else if (position === "higher") $(".awareness-strip", dashboard).before(panel);
  else $(".activity-panel", grid).before(panel);
}

function bindActions() {
  $("#new-incident-button").addEventListener("click", () => openIncident());
  $$("[data-new-incident]").forEach(button => button.addEventListener("click", () => openIncident()));
  $("#add-responder").addEventListener("click", () => openResponder());
  $("#add-responder-dashboard").addEventListener("click", () => openResponder());
  $$("[data-add-responder]").forEach(button => button.addEventListener("click", () => openResponder()));
  $("#add-facility").addEventListener("click", () => openFacility());
  $$("[data-add-facility]").forEach(button => button.addEventListener("click", () => openFacility()));
  $("#add-location").addEventListener("click", () => openLocation());
  $$("[data-add-location]").forEach(button => button.addEventListener("click", () => openLocation()));
  $("#add-asset").addEventListener("click", () => openAsset());
  $$("[data-add-asset]").forEach(button => button.addEventListener("click", () => openAsset()));
  $("#delete-asset").addEventListener("click", deleteAsset);
  $("#add-schedule-item").addEventListener("click", () => openScheduleItem());
  $$('[data-add-schedule-item]').forEach(button => button.addEventListener("click", () => openScheduleItem()));
  $("#delete-schedule-item").addEventListener("click", deleteScheduleItem);
  $("#add-qualification").addEventListener("click", () => openQualification());
  $$('[data-add-qualification]').forEach(button => button.addEventListener("click", () => openQualification()));
  $("#delete-qualification").addEventListener("click", deleteQualification);
  $("#use-device-location").addEventListener("click", captureDeviceLocation);
  $("#copy-owntracks-endpoint").addEventListener("click", () => copyText($("#owntracks-endpoint").textContent, "OwnTracks endpoint copied"));
  $("#copy-opengts-endpoint").addEventListener("click", () => copyText($("#opengts-endpoint").textContent, "OpenGTS endpoint copied"));
  $("#message-channel-filter").addEventListener("change", event => { app.messageChannelFilter = event.target.value; renderMessagesPage(); });
  $("#theme-button").addEventListener("click", () => {
    const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
    applyTheme(next);
    localStorage.setItem("tickets-local-theme", next);
    app.map.render(app.state);
  });
  $("#incident-status-filter").addEventListener("click", event => {
    const button = event.target.closest("button[data-filter]");
    if (!button) return;
    app.incidentFilter = button.dataset.filter;
    $$("#incident-status-filter button").forEach(item => item.classList.toggle("active", item === button));
    renderIncidentsPage();
  });
  $("#incident-severity-filter").addEventListener("change", event => {
    app.severityFilter = event.target.value;
    renderIncidentsPage();
  });
  $("#add-action-button").addEventListener("click", addIncidentAction);
  $("#upload-attachment-button").addEventListener("click", uploadIncidentAttachment);
  $("#open-incident-export").addEventListener("click", openIncidentExport);
  $("#refresh-incident-export").addEventListener("click", loadIncidentExport);
  $("#incident-export-format").addEventListener("change", loadIncidentExport);
  $("#download-incident-export").addEventListener("click", downloadIncidentExport);
  $("#new-action-text").addEventListener("keydown", event => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") addIncidentAction();
  });
  $("#geocode-button").addEventListener("click", geocodeIncident);
  $("#facility-geocode-button").addEventListener("click", geocodeFacility);
  $("#location-geocode-button").addEventListener("click", geocodeLocation);
  $("#center-geocode-button").addEventListener("click", geocodeMapCenter);
  $("#delete-location").addEventListener("click", deleteLocation);
  $("#delete-responder").addEventListener("click", deleteResponder);
  $("#position-responder-on-map").addEventListener("click", chooseResponderPositionOnMap);
  $("#incident-saved-location").addEventListener("change", event => applySavedLocation(event.target.value, $("#incident-form"), true));
  $("#facility-saved-location").addEventListener("change", event => applySavedLocation(event.target.value, $("#facility-form"), false));
  $("#add-demo-data").addEventListener("click", addDemoData);
  $("#aprs-reconnect").addEventListener("click", reconnectAPRS);
  $("#aprs-detect-audio").addEventListener("click", discoverAPRSAudioDevices);
  $("#network-copy-key").addEventListener("click", copyNetworkAccessKey);
  $("#network-regenerate-key").addEventListener("click", regenerateNetworkAccessKey);
  $("#network-test-connection").addEventListener("click", testNetworkConnection);
  $("#network-refresh-addresses").addEventListener("click", refreshNetworkAddresses);
  $("#mobile-create-enrollment").addEventListener("click", createMobileEnrollment);
  $("#mobile-device-list").addEventListener("click", event => {
    const button = event.target.closest("[data-revoke-mobile-device]");
    if (button) revokeMobileDevice(button.dataset.revokeMobileDevice);
  });
  $("#connection-test-now").addEventListener("click", () => loadConnections(true));
  $("#connection-check-list").addEventListener("click", event => {
    const button = event.target.closest("[data-connection-settings]");
    if (button) focusConnectionSettings(button.dataset.connectionSettings);
  });
  $("#network-quit-apply").addEventListener("click", quitApplication);
  $("#network-host-addresses").addEventListener("click", event => {
    const button = event.target.closest("[data-copy-network-url]");
    if (button) copyText(button.dataset.copyNetworkUrl, "Host address copied");
  });
  const aprsForm = $("#aprs-settings-form");
  aprsForm.elements.mode.addEventListener("change", updateAPRSModeControls);
  aprsForm.elements.local_decoder.addEventListener("change", updateAPRSModeControls);
  aprsForm.elements.local_igate_enabled.addEventListener("change", updateAPRSModeControls);
  const networkForm = $("#network-settings-form");
  networkForm.elements.mode.addEventListener("change", updateNetworkModeControls);
  bindSettingsDraftProtection();
  $("#weather-settings-form").addEventListener("submit", saveWeatherSettings);
  $("#water-settings-form").addEventListener("submit", saveWaterSettings);
  $("#water-site-search").addEventListener("click", discoverWaterSites);
  $("#water-site-add").addEventListener("click", addDiscoveredWaterSites);
  $("#integration-settings-form").addEventListener("submit", saveIntegrationSettings);
  $("#map-pack-form").addEventListener("submit", createMapPack);
	$("#map-pack-list").addEventListener("click", deleteMapPackFromEvent);
	$("#restore-event-file").addEventListener("change", resetRestoreValidation);
	$("#validate-restore").addEventListener("click", validateRestoreFile);
	$("#apply-restore").addEventListener("click", applyRestoreFile);
  $("#quit-application").addEventListener("click", quitApplication);
  $("#resource-tabs-toggle").addEventListener("click", toggleResourceTabs);
  $("#responders-tabs-toggle").addEventListener("click", toggleResourceTabs);
  $$('[data-toggle-secondary]').forEach(button => button.addEventListener("click", () => toggleSecondaryPanel(button.dataset.toggleSecondary)));
  $("#dashboard-resource-tabs").addEventListener("click", selectResourceType);
  $("#responders-resource-tabs").addEventListener("click", selectResourceType);
  $("#incident-update-alert-items").addEventListener("click", event => {
    const button = event.target.closest("[data-stale-incident]");
    if (!button) return;
    openIncident(app.state.incidents.find(item => item.id === button.dataset.staleIncident));
  });
  $("#incident-stage-selector").addEventListener("click", event => {
    const button = event.target.closest("[data-incident-stage]");
    if (!button) return;
    $("#incident-form").elements.status.value = button.dataset.incidentStage;
    renderIncidentStageButtons(button.dataset.incidentStage);
  });
  $("#incident-form").elements.status.addEventListener("change", event => renderIncidentStageButtons(event.target.value));
  $("#add-command-role").addEventListener("click", () => addCommandRoleRow());
  $("#command-role-list").addEventListener("click", event => {
    const button = event.target.closest("[data-remove-command-role]");
    if (button) button.closest(".command-role-row").remove();
  });
  $$("[data-map-window]").forEach(button => button.addEventListener("click", openMapWindow));
  $$("[data-map-fullscreen]").forEach(button => button.addEventListener("click", toggleDocumentFullscreen));
  $$("[data-map-close]").forEach(button => button.addEventListener("click", () => window.close()));
  document.addEventListener("fullscreenchange", updateMapFullscreenButtons);

  $("#dashboard-incidents").addEventListener("click", openIncidentFromEvent);
  $("#incidents-table").addEventListener("click", openIncidentFromEvent);
  $("#dashboard-responders").addEventListener("click", openResponderFromEvent);
  $("#responders-board").addEventListener("click", openResponderFromEvent);
  $("#facilities-grid").addEventListener("click", openFacilityFromEvent);
  $("#locations-grid").addEventListener("click", openLocationFromEvent);
  $("#assets-grid").addEventListener("click", openAssetFromEvent);
  $("#schedule-list").addEventListener("click", openScheduleItemFromEvent);
  $("#qualifications-grid").addEventListener("click", openQualificationFromEvent);
  $("#message-feed").addEventListener("click", event => { const button = event.target.closest("[data-message-incident]"); if (button) openIncident(app.state.incidents.find(item => item.id === button.dataset.messageIncident)); });
  $("#overlay-list").addEventListener("change", updateOverlayFromEvent);
  $("#overlay-list").addEventListener("click", overlayActionFromEvent);
}

function bindForms() {
  $("#incident-form").addEventListener("submit", saveIncident);
  $("#responder-form").addEventListener("submit", saveResponder);
  $("#facility-form").addEventListener("submit", saveFacility);
  $("#location-form").addEventListener("submit", saveLocation);
  $("#asset-form").addEventListener("submit", saveAsset);
  $("#schedule-form").addEventListener("submit", saveScheduleItem);
  $("#qualification-form").addEventListener("submit", saveQualification);
  $("#message-form").addEventListener("submit", saveMessage);
  $("#settings-form").addEventListener("submit", saveSettings);
  $("#network-settings-form").addEventListener("submit", saveNetworkSettings);
  $("#aprs-settings-form").addEventListener("submit", saveAPRSSettings);
  $("#overlay-import-form").addEventListener("submit", importKML);
  bindCoordinateInvalidation($("#incident-form"), ["address", "city", "region", "postal_code"], $("#geocode-results"));
  bindCoordinateInvalidation($("#facility-form"), ["address"], $("#facility-geocode-results"));
  bindCoordinateInvalidation($("#location-form"), ["address", "city", "region", "postal_code"], $("#location-geocode-results"));
}

function bindSettingsDraftProtection() {
  const forms = {
    network: "#network-settings-form",
    general: "#settings-form",
    aprs: "#aprs-settings-form",
    weather: "#weather-settings-form",
    water: "#water-settings-form",
    integrations: "#integration-settings-form"
  };
  Object.entries(forms).forEach(([key, selector]) => {
    const form = $(selector);
    form.addEventListener("input", () => { app.settingsFormDirty[key] = true; });
    form.addEventListener("change", () => { app.settingsFormDirty[key] = true; });
  });
}

function bindCoordinateInvalidation(form, fieldNames, results) {
  fieldNames.forEach(name => form.elements[name].addEventListener("input", () => {
    form.elements.latitude.value = "";
    form.elements.longitude.value = "";
    results.classList.remove("visible");
  }));
}

function bindDialogs() {
  $$("dialog").forEach(dialog => {
    $$(".dialog-close", dialog).forEach(button => button.addEventListener("click", () => dialog.close()));
    dialog.addEventListener("click", event => {
      if (event.target === dialog) dialog.close();
    });
  });
}

function switchPage(page) {
  app.page = page;
  $$(".page").forEach(view => view.classList.toggle("active", view.dataset.pageView === page));
  $$(".nav-item[data-page]").forEach(button => button.classList.toggle("active", button.dataset.page === page));
  $("#page-title").textContent = pageTitles[page] || "Tickets Local";
  document.body.classList.remove("nav-open");
  renderCurrentPage();
  window.scrollTo({ top: 0, behavior: "smooth" });
}

async function loadState(initial = false) {
  const requestSequence = ++app.stateRequestSequence;
  try {
    const state = await api("/api/state");
    if (requestSequence < app.stateAppliedSequence) return;
    app.stateAppliedSequence = requestSequence;
    state.weather_alerts = app.weather?.alerts || app.state.weather_alerts || [];
    app.state = state;
    renderAll();
  } catch (error) {
    setConnectionState(false);
    if (initial) {
      const source = app.network.active_mode === "client" ? "host data" : "local data";
      toast(`Could not load ${source}`, error.message, "error");
    }
  }
}

async function loadNetworkSettings() {
  try {
    app.network = await api("/api/network/settings");
    renderNetworkSettings();
  } catch (error) {
    toast("Could not load network settings", error.message, "error");
  }
}

async function loadConnections(showToast = false) {
  const button = $("#connection-test-now");
  if (button) button.disabled = true;
  try {
    app.connections = await api("/api/connections");
    renderConnectionTester();
    if (showToast) toast("Connection test complete", app.connections.overall === "connected" ? "All enabled connections responded." : "Review the highlighted connection results.");
  } catch (error) {
    app.connections = {
      checked_at: new Date().toISOString(),
      overall: "attention",
      items: [{ id: "lan-host", label: "LAN Host", state: "disconnected", detail: error.message }]
    };
    renderConnectionTester();
    if (showToast) toast("Connection test could not complete", error.message, "error");
  } finally {
    if (button) button.disabled = false;
  }
}

async function loadWeather(initial = false) {
  try {
    app.weather = await api("/api/weather");
    app.state.weather_alerts = app.weather.alerts || [];
    renderDashboardWeather();
    renderWeatherSettings();
    app.map.render(app.state);
  } catch (error) {
    if (initial) toast("Could not load weather", error.message, "error");
  }
}

async function loadWater(initial = false) {
  try {
    app.water = await api("/api/water");
    renderDashboardWater();
    renderWaterSettings();
  } catch (error) {
    if (initial) toast("Could not load water gauges", error.message, "error");
  }
}
async function loadIntegrations(initial=false){try{app.integrations=await api("/api/integrations");renderIntegrationSettings();app.map.render(app.state)}catch(error){if(initial)toast("Could not load operational feeds",error.message,"error")}}

function renderAll() {
  $("#organization-name").textContent = app.state.settings.organization;
  $("#nav-incident-count").textContent = activeIncidents().length;
  renderDashboard();
  renderIncidentsPage();
  renderRespondersPage();
  renderFacilitiesPage();
  renderLocationsPage();
  renderAssetsPage();
  renderSchedulePage();
  renderQualificationsPage();
  renderMessagesPage();
  renderActivityPage();
  renderSettings();
  renderNetworkSettings();
  renderMobileCompanion();
}

function renderCurrentPage() {
  if (app.page === "dashboard") renderDashboard();
  if (app.page === "incidents") renderIncidentsPage();
  if (app.page === "responders") renderRespondersPage();
  if (app.page === "facilities") renderFacilitiesPage();
  if (app.page === "locations") renderLocationsPage();
  if (app.page === "activity") renderActivityPage();
  if (app.page === "assets") renderAssetsPage();
  if (app.page === "schedule") renderSchedulePage();
  if (app.page === "qualifications") renderQualificationsPage();
  if (app.page === "messages") renderMessagesPage();
  if (app.page === "settings") {
    renderSettings();
    renderNetworkSettings();
  }
}

function renderDashboard() {
  const incidents = activeIncidents();
  const responders = [...app.state.responders].sort((a, b) => a.status.localeCompare(b.status) || displayResponder(a).localeCompare(displayResponder(b)));
  const stale = incidents.filter(isIncidentStale);
  const facilities = app.state.facilities;
  const available = responders.filter(item => item.status === "available").length;
  const openFacilities = facilities.filter(item => item.status === "open").length;
  const capacity = facilities.reduce((sum, item) => sum + item.capacity, 0);
  const occupied = facilities.reduce((sum, item) => sum + item.occupied, 0);

  $("#stat-active").textContent = incidents.length;
  $("#stat-critical").textContent = `${incidents.filter(item => item.severity === "critical").length} critical · ${stale.length} need update`;
  $("#stat-available").textContent = available;
  $("#stat-units").textContent = `${responders.length} total responder${responders.length === 1 ? "" : "s"}`;
  $("#stat-facilities").textContent = openFacilities;
  $("#stat-capacity").textContent = capacity ? `${Math.max(0, capacity - occupied)} of ${capacity} spaces available` : "No capacity reported";
  $("#stat-oldest").textContent = incidents.length ? elapsed(incidents.reduce((oldest, item) => new Date(item.created_at) < new Date(oldest.created_at) ? item : oldest).created_at) : "—";
  $("#situation-summary").textContent = incidents.length
    ? `${incidents.length} active incident${incidents.length === 1 ? "" : "s"} with ${available} unit${available === 1 ? "" : "s"} available.${stale.length ? ` ${stale.length} need an update.` : ""}`
    : "No active incidents. System ready.";
  renderIncidentUpdateAlert(stale);

  const dashboardIncidents = incidents.filter(matchesSearch).slice(0, 6);
  $("#dashboard-incidents").innerHTML = dashboardIncidents.map(incidentRow).join("");
  $("#dashboard-empty").classList.toggle("visible", dashboardIncidents.length === 0);
  $("#dashboard-incidents").closest(".table-wrap").hidden = dashboardIncidents.length === 0;

  const matchingResponders = responders.filter(matchesSearch);
  const resourceTypes = [...new Set(matchingResponders.map(responderType))].sort((a, b) => a.localeCompare(b));
  if (app.resourceTypeFilter !== "all" && !resourceTypes.includes(app.resourceTypeFilter)) app.resourceTypeFilter = "all";
  renderResourceTabs($("#dashboard-resource-tabs"), resourceTypes);
  $("#resource-tabs-toggle").textContent = app.resourceTabsEnabled ? "Type tabs: On" : "Group by type";
  const dashboardResponders = matchingResponders
    .filter(item => !app.resourceTabsEnabled || app.resourceTypeFilter === "all" || responderType(item) === app.resourceTypeFilter)
    .slice(0, 7);
  $("#dashboard-responders").innerHTML = dashboardResponders.length
    ? dashboardResponders.map(responderRow).join("")
    : app.resourceTabsEnabled && matchingResponders.length
      ? `<div class="empty-state visible"><p>No responders in this type.</p></div>`
      : `<div class="empty-state visible"><p>No responders configured.</p><button class="secondary-button" data-add-responder-inline>Add responder</button></div>`;
  const inline = $("[data-add-responder-inline]");
  if (inline) inline.addEventListener("click", () => openResponder());

  $("#dashboard-activity").innerHTML = app.state.activity.slice(0, 7).map(activityItem).join("");
  renderDashboardAPRS();
  renderDashboardWeather();
  renderDashboardWater();
  app.map.render(app.state);
}

function renderDashboardWater() {
  const water = app.water || { state: "disabled", message: "Water monitoring is off", gauges: [] };
  const state = $("#dashboard-water-state"); if (!state) return;
  state.className = `aprs-state ${water.state === "current" ? "connected" : water.state}`;
  state.textContent = water.state === "current" ? "Current" : label(water.state);
  $("#dashboard-water-message").textContent = water.message;
  $("#dashboard-water-updated").textContent = water.updated_at ? `${water.source} · updated ${relativeTime(water.updated_at)}${water.stale ? " · cached" : ""}` : "Configure gauge IDs under Settings.";
  $("#dashboard-water-gauges").innerHTML = (water.gauges || []).map(gauge => {
    const floodLevel = waterGaugeFloodLevel(gauge);
    const readings = [];
    if (gauge.stage_feet != null) readings.push(`Stage ${gauge.stage_feet.toFixed(2)} ft · ${label(gauge.stage_trend || "steady")}`);
    if (gauge.flow_cfs != null) readings.push(`Flow ${Math.round(gauge.flow_cfs).toLocaleString()} cfs · ${label(gauge.flow_trend || "steady")}`);
    if (gauge.flood_category) readings.push(`Now: ${label(gauge.flood_category)}`);
    if (gauge.forecast_flood_category) readings.push(`Forecast: ${label(gauge.forecast_flood_category)}`);
    if (gauge.forecast_stage_feet != null) readings.push(`Forecast crest ${gauge.forecast_stage_feet.toFixed(2)} ft`);
    return `<article class="water-gauge flood-${attr(floodLevel)}"><div><strong>${html(gauge.name || gauge.site_id)}</strong><span class="flood-badge">${html(waterFloodLabel(floodLevel))}</span></div><small>${html(readings.join(" · ") || "No current reading")} · ${gauge.observed_at ? html(relativeTime(gauge.observed_at)) : "unknown age"}${gauge.provisional ? " · provisional" : ""}</small></article>`;
  }).join("") || `<div class="weather-clear">No configured gauge readings.</div>`;
}

function waterGaugeFloodLevel(gauge) {
  const rank = { major: 4, moderate: 3, minor: 2, action: 1, normal: 0, none: 0, "not defined": 0 };
  const levels = [gauge.flood_category, gauge.forecast_flood_category]
    .map(value => String(value || "normal").toLowerCase())
    .map(value => value.includes("major") ? "major" : value.includes("moderate") ? "moderate" : value.includes("minor") ? "minor" : value.includes("action") ? "action" : "normal");
  return levels.sort((a, b) => rank[b] - rank[a])[0] || "normal";
}

function waterFloodLabel(level) {
  return ({ major: "Major flood", moderate: "Moderate flood", minor: "Minor flood", action: "Action stage", normal: "Normal" })[level] || "Normal";
}

function renderDashboardWeather() {
  const weather = app.weather || { state: "disabled", message: "Weather awareness is off", alerts: [] };
  const state = $("#dashboard-weather-state");
  if (!state) return;
  const stateClass = weather.state === "current" ? "connected" : weather.state;
  state.className = `aprs-state ${stateClass}`;
  state.textContent = weather.state === "current" ? "Current" : label(weather.state);
  $("#dashboard-weather-message").textContent = weather.message;
  $("#dashboard-weather-updated").textContent = weather.updated_at
    ? `${weather.source || "Weather source"} · updated ${relativeTime(weather.updated_at)}${weather.stale ? " · cached" : ""}`
    : "Enable it under Settings to monitor the configured map home.";
  const alerts = weather.alerts || [];
  if (alerts.some(alert => ["extreme", "severe"].includes(alert.severity))) setSecondaryPanel("weather", true);
  $("#dashboard-weather-alerts").innerHTML = alerts.length
    ? alerts.slice(0, 6).map(alert => `<article class="weather-alert ${attr(alert.severity || "unknown")}">
        <div><strong>${html(alert.event)}</strong><small>${html(alert.area || alert.headline || "Configured map home")}</small></div>
        <span>${alert.expires ? `Expires ${html(formatDateTime(alert.expires))}` : label(alert.severity)}</span>
      </article>`).join("")
    : `<div class="weather-clear">${weather.state === "disabled" ? "Weather monitoring is disabled." : "No active alerts reported."}</div>`;
  const current = weather.current;
  const currentPanel = $("#dashboard-current-weather");
  currentPanel.hidden = !current;
  currentPanel.innerHTML = current ? `<strong>${current.temperature_f == null ? "—" : `${Math.round(current.temperature_f)}°F`}</strong><span>${html(current.description || "Current conditions")}<br>${current.humidity_percent == null ? "" : `${Math.round(current.humidity_percent)}% humidity · `}${current.wind_speed_mph == null ? "" : `${Math.round(current.wind_speed_mph)} mph wind · `}${current.visibility_miles == null ? "" : `${current.visibility_miles.toFixed(1)} mi visibility · `}${current.observed_at ? `observed ${html(relativeTime(current.observed_at))}` : ""}</span>` : "";
}

function toggleSecondaryPanel(name) {
  const panel = $(`[data-secondary-panel="${name}"]`);
  setSecondaryPanel(name, !panel.classList.contains("expanded"));
}

function setSecondaryPanel(name, expanded) {
  const panel = $(`[data-secondary-panel="${name}"]`); const button = $(`[data-toggle-secondary="${name}"]`);
  if (!panel || !button) return; panel.classList.toggle("expanded", expanded); button.setAttribute("aria-expanded",String(expanded)); button.textContent=expanded?"Hide":"Details";
}

function renderDashboardAPRS() {
  const status = app.state.aprs_status || { state: "disabled", message: "APRS tracking is off" };
  const settings = app.state.settings.aprs || {};
  const local = status.local || { state: "disabled", message: "Local RF reception is off" };
  const overall = aprsOverallStatus(settings, status, local);
  const state = $("#dashboard-aprs-state");
  state.className = `aprs-state ${overall.state}`;
  state.textContent = overall.state === "connected" ? "Live" : label(overall.state);
  $("#dashboard-aprs-message").textContent = overall.message;
  const verifiedLogin = status.login_verified ? `Verified as ${settings.login_callsign}` : "";
  const details = [];
  if (settings.mode !== "local" || settings.local?.igate_enabled) {
    details.push(status.server
      ? `Internet: ${verifiedLogin ? `${verifiedLogin} · ` : ""}${status.server}`
      : "Internet: not configured");
  }
  if (settings.mode !== "internet") {
    details.push(`Local: ${local.decoder === "kiss_tcp" ? local.kiss_address || "KISS TNC" : "built-in Dire Wolf"}`);
  }
  $("#dashboard-aprs-server").textContent = details.join(" · ") || "Enable APRS under Settings to begin receiving.";
  $("#dashboard-aprs-last-packet").textContent = status.last_packet_at ? relativeTime(status.last_packet_at) : "Never";
  $("#dashboard-aprs-packets").textContent = status.packets_received || 0;
  $("#dashboard-aprs-local-last-packet").textContent = local.last_packet_at ? relativeTime(local.last_packet_at) : "Never";
  $("#dashboard-aprs-local-packets").textContent = local.packets_received || 0;
  $("#dashboard-aprs-decoded").textContent = (status.position_packets_decoded || 0) + (local.position_packets_decoded || 0);
  $("#dashboard-aprs-updates").textContent = (status.positions_updated || 0) + (local.positions_updated || 0);
  $("#dashboard-aprs-area").textContent = settings.area_enabled || settings.local?.show_all ? `${status.area_stations || 0} active` : "Off";
  $("#dashboard-aprs-igate").textContent = settings.local?.igate_enabled ? (local.igate_verified ? "Verified" : "Waiting") : "Off";
}

function renderIncidentUpdateAlert(stale = activeIncidents().filter(isIncidentStale)) {
  const alert = $("#incident-update-alert");
  alert.hidden = stale.length === 0;
  if (!stale.length) {
    $("#incident-update-alert-items").replaceChildren();
    return;
  }
  const minutes = incidentStaleMinutes();
  $("#incident-update-alert-title").textContent = `${stale.length} active incident${stale.length === 1 ? "" : "s"} need an update`;
  $("#incident-update-alert-detail").textContent = `No saved change or action note within the ${minutes}-minute alert interval.`;
  $("#incident-update-alert-items").innerHTML = stale
    .sort((a, b) => new Date(a.updated_at) - new Date(b.updated_at))
    .slice(0, 6)
    .map(item => `<button type="button" data-stale-incident="${attr(item.id)}">#${item.number} · ${html(item.title)} <span>${html(relativeTime(item.updated_at))}</span></button>`)
    .join("");
}

function renderIncidentsPage() {
  let incidents = [...app.state.incidents].sort(sortIncidents);
  if (app.incidentFilter === "active") incidents = incidents.filter(item => activeStatuses.has(item.status));
  if (app.incidentFilter === "closed") incidents = incidents.filter(item => !activeStatuses.has(item.status));
  if (app.severityFilter) incidents = incidents.filter(item => item.severity === app.severityFilter);
  incidents = incidents.filter(matchesSearch);
  $("#incidents-table").innerHTML = incidents.map(fullIncidentRow).join("");
  $("#incidents-empty").classList.toggle("visible", incidents.length === 0);
  $("#incidents-table").closest(".table-wrap").hidden = incidents.length === 0;
}

function renderRespondersPage() {
  const matchingResponders = [...app.state.responders]
    .sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)))
    .filter(matchesSearch);
  const resourceTypes = [...new Set(matchingResponders.map(responderType))].sort((a, b) => a.localeCompare(b));
  if (app.resourceTypeFilter !== "all" && !resourceTypes.includes(app.resourceTypeFilter)) app.resourceTypeFilter = "all";
  renderResourceTabs($("#responders-resource-tabs"), resourceTypes);
  $("#responders-tabs-toggle").textContent = app.resourceTabsEnabled ? "Type tabs: On" : "Group by type";
  const responders = matchingResponders.filter(item =>
    !app.resourceTabsEnabled || app.resourceTypeFilter === "all" || responderType(item) === app.resourceTypeFilter
  );
  $("#responders-board").innerHTML = responders.map(responderCard).join("");
  $("#responders-empty").classList.toggle("visible", matchingResponders.length === 0);
}

function toggleResourceTabs() {
  app.resourceTabsEnabled = !app.resourceTabsEnabled;
  app.resourceTypeFilter = "all";
  localStorage.setItem("tickets-local-resource-tabs", String(app.resourceTabsEnabled));
  renderDashboard();
  renderRespondersPage();
}

function selectResourceType(event) {
  const button = event.target.closest("[data-resource-type]");
  if (!button) return;
  app.resourceTypeFilter = button.dataset.resourceType;
  renderDashboard();
  renderRespondersPage();
}

function renderResourceTabs(container, resourceTypes) {
  container.hidden = !app.resourceTabsEnabled;
  container.innerHTML = app.resourceTabsEnabled
    ? ["all", ...resourceTypes].map(type => `<button type="button" class="resource-tab${app.resourceTypeFilter === type ? " active" : ""}" data-resource-type="${attr(type)}">${html(type === "all" ? "All" : type)}</button>`).join("")
    : "";
}

function renderFacilitiesPage() {
  const facilities = [...app.state.facilities]
    .sort((a, b) => a.name.localeCompare(b.name))
    .filter(matchesSearch);
  $("#facilities-grid").innerHTML = facilities.map(facilityCard).join("");
  $("#facilities-empty").classList.toggle("visible", facilities.length === 0);
}

function renderLocationsPage() {
  const locations = [...(app.state.locations || [])]
    .sort((a, b) => a.name.localeCompare(b.name))
    .filter(matchesSearch);
  $("#locations-grid").innerHTML = locations.map(locationCard).join("");
  $("#locations-empty").classList.toggle("visible", locations.length === 0);
}

function renderAssetsPage() {
  const assets = [...(app.state.assets || [])]
    .sort((a, b) => a.category.localeCompare(b.category) || a.name.localeCompare(b.name))
    .filter(matchesSearch);
  $("#assets-grid").innerHTML = assets.map(assetCard).join("");
  $("#assets-empty").classList.toggle("visible", assets.length === 0);
}

function assetCard(asset) {
  const due = asset.maintenance_due
    ? "Maintenance " + (new Date(asset.maintenance_due) < new Date() ? "overdue" : "due " + new Date(asset.maintenance_due).toLocaleDateString())
    : "No maintenance date";
  return '<article class="location-card asset-card ' + attr(asset.status) + '" data-asset-id="' + attr(asset.id) + '">' +
    '<div class="location-card-header"><div><h3>' + html(asset.name) + '</h3><p>' + html(label(asset.category)) + (asset.identifier ? " · " + html(asset.identifier) : "") + '</p></div><span class="badge status-' + attr(asset.status) + '">' + html(label(asset.status)) + '</span></div>' +
    '<p>' + asset.quantity.toLocaleString() + ' available · ' + html(asset.location || "Location not recorded") + '</p>' +
    '<span class="location-coordinates">' + html(asset.custodian || "No custodian") + " · " + html(due) + '</span>' +
    '<div class="card-footer"><span>' + html(asset.notes || "No notes") + '</span><span>Updated ' + html(relativeTime(asset.updated_at)) + '</span></div></article>';
}

function renderSchedulePage() {
  const items = [...(app.state.schedule || [])]
    .sort((a, b) => new Date(a.start_at) - new Date(b.start_at))
    .filter(matchesSearch);
  $("#schedule-list").innerHTML = items.map(scheduleCard).join("");
  $("#schedule-empty").classList.toggle("visible", items.length === 0);
}

function scheduleCard(item) {
  const start = new Date(item.start_at);
  const end = new Date(item.end_at);
  const assigned = (item.assigned_responder_ids || []).map(id => {
    const responder = (app.state.responders || []).find(candidate => candidate.id === id);
    return responder ? displayResponder(responder) : "Unavailable responder";
  });
  const sameDay = start.toDateString() === end.toDateString();
  const when = start.toLocaleString([], { dateStyle: "medium", timeStyle: "short" }) + " – " +
    end.toLocaleString([], sameDay ? { timeStyle: "short" } : { dateStyle: "medium", timeStyle: "short" });
  return '<article class="schedule-card ' + attr(item.status) + '" data-schedule-id="' + attr(item.id) + '">' +
    '<div class="schedule-time"><strong>' + html(start.toLocaleDateString([], { month: "short", day: "numeric" })) + '</strong><span>' + html(start.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })) + '</span></div>' +
    '<div class="schedule-details"><div class="location-card-header"><div><h3>' + html(item.title) + '</h3><p>' + html(label(item.type)) + '</p></div><span class="badge status-' + attr(item.status) + '">' + html(label(item.status)) + '</span></div>' +
    '<p class="schedule-when">' + html(when) + (item.location ? ' · ' + html(item.location) : '') + '</p>' +
    '<div class="card-footer"><span>' + html(assigned.length ? assigned.join(", ") : item.coordinator || "No personnel assigned") + '</span><span>Updated ' + html(relativeTime(item.updated_at)) + '</span></div></div></article>';
}

function renderQualificationsPage() {
  const items = [...(app.state.qualifications || [])]
    .sort((a, b) => qualificationRank(a) - qualificationRank(b) || qualificationResponder(a).localeCompare(qualificationResponder(b)) || a.name.localeCompare(b.name))
    .filter(matchesSearch);
  $("#qualifications-grid").innerHTML = items.map(qualificationCard).join("");
  $("#qualifications-empty").classList.toggle("visible", items.length === 0);
  const all = app.state.qualifications || [];
  const expiring = all.filter(item => qualificationEffectiveStatus(item) === "expiring").length;
  const expired = all.filter(item => qualificationEffectiveStatus(item) === "expired").length;
  $("#qualification-summary").innerHTML = '<span><strong>' + all.length + '</strong> total</span><span><strong>' + expiring + '</strong> expiring within 60 days</span><span class="' + (expired ? "warning" : "") + '"><strong>' + expired + '</strong> expired</span>';
}

function qualificationResponder(item) {
  const responder = (app.state.responders || []).find(candidate => candidate.id === item.responder_id);
  return responder ? displayResponder(responder) : "Unavailable responder";
}

function qualificationEffectiveStatus(item) {
  if (item.status === "revoked" || item.status === "pending") return item.status;
  if (!item.expires_at) return item.status || "current";
  const days = (new Date(item.expires_at).getTime() - Date.now()) / 86400000;
  if (days < 0) return "expired";
  if (days <= 60) return "expiring";
  return item.status || "current";
}

function qualificationRank(item) {
  return { expired: 0, revoked: 1, expiring: 2, pending: 3, current: 4 }[qualificationEffectiveStatus(item)] ?? 5;
}

function qualificationCard(item) {
  const status = qualificationEffectiveStatus(item);
  const dates = [];
  if (item.completed_at) dates.push("Completed " + new Date(item.completed_at).toLocaleDateString());
  if (item.expires_at) dates.push("Expires " + new Date(item.expires_at).toLocaleDateString());
  return '<article class="location-card qualification-card ' + attr(status) + '" data-qualification-id="' + attr(item.id) + '"><div class="location-card-header"><div><h3>' + html(item.name) + '</h3><p>' + html(qualificationResponder(item)) + ' · ' + html(label(item.category)) + '</p></div><span class="badge status-' + attr(status) + '">' + html(label(status)) + '</span></div><p>' + html(item.provider || "Provider not recorded") + (item.credential_id ? ' · ' + html(item.credential_id) : '') + '</p><span class="location-coordinates">' + html(dates.join(" · ") || "No dates recorded") + '</span><div class="card-footer"><span>' + html(item.notes || "No notes") + '</span><span>Updated ' + html(relativeTime(item.updated_at)) + '</span></div></article>';
}

function renderMessagesPage() {
  const messages = [...(app.state.messages || [])]
    .reverse()
    .filter(item => app.messageChannelFilter === "all" || item.channel === app.messageChannelFilter)
    .filter(matchesSearch);
  $("#nav-message-count").textContent = Math.min((app.state.messages || []).length, 99);
  $("#message-feed").innerHTML = messages.map(messageItem).join("");
  $("#messages-empty").classList.toggle("visible", messages.length === 0);
  const select = $("#message-form").elements.incident_id;
  const selected = select.value;
  select.innerHTML = '<option value="">No incident</option>' + [...app.state.incidents].sort((a, b) => b.number - a.number).map(incident => '<option value="' + attr(incident.id) + '">#' + incident.number + ' · ' + html(incident.title) + '</option>').join("");
  select.value = selected;
}

function messageItem(message) {
  const incident = message.incident_id ? app.state.incidents.find(item => item.id === message.incident_id) : null;
  return '<div class="message-item channel-' + attr(message.channel) + '"><div class="message-meta"><span class="badge">' + html(label(message.channel)) + '</span><strong>' + html(message.author || "Operator") + '</strong><time>' + html(formatDateTime(message.created_at)) + '</time></div><p>' + html(message.body) + '</p>' + (incident ? '<button type="button" class="text-button" data-message-incident="' + attr(incident.id) + '">Incident #' + incident.number + ' · ' + html(incident.title) + '</button>' : '') + '</div>';
}

async function saveMessage(event) {
  event.preventDefault();
  const form = event.currentTarget;
  setFormBusy(form, true);
  try {
    await api("/api/messages", { method: "POST", body: { channel: form.elements.channel.value, incident_id: form.elements.incident_id.value, author: form.elements.author.value, body: form.elements.body.value } });
    const author = form.elements.author.value;
    const channel = form.elements.channel.value;
    form.reset();
    form.elements.author.value = author;
    form.elements.channel.value = channel;
    toast("Message posted", label(channel));
    await loadState();
  } catch (error) {
    toast("Message was not posted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function renderActivityPage() {
  const activity = app.state.activity.filter(matchesSearch);
  $("#full-activity").innerHTML = activity.map(activityItem).join("");
  $("#activity-empty").classList.toggle("visible", activity.length === 0);
}

function renderSettings() {
  const form = $("#settings-form");
  if (!app.settingsFormDirty.general) {
    form.elements.organization.value = app.state.settings.organization;
    form.elements.center_address.value = app.state.settings.center_address || "";
    form.elements.center_lat.value = app.state.settings.center_lat;
    form.elements.center_lon.value = app.state.settings.center_lon;
    form.elements.default_zoom.value = app.state.settings.default_zoom;
    form.elements.incident_stale_minutes.value = app.state.settings.incident_stale_minutes || 10;
  }
  renderOverlayList();
  const aprsForm = $("#aprs-settings-form");
  const settings = app.state.settings.aprs;
  if (!app.settingsFormDirty.aprs) {
    aprsForm.elements.enabled.checked = settings.enabled;
    aprsForm.elements.mode.value = settings.mode || "internet";
    aprsForm.elements.login_callsign.value = settings.login_callsign;
    aprsForm.elements.server.value = settings.server;
    aprsForm.elements.extra_filter.value = settings.extra_filter;
    aprsForm.elements.watch_callsigns.value = (settings.watch_callsigns || []).join(", ");
    aprsForm.elements.stale_minutes.value = settings.stale_minutes;
    aprsForm.elements.trail_hours.value = settings.trail_hours;
    aprsForm.elements.area_enabled.checked = settings.area_enabled;
    aprsForm.elements.area_radius_miles.value = settings.area_radius_miles || 25;
    aprsForm.elements.local_decoder.value = settings.local?.decoder || "bundled";
    aprsForm.elements.local_kiss_address.value = settings.local?.kiss_address || "127.0.0.1:8001";
    populateAPRSAudioDeviceSelectors(
      settings.local?.audio_device || "",
      settings.local?.audio_output_device || ""
    );
    aprsForm.elements.local_show_all.checked = settings.local?.show_all !== false;
    aprsForm.elements.local_igate_enabled.checked = Boolean(settings.local?.igate_enabled);
    aprsForm.elements.passcode.value = "";
  }
  $("#aprs-passcode-help").textContent = settings.passcode_configured
    ? "A passcode is stored separately from backups on the active data host. Leave blank to keep it."
    : "Required for APRS-IS or local iGate operation; stored separately from backups on the active data host.";
  updateAPRSModeControls();
  renderAPRSStatus();
  renderWeatherSettings();
  renderWaterSettings();
  renderIntegrationSettings();
  renderMapPacks();
}

async function loadMapPacks(initial = false) {
  try {
    app.mapPacks = await api("/api/map/packs");
    renderMapPacks();
  } catch (error) {
    if (initial) toast("Could not load offline maps", error.message, "error");
  }
}

function renderMapPacks() {
  const list = $("#map-pack-list");
  if (!list) return;
  list.innerHTML = app.mapPacks.length ? app.mapPacks.map(pack => `
    <div class="map-pack-row" data-map-pack-id="${attr(pack.id)}">
      <div><strong>${html(pack.name)}</strong><small>${pack.radius_miles} mi · zoom ${pack.min_zoom}–${pack.max_zoom} · ${pack.tile_count.toLocaleString()} tiles · ${formatFileSize(pack.bytes)}</small></div>
      <button class="row-action" type="button" data-delete-map-pack aria-label="Remove ${attr(pack.name)}">Remove</button>
    </div>`).join("") : `<div class="overlay-empty">No saved map packs. Tiles you view are still cached automatically.</div>`;
}

async function createMapPack(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector("button[type='submit']");
  const progress = $("#map-pack-progress");
  button.disabled = true;
  progress.textContent = "Downloading map tiles. Keep Tickets Local running until this finishes…";
  try {
    const pack = await api("/api/map/packs", { method: "POST", body: {
      name: form.elements.name.value,
      center_lat: app.state.settings.center_lat,
      center_lon: app.state.settings.center_lon,
      radius_miles: Number(form.elements.radius_miles.value),
      min_zoom: Number(form.elements.min_zoom.value),
      max_zoom: Number(form.elements.max_zoom.value)
    }});
    progress.textContent = `${pack.name} is ready offline: ${pack.tile_count.toLocaleString()} tiles, ${formatFileSize(pack.bytes)}.`;
    await loadMapPacks();
    toast("Offline map ready", pack.name);
  } catch (error) {
    progress.textContent = error.message;
    toast("Map pack failed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function deleteMapPackFromEvent(event) {
  const button = event.target.closest("[data-delete-map-pack]");
  if (!button) return;
  const row = button.closest("[data-map-pack-id]");
  const pack = app.mapPacks.find(item => item.id === row?.dataset.mapPackId);
  if (!pack || !confirm(`Remove the saved map-pack record “${pack.name}”? Shared cached tiles remain available.`)) return;
  await api(`/api/map/packs/${encodeURIComponent(pack.id)}`, { method: "DELETE" });
  await loadMapPacks();
  toast("Map pack removed", pack.name);
}

function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function renderWaterSettings() {
  const form = $("#water-settings-form"); if (!form) return;
  const settings = app.state.settings.water || { enabled:false, site_ids:"", refresh_minutes:15, source_url:"", noaa_enabled:false, noaa_url:"" };
  if (!app.settingsFormDirty.water) { form.elements.enabled.checked=Boolean(settings.enabled); form.elements.site_ids.value=settings.site_ids||""; form.elements.refresh_minutes.value=settings.refresh_minutes||15; form.elements.source_url.value=settings.source_url||""; form.elements.noaa_enabled.checked=Boolean(settings.noaa_enabled); form.elements.noaa_url.value=settings.noaa_url||""; }
  const water=app.water||{state:"disabled",message:"Water monitoring is off"}; const state=$("#water-state"); state.className=`aprs-state ${water.state === "current" ? "connected" : water.state}`; state.textContent=water.state === "current" ? "Current" : label(water.state); $("#water-runtime").textContent=water.updated_at?`${water.message} Last successful update ${formatDateTime(water.updated_at)}.`:water.message;
}

async function discoverWaterSites() {
  const button = $("#water-site-search");
  const results = $("#water-site-results");
  const addButton = $("#water-site-add");
  const latitude = Number(app.state.settings.center_lat);
  const longitude = Number(app.state.settings.center_lon);
  const radius = Number($("#water-site-radius").value);
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) {
    toast("Gauge search unavailable", "Configure the map home location first.", "error");
    return;
  }
  button.disabled = true;
  addButton.hidden = true;
  results.innerHTML = `<span>Searching active USGS stream gauges…</span>`;
  try {
    const response = await api(`/api/water/sites?${new URLSearchParams({lat: latitude, lon: longitude, radius})}`);
    const sites = response.sites || [];
    results.innerHTML = sites.map(site => `<label class="gauge-site-result"><input type="checkbox" value="${attr(site.site_id)}"><span><strong>${html(site.name || `USGS ${site.site_id}`)}</strong><small>${html(site.distance_miles.toFixed(1))} miles from map home · USGS ${html(site.site_id)}</small></span></label>`).join("") || `<span>No active USGS stream gauges were found within ${radius} miles.</span>`;
    addButton.hidden = sites.length === 0;
  } catch (error) {
    results.innerHTML = `<span>Gauge search failed: ${html(error.message)}</span>`;
    toast("Gauge search failed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

function addDiscoveredWaterSites() {
  const form = $("#water-settings-form");
  const selected = $$("#water-site-results input:checked").map(input => input.value);
  if (!selected.length) {
    toast("No gauges selected", "Choose one or more named stations first.", "error");
    return;
  }
  const existing = form.elements.site_ids.value.split(/[\s,;]+/).filter(Boolean);
  form.elements.site_ids.value = [...new Set([...existing, ...selected])].slice(0, 25).join(", ");
  app.settingsFormDirty.water = true;
  form.elements.site_ids.dispatchEvent(new Event("input", {bubbles: true}));
  toast("Gauges added", `${selected.length} station${selected.length === 1 ? "" : "s"} ready to save.`);
}
function renderIntegrationSettings(){const form=$("#integration-settings-form");if(!form)return;const settings=app.state.settings.integrations||{enabled:false,refresh_minutes:5};if(!app.settingsFormDirty.integrations){for(const name of ["storm_reports_url","infrastructure_url","amateur_repeaters_url","gmrs_repeaters_url","meshcore_url","aredn_node_urls","sensor_urls"])form.elements[name].value=settings[name]||"";form.elements.amateur_osm_enabled.checked=Boolean(settings.amateur_osm_enabled);form.elements.enabled.checked=Boolean(settings.enabled);form.elements.refresh_minutes.value=settings.refresh_minutes||5}const status=app.integrations||{state:"disabled",message:"External operational feeds are off",aredn_nodes:[]};const state=$("#integration-state");state.className=`aprs-state ${status.state==="current"?"connected":status.state}`;state.textContent=status.state==="current"?"Current":label(status.state);const nodes=(status.aredn_nodes||[]).map(node=>`${node.name}: ${node.reachable?`${node.latency_ms} ms${node.route_cost==null?"":` · cost ${node.route_cost}`}`:"unreachable"}`).join(" · ");$("#integration-runtime").textContent=`${status.message}${nodes?` · ${nodes}`:""}`}

function renderWeatherSettings() {
  const form = $("#weather-settings-form");
  if (!form) return;
  const settings = app.state.settings.weather || { enabled: false, refresh_minutes: 5, radar_enabled: false, radar_opacity: 55, radar_animation: false, radar_frames: 4 };
  if (!app.settingsFormDirty.weather) {
    form.elements.enabled.checked = Boolean(settings.enabled);
    form.elements.refresh_minutes.value = settings.refresh_minutes || 5;
    form.elements.radar_enabled.checked = Boolean(settings.radar_enabled);
    form.elements.radar_opacity.value = settings.radar_opacity || 55;
    form.elements.radar_animation.checked = Boolean(settings.radar_animation);
    form.elements.radar_frames.value = settings.radar_frames || 4;
    form.elements.alerts_url.value = settings.alerts_url || "";
    form.elements.radar_url.value = settings.radar_url || "";
  }
  const weather = app.weather || { state: "disabled", message: "Weather awareness is off" };
  const state = $("#weather-state");
  const stateClass = weather.state === "current" ? "connected" : weather.state;
  state.className = `aprs-state ${stateClass}`;
  state.textContent = weather.state === "current" ? "Current" : label(weather.state);
  $("#weather-runtime").textContent = weather.updated_at
    ? `${weather.message} Last successful update ${formatDateTime(weather.updated_at)}.`
    : weather.message;
}

function renderNetworkSettings() {
  const form = $("#network-settings-form");
  if (!form) return;
  const configured = app.network.configured || { mode: "standalone", host_url: "", access_key: "", listen_port: 8787 };
  if (!app.settingsFormDirty.network) {
    form.elements.mode.value = configured.mode || "standalone";
    form.elements.listen_port.value = configured.listen_port || 8787;
    form.elements.host_url.value = configured.host_url || "";
    form.elements.access_key.value = configured.access_key || "";
    form.elements.client_access_key.value = configured.access_key || "";
  }
  updateNetworkModeControls();

  const state = $("#network-state");
  state.className = `aprs-state ${app.network.active_mode === "standalone" ? "disabled" : "connected"}`;
  state.textContent = label(app.network.active_mode || "standalone");
  const addresses = app.network.detected_host_urls || app.network.host_urls || [];
  $("#network-host-addresses").innerHTML = addresses.length
    ? `<strong>Detected client host addresses</strong>${addresses.map(address => `<div class="network-address-row"><code>${html(address)}</code><button class="text-button" type="button" data-copy-network-url="${attr(address)}">Copy</button></div>`).join("")}`
    : `<span>No usable LAN address is currently detected. Connect this computer to the LAN and refresh.</span>`;

  const clients = app.network.clients || [];
  const activeDetails = [
    `Active role: ${label(app.network.active_mode || "standalone")}`,
    `Port ${app.network.active_port || 8787}`
  ];
  if (app.network.active_host_url) activeDetails.push(app.network.active_host_url);
  const clientList = clients.length
    ? `<div class="network-client-list">${clients.map(client => `<span class="network-client">${html(client.name)} · ${html(client.address)} · ${html(relativeTime(client.last_seen_at))}</span>`).join("")}</div>`
    : "";
  const runtime = $("#network-runtime");
  runtime.className = `network-runtime${app.network.restart_required ? " restart-required" : ""}`;
  runtime.innerHTML = `<strong>${html(app.network.message || "Network status unavailable")}</strong><span>${html(activeDetails.join(" · "))}</span>${clientList}`;
  $("#network-quit-apply").hidden = !app.network.restart_required;
  renderMobileEnrollment();
  renderConnectionTester();
}

function renderMobileEnrollment() {
  const responders = [...(app.state.responders || [])].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  const responderSelect = $("#mobile-enrollment-responder");
  const selectedResponder = responderSelect.value;
  responderSelect.innerHTML = `<option value="">Choose a responder</option>${responders.map(item => `<option value="${attr(item.id)}">${html(displayResponder(item))}</option>`).join("")}`;
  if (responders.some(item => item.id === selectedResponder)) responderSelect.value = selectedResponder;
  const addresses = app.network.host_urls || [];
  const addressSelect = $("#mobile-enrollment-address");
  const selectedAddress = addressSelect.value;
  addressSelect.innerHTML = `<option value="">Choose a detected LAN address</option>${addresses.map(address => `<option value="${attr(address)}">${html(address)}</option>`).join("")}`;
  if (addresses.includes(selectedAddress)) addressSelect.value = selectedAddress;
  else if (addresses.length === 1) addressSelect.value = addresses[0];
  const names = new Map(responders.map(item => [item.id, displayResponder(item)]));
  $("#mobile-device-list").innerHTML = app.mobileDevices.length
    ? `<strong>Enrolled phones</strong>${app.mobileDevices.map(device => `<div class="mobile-device-row"><span><b>${html(device.name)}</b> · ${html(names.get(device.responder_id) || "Deleted responder")}<small>${device.last_seen_at ? `Last used ${html(relativeTime(device.last_seen_at))}` : `Created ${html(relativeTime(device.created_at))}`}</small></span><button class="text-button" type="button" data-revoke-mobile-device="${attr(device.id)}">Revoke</button></div>`).join("")}`
    : `<small>No phones are enrolled yet.</small>`;
}

async function loadMobileDevices() {
  if (app.network.active_mode !== "host") {
    app.mobileDevices = [];
    return;
  }
  try {
    const result = await api("/api/mobile/admin/devices");
    app.mobileDevices = result.devices || [];
    renderMobileEnrollment();
  } catch {
    app.mobileDevices = [];
  }
}

async function createMobileEnrollment() {
  const responderID = $("#mobile-enrollment-responder").value;
  const hostURL = $("#mobile-enrollment-address").value;
  if (!responderID || !hostURL) {
    toast("Choose a responder and address", "Both selections are required before creating a QR code.", "warning");
    return;
  }
  const button = $("#mobile-create-enrollment");
  button.disabled = true;
  try {
    const result = await api("/api/mobile/admin/enrollment", { method: "POST", body: { responder_id: responderID, host_url: hostURL } });
    const output = $("#mobile-enrollment-result");
    output.hidden = false;
    output.innerHTML = `<img src="${attr(result.qr_data_uri)}" alt="QR code for one-time phone enrollment"><strong>Scan with the responder’s phone camera</strong><span>Expires ${html(formatDateTime(result.expires_at))}</span><button class="text-button" type="button" data-copy-enrollment-url>Copy setup link</button>`;
    $("[data-copy-enrollment-url]", output).addEventListener("click", () => copyText(result.url, "Enrollment link copied"));
    toast("QR enrollment ready", "It expires in 10 minutes and works once.");
  } catch (error) {
    toast("Could not create enrollment", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function revokeMobileDevice(id) {
  if (!confirm("Revoke this phone? It will immediately lose mobile access.")) return;
  try {
    await api(`/api/mobile/admin/devices/${encodeURIComponent(id)}`, { method: "DELETE" });
    await loadMobileDevices();
    toast("Phone access revoked", "The device credential can no longer connect.");
  } catch (error) {
    toast("Could not revoke phone", error.message, "error");
  }
}

function renderConnectionTester() {
  const list = $("#connection-check-list");
  const summary = $("#connection-summary");
  if (!list || !summary) return;
  const diagnostics = app.connections || { overall: "waiting", items: [] };
  const items = diagnostics.items || [];
  const connected = items.filter(item => item.state === "connected").length;
  const disconnected = items.filter(item => item.state === "disconnected").length;
  const stale = items.filter(item => item.state === "stale").length;
  const disabled = items.filter(item => item.state === "disabled").length;
  const checked = diagnostics.checked_at ? `Last checked ${relativeTime(diagnostics.checked_at)}` : "Not checked yet";
  summary.textContent = `${checked} · ${connected} connected · ${disconnected} disconnected · ${stale} delayed · ${disabled} disabled`;
  list.innerHTML = items.length ? items.map(item => {
    const target = connectionSettingsTarget(item.id);
    const tag = target ? "button" : "div";
    const action = target ? ` type="button" data-connection-settings="${attr(target)}" title="Open ${attr(item.label)} settings"` : "";
    return `
    <${tag} class="connection-check ${attr(item.state)}${target ? " actionable" : ""}"${action}>
      <span class="connection-check-dot" aria-hidden="true"></span>
      <div><strong>${html(item.label)} · ${html(label(item.state))}</strong><small>${html(item.detail || "No details reported")}</small></div>
    </${tag}>`;
  }).join("") : `<div class="empty-inline">Waiting for connection results…</div>`;
}

function connectionSettingsTarget(id) {
  if (id === "lan-sharing" || id === "lan-host" || id === "lan-clients" || id.startsWith("lan-client-")) return "network-settings-form";
  if (id === "aprs-is" || id === "aprs-local") return "aprs-settings-form";
  if (id === "nws" || id === "nws-radar") return "weather-settings-form";
  if (id === "water") return "water-settings-form";
  if (["storm-reports", "infrastructure", "amateur-repeaters", "gmrs-repeaters", "meshcore", "aredn", "sensors"].includes(id) || id.startsWith("aredn-")) return "integration-settings-form";
  return "";
}

function focusConnectionSettings(target) {
  const panel = document.getElementById(target);
  if (!panel) return;
  panel.scrollIntoView({ behavior: "smooth", block: "start" });
  panel.classList.remove("connection-target");
  void panel.offsetWidth;
  panel.classList.add("connection-target");
  setTimeout(() => panel.classList.remove("connection-target"), 1800);
}

function updateNetworkModeControls() {
  const form = $("#network-settings-form");
  const mode = form.elements.mode.value || "standalone";
  $$("[data-network-host]", form).forEach(item => { item.hidden = mode !== "host"; });
  $$("[data-network-client]", form).forEach(item => { item.hidden = mode !== "client"; });
}

function networkSettingsPayload(regenerateKey = false) {
  const form = $("#network-settings-form");
  const mode = form.elements.mode.value || "standalone";
  return {
    mode,
    host_url: form.elements.host_url.value,
    access_key: mode === "client" ? form.elements.client_access_key.value : form.elements.access_key.value,
    listen_port: Number(form.elements.listen_port.value || 8787),
    regenerate_key: regenerateKey
  };
}

async function saveNetworkSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  setFormBusy(form, true);
  try {
    app.network = await api("/api/network/settings", { method: "PUT", body: networkSettingsPayload() });
    app.settingsFormDirty.network = false;
    form.elements.access_key.value = app.network.configured?.access_key || "";
    form.elements.client_access_key.value = app.network.configured?.access_key || "";
    renderNetworkSettings();
    toast(
      "Network settings saved",
      app.network.restart_required ? "Restart Tickets Local to apply the role or listening-port change." : "Saved and applied immediately."
    );
  } catch (error) {
    toast("Network settings were not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function testNetworkConnection() {
  const button = $("#network-test-connection");
  const runtime = $("#network-runtime");
  button.disabled = true;
  runtime.className = "network-runtime";
  runtime.innerHTML = "<strong>Testing the host connection…</strong>";
  try {
    const result = await api("/api/network/test", { method: "POST", body: networkSettingsPayload() });
    app.network = await api("/api/network/settings", { method: "PUT", body: networkSettingsPayload() });
    app.settingsFormDirty.network = false;
    renderNetworkSettings();
    runtime.innerHTML = `<strong>${html(result.message)} and settings saved</strong><span>${result.host_version ? `Host version ${html(result.host_version)}` : ""}</span>`;
    toast("Host connection saved", app.network.restart_required ? "Connection verified. Restart once to change this computer to Client mode." : "Connection verified and applied immediately.");
  } catch (error) {
    runtime.className = "network-runtime restart-required";
    runtime.innerHTML = `<strong>Host connection failed</strong><span>${html(error.message)}</span>`;
    toast("Host connection failed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function refreshNetworkAddresses() {
  const button = $("#network-refresh-addresses");
  button.disabled = true;
  try {
    app.network = await api(`/api/network/status?refresh=${Date.now()}`);
    renderNetworkSettings();
    const count = (app.network.detected_host_urls || []).length;
    toast("Host addresses refreshed", count ? `${count} usable LAN address${count === 1 ? "" : "es"} detected.` : "No usable LAN address is currently detected.", count ? "" : "warning");
  } catch (error) {
    toast("Could not refresh host addresses", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function copyNetworkAccessKey() {
  const input = $("#network-settings-form").elements.access_key;
  if (!input.value) {
    toast("No access key yet", "Save Host mode to generate an access key.", "warning");
    return;
  }
  await copyText(input.value, "", input);
  toast("LAN access key copied", "Paste it into each Client computer’s network settings.");
}

async function copyText(value, message, fallbackInput = null) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    if (fallbackInput) {
      fallbackInput.select();
      document.execCommand("copy");
    } else {
      const temporary = document.createElement("textarea");
      temporary.value = value;
      temporary.style.position = "fixed";
      temporary.style.opacity = "0";
      document.body.appendChild(temporary);
      temporary.select();
      document.execCommand("copy");
      temporary.remove();
    }
  }
  if (message) toast(message, value);
}

async function regenerateNetworkAccessKey() {
  if (!confirm("Generate a new LAN access key? Every Client computer will need the new key after the Host is restarted.")) return;
  const button = $("#network-regenerate-key");
  button.disabled = true;
  try {
    app.network = await api("/api/network/settings", { method: "PUT", body: networkSettingsPayload(true) });
    app.settingsFormDirty.network = false;
    $("#network-settings-form").elements.access_key.value = app.network.configured?.access_key || "";
    renderNetworkSettings();
    toast("New LAN access key generated", app.network.restart_required ? "Copy it to the Client computers, then restart the Host." : "The Host is accepting the new key now; update each Client computer.");
  } catch (error) {
    toast("Access key was not changed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

function renderAPRSStatus() {
  const status = app.state.aprs_status || { state: "disabled", message: "APRS tracking is off" };
  const local = status.local || { state: "disabled", message: "Local RF reception is off" };
  const settings = app.state.settings.aprs || {};
  const overall = aprsOverallStatus(settings, status, local);
  const state = $("#aprs-state");
  state.className = `aprs-state ${overall.state}`;
  state.textContent = overall.state === "connected" ? "Live" : label(overall.state);
  const internetState = $("#aprs-internet-state");
  internetState.className = `aprs-state ${status.state}`;
  internetState.textContent = status.state === "connected" ? "Live" : label(status.state);
  const localState = $("#aprs-local-state");
  localState.className = `aprs-state ${local.state}`;
  localState.textContent = local.state === "connected" ? "Live" : label(local.state);
  const details = [];
  if (status.login_verified) details.push(`Verified as ${app.state.settings.aprs.login_callsign}`);
  if (status.server) details.push(status.server);
  if (status.filter) details.push(status.filter);
  const internetCounts = `${status.packets_received || 0} packets · ${status.position_packets_decoded || 0} positions · ${status.positions_updated || 0} responder updates`;
  const localCounts = `${local.packets_received || 0} packets · ${local.position_packets_decoded || 0} positions · ${local.positions_displayed || 0} map positions · ${local.positions_updated || 0} responder updates`;
  const decoderDetails = [];
  if (local.decoder === "bundled") {
    decoderDetails.push(local.decoder_available ? "Decoder installed" : "Decoder unavailable");
    if (local.decoder_architecture) decoderDetails.push(local.decoder_architecture === "arm64" ? "Apple silicon" : local.decoder_architecture);
    if (local.audio_level !== undefined && local.audio_level !== null) decoderDetails.push(`Audio level ${local.audio_level}`);
  }
  const decoderActivity = Array.isArray(local.recent_messages) && local.recent_messages.length
    ? `<details class="direwolf-activity"><summary>Recent decoder activity (${local.recent_messages.length})</summary><pre>${html(local.recent_messages.join("\n"))}</pre></details>`
    : "";
  const area = app.state.settings.aprs?.area_enabled
    ? ` · ${status.area_stations || 0} nearby stations · ${status.area_positions_received || 0} nearby positions`
    : "";
  $("#aprs-runtime").innerHTML = `
    <div class="aprs-runtime-source"><span class="status-dot state-${attr(status.state)}"></span><strong>Internet · ${html(status.message)}</strong></div>
    <small>${html(details.join(" · ") || "Internet feed not selected or not configured")}</small>
    <small>${html(internetCounts)}${html(area)}${status.last_packet_at ? ` · Last ${html(relativeTime(status.last_packet_at))}` : ""}</small>
    <div class="aprs-runtime-source"><span class="status-dot state-${attr(local.state)}"></span><strong>Local RF · ${html(local.message)}</strong></div>
    <small>${html(local.decoder === "kiss_tcp" ? local.kiss_address || "KISS TCP" : ["Built-in Dire Wolf", ...decoderDetails].join(" · "))}${local.igate_enabled ? " · RF-to-IS iGate enabled" : ""}</small>
    <small>${html(localCounts)}${local.last_packet_at ? ` · Last ${html(relativeTime(local.last_packet_at))}` : ""}</small>
    ${decoderActivity}`;
}

function populateAPRSAudioDeviceSelectors(selectedInput = "", selectedOutput = "") {
  const form = $("#aprs-settings-form");
  const input = form.elements.local_audio_device;
  const output = form.elements.local_audio_output_device;
  const inputs = app.aprsAudioDevices.filter(device => device.max_input_channels > 0);
  const outputs = app.aprsAudioDevices.filter(device => device.max_output_channels > 0);
  const options = (items, selected, prompt, defaultField) => {
    const values = new Set(items.map(item => item.name));
    const retained = selected && !values.has(selected)
      ? `<option value="${attr(selected)}">${html(selected)} (saved)</option>`
      : "";
    return `<option value="">${html(prompt)}</option>${retained}${items.map(item => {
      const marker = item[defaultField] ? " — system default" : "";
      return `<option value="${attr(item.name)}">${html(item.name)}${marker}</option>`;
    }).join("")}`;
  };
  input.innerHTML = options(inputs, selectedInput, "Select radio input…", "default_input");
  output.innerHTML = options(outputs, selectedOutput, "Select audio output…", "default_output");
  input.value = selectedInput;
  output.value = selectedOutput;
}

async function discoverAPRSAudioDevices() {
  const button = $("#aprs-detect-audio");
  const results = $("#aprs-audio-device-results");
  const form = $("#aprs-settings-form");
  const priorInput = form.elements.local_audio_device.value;
  const priorOutput = form.elements.local_audio_output_device.value;
  button.disabled = true;
  results.className = "audio-device-results";
  results.textContent = "Detecting audio devices…";
  try {
    const discovery = await api("/api/aprs/local/devices");
    app.aprsAudioDevices = discovery.devices || [];
    const defaultInput = app.aprsAudioDevices.find(device => device.default_input && device.max_input_channels > 0)?.name || "";
    const defaultOutput = app.aprsAudioDevices.find(device => device.default_output && device.max_output_channels > 0)?.name || "";
    const input = priorInput || defaultInput || app.aprsAudioDevices.find(device => device.max_input_channels > 0)?.name || "";
    const output = priorOutput || defaultOutput || app.aprsAudioDevices.find(device => device.max_output_channels > 0)?.name || "";
    populateAPRSAudioDeviceSelectors(input, output);
    results.className = "audio-device-results success";
    results.textContent = `${discovery.message}. ${discovery.decoder_version || "Built-in Dire Wolf"} · ${discovery.decoder_architecture || "current Mac"}. Select the radio interface, then save and apply.`;
    toast("Audio devices detected", `${app.aprsAudioDevices.length} devices found.`);
  } catch (error) {
    results.className = "audio-device-results error";
    results.textContent = error.message;
    toast("Audio-device detection failed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

function aprsOverallStatus(settings, internet, local) {
  if (!settings.enabled) return { state: "disabled", message: "APRS tracking is off" };
  const mode = settings.mode || "internet";
  if (mode === "internet") return { state: internet.state, message: `Internet only · ${internet.message || "status unavailable"}` };
  if (mode === "local") return { state: local.state, message: `Local RF only · ${local.message || "status unavailable"}` };
  if (internet.state === "connected" && local.state === "connected") {
    return { state: "connected", message: "Hybrid feed live · Internet and local RF connected" };
  }
  if (internet.state === "connected" || local.state === "connected") {
    return { state: "partial", message: `Hybrid feed partially live · Internet: ${label(internet.state)} · Local RF: ${label(local.state)}` };
  }
  return { state: internet.state === "authentication" || local.state === "configuration" ? "configuration" : "reconnecting", message: `Hybrid feed waiting · Internet: ${label(internet.state)} · Local RF: ${label(local.state)}` };
}

function updateAPRSModeControls() {
  const form = $("#aprs-settings-form");
  const mode = form.elements.mode.value || "internet";
  const decoder = form.elements.local_decoder.value || "bundled";
  const needsInternet = mode === "internet" || mode === "hybrid" || form.elements.local_igate_enabled.checked;
  $$("[data-aprs-internet]", form).forEach(item => { item.hidden = !needsInternet; });
  $$("[data-aprs-local]", form).forEach(item => { item.hidden = mode === "internet"; });
  $$("[data-local-kiss-address]", form).forEach(item => { item.hidden = decoder !== "kiss_tcp"; });
  $$("[data-local-audio-device]", form).forEach(item => { item.hidden = decoder !== "bundled"; });
  $$("[data-local-igate]", form).forEach(item => { item.hidden = decoder !== "bundled"; });
}

function incidentRow(incident) {
  const assigned = incident.assignments.map(item => responderByID(item.responder_id)).filter(Boolean);
  const stale = isIncidentStale(incident);
  const next = nextIncidentStatus(incident.status);
  return `<tr class="clickable${stale ? " incident-needs-update" : ""}" data-incident-id="${attr(incident.id)}">
    <td><div class="incident-title severity-${attr(incident.severity)}"><span class="severity-bar"></span><div><strong>${html(incident.title)}</strong><small>#${incident.number} · ${html(incident.type)}</small>${stale ? `<span class="stale-update-badge">No update ${html(relativeTime(incident.updated_at))}</span>` : ""}</div></div></td>
    <td>${html(locationText(incident))}</td>
    <td><div class="incident-status-stack"><span class="badge status-${attr(incident.status)}">${html(label(incident.status))}</span>${next ? `<button type="button" class="incident-advance-button" data-incident-advance="${attr(next)}" data-incident-id="${attr(incident.id)}">Next: ${html(label(next))}</button>` : ""}</div></td>
    <td><div class="unit-chips">${assigned.length ? assigned.map(item => `<span class="unit-chip">${html(displayResponder(item))}</span>`).join("") : `<span>—</span>`}</div></td>
    <td>${html(elapsed(incident.created_at))}</td>
  </tr>`;
}

function fullIncidentRow(incident) {
  const assigned = incident.assignments.map(item => responderByID(item.responder_id)).filter(Boolean);
  const stale = isIncidentStale(incident);
  const next = nextIncidentStatus(incident.status);
  return `<tr class="clickable${stale ? " incident-needs-update" : ""}" data-incident-id="${attr(incident.id)}">
    <td><strong>#${incident.number}</strong></td>
    <td><span class="priority-label severity-${attr(incident.severity)}">${html(incident.severity)}</span></td>
    <td><div class="incident-title"><div><strong>${html(incident.title)}</strong><small>${html(incident.type)}</small>${stale ? `<span class="stale-update-badge">No update ${html(relativeTime(incident.updated_at))}</span>` : ""}</div></div></td>
    <td>${html(locationText(incident))}</td>
    <td><div class="incident-status-stack"><span class="badge status-${attr(incident.status)}">${html(label(incident.status))}</span>${next ? `<button type="button" class="incident-advance-button" data-incident-advance="${attr(next)}" data-incident-id="${attr(incident.id)}">Next: ${html(label(next))}</button>` : ""}</div></td>
    <td><div class="unit-chips">${assigned.map(item => `<span class="unit-chip">${html(displayResponder(item))}</span>`).join("") || "—"}</div></td>
    <td title="${attr(formatDateTime(incident.created_at))}">${html(relativeTime(incident.created_at))}</td>
    <td><button class="row-action" type="button" aria-label="Edit incident">›</button></td>
  </tr>`;
}

function responderRow(responder) {
  const aprs = responderAPRSLabel(responder);
  return `<div class="responder-row" data-responder-id="${attr(responder.id)}">
    <div class="unit-avatar">${html(initials(displayResponder(responder)))}</div>
    <div><strong>${html(displayResponder(responder))}</strong><small>${html(aprs || (responder.name && responder.callsign ? responder.name : responder.type || "Responder"))}</small></div>
    <span class="status-indicator ${attr(responder.status)}" title="${attr(label(responder.status))}"></span>
  </div>`;
}

function responderCard(responder) {
  const activeAssignment = app.state.incidents.find(incident => activeStatuses.has(incident.status) && incident.assignments.some(item => item.responder_id === responder.id));
  const aprs = responderAPRSLabel(responder);
  return `<article class="responder-card ${attr(responder.status)}" data-responder-id="${attr(responder.id)}">
    <div class="responder-card-header">
      <div class="unit-avatar">${html(initials(displayResponder(responder)))}</div>
      <div><h3>${html(displayResponder(responder))}</h3><p>${html(responder.name && responder.callsign ? responder.name : responder.type || "Responder")}</p></div>
      <span class="badge status-${attr(responder.status)}">${html(label(responder.status))}</span>
    </div>
    <div class="capability-list">${responder.capabilities.length ? responder.capabilities.map(item => `<span class="capability">${html(item)}</span>`).join("") : `<span class="capability">No capabilities listed</span>`}${responder.aprs_enabled ? `<span class="capability aprs-chip">APRS</span>` : ""}</div>
    <div class="card-footer"><span>${activeAssignment ? `Incident #${activeAssignment.number}` : "No active assignment"}</span><span>${html(aprs || `Updated ${relativeTime(responder.updated_at)}`)}</span></div>
  </article>`;
}

function facilityCard(facility) {
  const percent = facility.capacity ? Math.min(100, Math.round((facility.occupied / facility.capacity) * 100)) : 0;
  const remaining = Math.max(0, facility.capacity - facility.occupied);
  return `<article class="facility-card ${attr(facility.status)}" data-facility-id="${attr(facility.id)}">
    <div class="facility-card-header"><div><h3>${html(facility.name)}</h3><p>${html(facility.type || "Facility")}</p></div><span class="badge status-${attr(facility.status)}">${html(label(facility.status))}</span></div>
    <p>${html(facility.address || "No address recorded")}</p>
    <div class="capacity-row">
      <div class="capacity-copy"><span>${facility.capacity ? `${remaining} available` : "Capacity not reported"}</span><span>${facility.capacity ? `${facility.occupied} / ${facility.capacity}` : ""}</span></div>
      <div class="capacity-track"><span style="width:${percent}%"></span></div>
    </div>
    <div class="card-footer"><span>${html(facility.phone || "No phone")}</span><span>Updated ${html(relativeTime(facility.updated_at))}</span></div>
  </article>`;
}

function locationCard(location) {
  const address = [location.address, location.city, location.region, location.postal_code].filter(Boolean).join(", ");
  const coordinates = location.latitude != null && location.longitude != null
    ? `${Number(location.latitude).toFixed(5)}, ${Number(location.longitude).toFixed(5)}`
    : "Not positioned on map";
  return `<article class="location-card" data-location-id="${attr(location.id)}">
    <div class="location-card-header"><div><h3>${html(location.name)}</h3><p>${html(location.type || "Saved location")}</p></div><svg><use href="#i-map-pin"/></svg></div>
    <p>${html(address)}</p>
    <span class="location-coordinates">${html(coordinates)}</span>
    <div class="card-footer"><span>${html(location.notes || "No notes")}</span><span>Updated ${html(relativeTime(location.updated_at))}</span></div>
  </article>`;
}

function activityItem(activity) {
  const marker = activity.entity_type === "incident" ? "I"
    : activity.entity_type === "responder" ? "R"
      : activity.entity_type === "facility" ? "F"
        : activity.entity_type === "location" ? "L"
          : activity.entity_type === "overlay" ? "K"
            : activity.entity_type === "asset" ? "A"
              : activity.entity_type === "schedule" ? "C"
                : activity.entity_type === "qualification" ? "Q"
                  : activity.entity_type === "message" ? "M" : "S";
  return `<div class="activity-item">
    <div class="activity-symbol">${marker}</div>
    <div><p>${html(activity.summary)}</p><small>${html(label(activity.kind.replace(".", " · ")))}</small></div>
    <time title="${attr(formatDateTime(activity.created_at))}">${html(relativeTime(activity.created_at))}</time>
  </div>`;
}

function openIncidentFromEvent(event) {
  const advance = event.target.closest("[data-incident-advance]");
  if (advance) {
    event.preventDefault();
    event.stopPropagation();
    advanceIncident(advance.dataset.incidentId, advance.dataset.incidentAdvance, advance);
    return;
  }
  const row = event.target.closest("[data-incident-id]");
  if (!row) return;
  openIncident(app.state.incidents.find(item => item.id === row.dataset.incidentId));
}

async function advanceIncident(id, status, button) {
  const incident = app.state.incidents.find(item => item.id === id);
  if (!incident) return;
  if (status === "closed" && !confirm(`Close incident #${incident.number}? Active assignments will be cleared and responders released.`)) return;
  button.disabled = true;
  try {
    const payload = {
      title: incident.title,
      type: incident.type,
      severity: incident.severity,
      status,
      address: incident.address,
      city: incident.city,
      region: incident.region,
      postal_code: incident.postal_code,
      latitude: incident.latitude,
      longitude: incident.longitude,
      contact_name: incident.contact_name,
      contact_phone: incident.contact_phone,
      description: incident.description
    };
    await api(`/api/incidents/${encodeURIComponent(id)}`, { method: "PUT", body: payload });
    toast(`Incident #${incident.number} advanced`, label(status));
    await loadState();
  } catch (error) {
    toast("Incident stage was not changed", error.message, "error");
    button.disabled = false;
  }
}

function openResponderFromEvent(event) {
  const row = event.target.closest("[data-responder-id]");
  if (!row) return;
  openResponder(app.state.responders.find(item => item.id === row.dataset.responderId));
}

function openFacilityFromEvent(event) {
  const row = event.target.closest("[data-facility-id]");
  if (!row) return;
  openFacility(app.state.facilities.find(item => item.id === row.dataset.facilityId));
}

function openLocationFromEvent(event) {
  const row = event.target.closest("[data-location-id]");
  if (!row) return;
  openLocation((app.state.locations || []).find(item => item.id === row.dataset.locationId));
}

function openAssetFromEvent(event) {
  const row = event.target.closest("[data-asset-id]");
  if (!row) return;
  openAsset((app.state.assets || []).find(item => item.id === row.dataset.assetId));
}

function openScheduleItemFromEvent(event) {
  const row = event.target.closest("[data-schedule-id]");
  if (!row) return;
  openScheduleItem((app.state.schedule || []).find(item => item.id === row.dataset.scheduleId));
}

function openQualificationFromEvent(event) {
  const row = event.target.closest("[data-qualification-id]");
  if (!row) return;
  openQualification((app.state.qualifications || []).find(item => item.id === row.dataset.qualificationId));
}

function openIncident(incident = null) {
  const dialog = $("#incident-dialog");
  const form = $("#incident-form");
  form.reset();
  form.dataset.editingId = incident?.id || "";
  form.dataset.updatedAt = incident?.updated_at || "";
  form.elements.id.value = incident?.id || "";
  dialog.classList.toggle("editing", Boolean(incident));
  $("#incident-dialog-title").textContent = incident ? `Incident #${incident.number}` : "Create incident";
  $("#incident-dialog-eyebrow").textContent = incident ? "DISPATCH RECORD" : "NEW DISPATCH RECORD";
  $("#geocode-results").classList.remove("visible");
  $("#geocode-results").innerHTML = "";
  populateSavedLocationSelect($("#incident-saved-location"), incident);

  if (incident) {
    setForm(form, incident);
    $("#incident-dialog-meta").textContent = `Created ${formatDateTime(incident.created_at)} · Updated ${relativeTime(incident.updated_at)}`;
  } else {
    form.elements.status.value = "new";
    form.elements.severity.value = "medium";
    $("#incident-dialog-meta").textContent = "Incident number assigned automatically";
  }
  renderIncidentStageButtons(form.elements.status.value);
  renderAssignmentPicker(incident);
  renderCommandRoles(incident);
  renderIncidentActions(incident);
  renderIncidentAttachments(incident);
  dialog.showModal();
  requestAnimationFrame(() => form.elements.title.focus());
}

function renderIncidentStageButtons(status) {
  const progression = ["new", "assigned", "enroute", "onscene", "closed"];
  const currentIndex = progression.indexOf(status);
  $$("[data-incident-stage]", $("#incident-stage-selector")).forEach(button => {
    const index = progression.indexOf(button.dataset.incidentStage);
    button.classList.toggle("active", button.dataset.incidentStage === status);
    button.classList.toggle("complete", currentIndex >= 0 && index < currentIndex);
  });
}

function renderAssignmentPicker(incident) {
  const selected = new Set((incident?.assignments || []).map(item => item.responder_id));
  const responders = [...app.state.responders].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  $("#assignment-picker").innerHTML = responders.length
    ? responders.map(responder => {
      const unavailable = responder.status !== "available" && !selected.has(responder.id);
      return `<label class="assignment-option" ${unavailable ? `title="${attr(label(responder.status))}"` : ""}>
        <input type="checkbox" data-assignment-responder="${attr(responder.id)}" ${selected.has(responder.id) ? "checked" : ""} ${unavailable ? "disabled" : ""}>
        <span><strong>${html(displayResponder(responder))}</strong><small>${html(responder.name || responder.type || "Responder")}</small></span>
        <span class="status-indicator ${attr(responder.status)}"></span>
      </label>`;
    }).join("")
    : `<div class="no-options">No responders configured. Save the incident, then add responders from the resource board.</div>`;
}

const commandRoleOptions = [
  ["incident_commander", "Incident Commander"], ["public_information", "Public Information Officer"],
  ["safety", "Safety Officer"], ["liaison", "Liaison Officer"], ["operations", "Operations Section Chief"],
  ["planning", "Planning Section Chief"], ["logistics", "Logistics Section Chief"], ["finance", "Finance/Admin Section Chief"]
];

function renderCommandRoles(incident) {
  const list = $("#command-role-list");
  list.innerHTML = "";
  (incident?.command || []).forEach(item => addCommandRoleRow(item));
  if (!list.children.length) list.innerHTML = '<div class="no-options command-empty">No command roles assigned.</div>';
}

function addCommandRoleRow(item = null) {
  const list = $("#command-role-list");
  $(".command-empty", list)?.remove();
  const responders = [...(app.state.responders || [])].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  const row = document.createElement("div");
  row.className = "command-role-row";
  row.innerHTML = '<label>Role<select data-command-role required>' + commandRoleOptions.map(option => '<option value="' + option[0] + '">' + option[1] + '</option>').join("") + '</select></label>' +
    '<label>Responder<select data-command-responder required><option value="">Select responder</option>' + responders.map(responder => '<option value="' + attr(responder.id) + '">' + html(displayResponder(responder)) + '</option>').join("") + '</select></label>' +
    '<button class="row-action" type="button" data-remove-command-role>Remove</button>';
  list.appendChild(row);
  if (item) {
    $("[data-command-role]", row).value = item.role;
    $("[data-command-responder]", row).value = item.responder_id;
  }
}

function renderIncidentActions(incident) {
  const actions = [...(incident?.actions || [])].reverse();
  $("#new-action-text").value = "";
  $("#incident-action-list").innerHTML = actions.length
    ? actions.map(action => `<div class="incident-action"><p>${html(action.description)}</p><time>${html(formatDateTime(action.created_at))}</time></div>`).join("")
    : `<div class="no-options">No action notes have been recorded.</div>`;
}

function renderIncidentAttachments(incident) {
  const items=incident?.attachments||[];$("#incident-attachment-file").value="";
  $("#incident-attachment-list").innerHTML=items.length?items.map(item=>`<div class="incident-attachment"><div><strong>${html(item.name)}</strong><small>${html(item.media_type)} · ${(item.size/1024).toFixed(1)} KB · ${html(relativeTime(item.created_at))}</small></div><a class="text-button" href="/api/incidents/${encodeURIComponent(incident.id)}/attachments/${encodeURIComponent(item.id)}">Download</a></div>`).join(""):`<div class="weather-clear">No attachments.</div>`;
}

async function uploadIncidentAttachment() {
  const form=$("#incident-form"),incidentID=form.dataset.editingId,file=$("#incident-attachment-file").files[0];
  if(!incidentID||!file){toast("Choose a file","Save the incident first, then select a JPEG, PNG, or PDF.","error");return}
  if(file.size>10*1024*1024){toast("File is too large","Attachments must be 10 MB or smaller.","error");return}
  const data=new FormData();data.append("file",file,file.name);const button=$("#upload-attachment-button");button.disabled=true;
  try{const response=await fetch(`/api/incidents/${encodeURIComponent(incidentID)}/attachments`,{method:"POST",body:data,headers:{Accept:"application/json"}});const payload=await response.json();if(!response.ok)throw new Error(payload.error||`Upload failed (${response.status})`);await loadState();const saved=app.state.incidents.find(item=>item.id===incidentID);form.dataset.updatedAt=saved.updated_at;renderIncidentAttachments(saved);toast("Attachment added",file.name)}catch(error){toast("Attachment was not added",error.message,"error")}finally{button.disabled=false}
}

function openIncidentExport() {
  const incidentID = $("#incident-form").dataset.editingId;
  const incident = app.state.incidents.find(item => item.id === incidentID);
  if (!incident) {
    toast("Save the incident first", "Exports are available after the incident has been saved.", "error");
    return;
  }
  const dialog = $("#incident-export-dialog");
  dialog.dataset.incidentId = incidentID;
  $("#incident-export-title").textContent = `Export incident #${incident.number}`;
  $("#winlink-email-subject").value = `Incident #${incident.number} - ${incident.title}`.slice(0, 100);
  $("#incident-export-preview").value = "";
  $("#download-incident-export").disabled = true;
  dialog.showModal();
  loadIncidentExport();
}

async function loadIncidentExport() {
  const dialog = $("#incident-export-dialog");
  const incidentID = dialog.dataset.incidentId;
  if (!incidentID) return;
  const format = $("#incident-export-format").value;
  const isWinlink = format === "winlink";
  $("#export-winlink-fields").hidden = !isWinlink;
  const preview = $("#incident-export-preview");
  const download = $("#download-incident-export");
  preview.value = "Generating preview…";
  preview.disabled = true;
  download.disabled = true;
  try {
    const response = await fetch(`/api/incidents/${encodeURIComponent(incidentID)}/export?format=${encodeURIComponent(format)}`, { headers: { Accept: "application/json" } });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `Export failed (${response.status})`);
    preview.value = result.content;
    dialog.dataset.filename = result.filename;
    dialog.dataset.contentType = result.content_type;
    $("#incident-export-meta").textContent = `${result.filename} · Edit this preview if needed before downloading.`;
    download.textContent = isWinlink ? "Download email (.eml)" : "Download reviewed file";
    download.disabled = false;
  } catch (error) {
    preview.value = "";
    $("#incident-export-meta").textContent = error.message;
    toast("Export preview failed", error.message, "error");
  } finally {
    preview.disabled = false;
  }
}

async function downloadIncidentExport() {
  const dialog = $("#incident-export-dialog");
  let content = $("#incident-export-preview").value;
  if (!content || !dialog.dataset.filename) return;
  if ($("#incident-export-format").value === "winlink") {
    const recipient = $("#winlink-email-recipient").value.trim();
    if (!recipient) {
      toast("Recipient required", "Enter the operator-confirmed Winlink email address.", "error");
      $("#winlink-email-recipient").focus();
      return;
    }
    const button = $("#download-incident-export");
    button.disabled = true;
    try {
      const response = await fetch(`/api/incidents/${encodeURIComponent(dialog.dataset.incidentId)}/winlink-email`, {
        method: "POST",
        headers: { Accept: "application/json", "Content-Type": "application/json" },
        body: JSON.stringify({ recipient, precedence: $("#winlink-email-precedence").value, subject: $("#winlink-email-subject").value, body: content })
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || `Email preparation failed (${response.status})`);
      content = result.content;
      dialog.dataset.filename = result.filename;
      dialog.dataset.contentType = result.content_type;
    } catch (error) {
      toast("Winlink email was not prepared", error.message, "error");
      return;
    } finally {
      button.disabled = false;
    }
  }
  const blob = new Blob([content], { type: dialog.dataset.contentType || "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = dialog.dataset.filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  toast("Reviewed export downloaded", dialog.dataset.filename);
}

async function saveIncident(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const selected = new Set($$("[data-assignment-responder]", form).filter(item => item.checked).map(item => item.dataset.assignmentResponder));
  const original = app.state.incidents.find(item => item.id === id);
  setFormBusy(form, true);
  try {
    const mapResolution = await resolveCoordinatesForSave(
      form,
      [form.elements.address.value, form.elements.city.value, form.elements.region.value, form.elements.postal_code.value].filter(Boolean).join(", ")
    );
    const payload = {
      title: form.elements.title.value,
      type: form.elements.type.value,
      severity: form.elements.severity.value,
      status: form.elements.status.value,
      address: form.elements.address.value,
      city: form.elements.city.value,
      region: form.elements.region.value,
      postal_code: form.elements.postal_code.value,
      latitude: optionalNumber(form.elements.latitude.value),
      longitude: optionalNumber(form.elements.longitude.value),
      contact_name: form.elements.contact_name.value,
      contact_phone: form.elements.contact_phone.value,
      description: form.elements.description.value,
      command: $$(".command-role-row", form).map(row => ({
        role: $("[data-command-role]", row).value,
        responder_id: $("[data-command-responder]", row).value
      })),
      expected_updated_at: id ? form.dataset.updatedAt : undefined
    };
    if (payload.status === "closed" || payload.status === "cancelled") selected.clear();
    const saved = await api(id ? `/api/incidents/${encodeURIComponent(id)}` : "/api/incidents", {
      method: id ? "PUT" : "POST",
      body: payload
    });
    const existingAssignments = new Set((original?.assignments || []).map(item => item.responder_id));
    for (const responderID of selected) {
      if (!existingAssignments.has(responderID)) {
        await api(`/api/incidents/${encodeURIComponent(saved.id)}/assignments`, { method: "POST", body: { responder_id: responderID } });
      }
    }
    for (const responderID of existingAssignments) {
      if (!selected.has(responderID)) {
        await api(`/api/incidents/${encodeURIComponent(saved.id)}/assignments/${encodeURIComponent(responderID)}`, { method: "DELETE" });
      }
    }
    $("#incident-dialog").close();
    form.dataset.editingId = "";
    form.elements.id.value = "";
    toast(id ? "Incident updated" : `Incident #${saved.number} created`, saved.title);
    await loadState();
    if (saved.latitude != null && saved.longitude != null) {
      app.map.focusPoint(saved.latitude, saved.longitude);
    } else {
      toast(
        "Incident saved without a map marker",
        mapResolution.warning || "Use Look up address or enter coordinates to place it on the map.",
        "warning"
      );
    }
  } catch (error) {
    toast("Incident was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function addIncidentAction() {
  const id = $("#incident-form").dataset.editingId || "";
  const description = $("#new-action-text").value.trim();
  if (!id || !description) return;
  $("#add-action-button").disabled = true;
  try {
    const incident = await api(`/api/incidents/${encodeURIComponent(id)}/actions`, { method: "POST", body: { description } });
    const index = app.state.incidents.findIndex(item => item.id === incident.id);
    if (index >= 0) app.state.incidents[index] = incident;
    renderIncidentActions(incident);
    toast("Action note added", `Incident #${incident.number}`);
  } catch (error) {
    toast("Note was not added", error.message, "error");
  } finally {
    $("#add-action-button").disabled = false;
  }
}

function openResponder(responder = null) {
  const dialog = $("#responder-dialog");
  const form = $("#responder-form");
  form.reset();
  form.dataset.editingId = responder?.id || "";
  form.dataset.updatedAt = responder?.updated_at || "";
  form.elements.id.value = responder?.id || "";
  dialog.classList.toggle("editing", Boolean(responder));
  $("#device-location-status").textContent = "One-time capture only. Tickets Local does not track this browser in the background.";
  $("#responder-dialog-title").textContent = responder ? "Edit responder" : "Add responder";
  const ownTracksBase = app.network.active_mode === "host" && app.network.host_urls?.length ? app.network.host_urls[0] : location.origin;
  $("#owntracks-endpoint").textContent = responder ? ownTracksBase.replace(/\/$/, "") + "/api/tracking/owntracks/" + responder.id : "Save the responder first";
  $("#owntracks-help").textContent = app.network.active_mode === "host" ? "Use HTTP mode and Basic authentication: any username, LAN access key as password. Trusted private LAN only." : "Switch this computer to Host mode before configuring a phone on the LAN.";
  $("#opengts-endpoint").textContent = responder ? ownTracksBase.replace(/\/$/, "") + "/api/tracking/opengts/" + responder.id : "Save the responder first";
  $("#opengts-help").textContent = app.network.active_mode === "host" ? "Send gprmc-style lat, lon, date, time, speed, head, and alt parameters. Use any Basic Auth username and the LAN access key as password." : "Switch this computer to Host mode before configuring a GPS tracker on the LAN.";
  if (responder) {
    setForm(form, responder);
    form.elements.marker_color.value = responder.marker_color || "#3b82f6";
    form.elements.capabilities.value = responder.capabilities.join(", ");
    form.elements.aprs_enabled.checked = Boolean(responder.aprs_enabled);
  } else {
    form.elements.status.value = "available";
    form.elements.aprs_enabled.checked = false;
    form.elements.marker_color.value = "#3b82f6";
  }
  dialog.showModal();
  requestAnimationFrame(() => form.elements.name.focus());
}

async function captureDeviceLocation() {
  const form = $("#responder-form");
  const button = $("#use-device-location");
  const status = $("#device-location-status");
  if (!navigator.geolocation) { toast("Device location unavailable", "This browser does not provide location access.", "error"); return; }
  button.disabled = true;
  status.textContent = "Waiting for this device’s location…";
  navigator.geolocation.getCurrentPosition(async position => {
    const latitude = position.coords.latitude;
    const longitude = position.coords.longitude;
    form.elements.latitude.value = latitude.toFixed(6);
    form.elements.longitude.value = longitude.toFixed(6);
    const id = form.dataset.editingId || "";
    try {
      if (id) {
        const saved = await api("/api/responders/" + encodeURIComponent(id) + "/position", { method: "POST", body: { latitude, longitude, accuracy_meters: position.coords.accuracy, expected_updated_at: form.dataset.updatedAt } });
        form.dataset.updatedAt = saved.updated_at;
        status.textContent = "Saved from this device · accuracy about " + Math.round(position.coords.accuracy) + " m";
        await loadState();
      } else {
        status.textContent = "Captured · accuracy about " + Math.round(position.coords.accuracy) + " m. Save the responder to keep it.";
      }
      toast("Device location captured", latitude.toFixed(5) + ", " + longitude.toFixed(5));
    } catch (error) {
      status.textContent = "Location was captured but could not be saved.";
      toast("Device location was not saved", error.message, "error");
    } finally { button.disabled = false; }
  }, error => {
    status.textContent = error.code === 1 ? "Location permission was not granted." : "This device could not determine a location.";
    toast("Device location unavailable", status.textContent, "error");
    button.disabled = false;
  }, { enableHighAccuracy: true, timeout: 15000, maximumAge: 30000 });
}

async function saveResponder(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const payload = {
    name: form.elements.name.value,
    callsign: form.elements.callsign.value,
    type: form.elements.type.value,
    status: form.elements.status.value,
    phone: form.elements.phone.value,
    capabilities: form.elements.capabilities.value.split(",").map(item => item.trim()).filter(Boolean),
    latitude: optionalNumber(form.elements.latitude.value),
    longitude: optionalNumber(form.elements.longitude.value),
    aprs_enabled: form.elements.aprs_enabled.checked,
    map_label: form.elements.map_label.value,
    marker_color: form.elements.marker_color.value,
    notes: form.elements.notes.value,
    expected_updated_at: id ? form.dataset.updatedAt : undefined
  };
  setFormBusy(form, true);
  try {
    const saved = await api(id ? `/api/responders/${encodeURIComponent(id)}` : "/api/responders", { method: id ? "PUT" : "POST", body: payload });
    $("#responder-dialog").close();
    form.dataset.editingId = "";
    form.elements.id.value = "";
    if (saved.aprs_enabled) {
      try {
        await api("/api/aprs/reconnect", { method: "POST", body: {} });
      } catch {
        // The responder was saved successfully; the manager also receives a
        // server-side refresh notification from the responder endpoint.
      }
    }
    toast(
      id ? "Responder updated" : "Responder added",
      `${displayResponder(saved)}${saved.aprs_enabled ? " · APRS refresh requested" : ""}`
    );
    await loadState();
    if (saved.aprs_enabled) setTimeout(() => loadState(), 1500);
  } catch (error) {
    toast("Responder was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function deleteResponder() {
  const form = $("#responder-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  const responder = app.state.responders.find(item => item.id === id);
  if (!confirm(`Delete ${responder ? displayResponder(responder) : "this responder"}? Its saved APRS trail will also be removed.`)) return;
  setFormBusy(form, true);
  try {
    await api(`/api/responders/${encodeURIComponent(id)}`, { method: "DELETE" });
    $("#responder-dialog").close();
    toast("Responder deleted", responder ? displayResponder(responder) : "");
    await loadState();
  } catch (error) {
    toast("Responder was not deleted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function chooseResponderPositionOnMap() {
  const form = $("#responder-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  $("#responder-dialog").close();
  switchPage("dashboard");
  requestAnimationFrame(() => app.map.startResponderPosition(id));
}

function openFacility(facility = null) {
  const dialog = $("#facility-dialog");
  const form = $("#facility-form");
  form.reset();
  form.dataset.editingId = facility?.id || "";
  form.dataset.updatedAt = facility?.updated_at || "";
  form.elements.id.value = facility?.id || "";
  $("#facility-geocode-results").classList.remove("visible");
  $("#facility-geocode-results").innerHTML = "";
  populateSavedLocationSelect($("#facility-saved-location"), facility);
  $("#facility-dialog-title").textContent = facility ? "Edit facility" : "Add facility";
  if (facility) {
    setForm(form, facility);
    form.elements.marker_color.value = facility.marker_color || "#22c55e";
  } else {
    form.elements.status.value = "open";
    form.elements.capacity.value = 0;
    form.elements.occupied.value = 0;
    form.elements.marker_color.value = "#22c55e";
  }
  dialog.showModal();
  requestAnimationFrame(() => form.elements.name.focus());
}

async function saveFacility(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  setFormBusy(form, true);
  try {
    const mapResolution = await resolveCoordinatesForSave(form, form.elements.address.value);
    const payload = {
      name: form.elements.name.value,
      type: form.elements.type.value,
      status: form.elements.status.value,
      address: form.elements.address.value,
      phone: form.elements.phone.value,
      capacity: Number(form.elements.capacity.value || 0),
      occupied: Number(form.elements.occupied.value || 0),
      latitude: optionalNumber(form.elements.latitude.value),
      longitude: optionalNumber(form.elements.longitude.value),
      map_label: form.elements.map_label.value,
      marker_color: form.elements.marker_color.value,
      notes: form.elements.notes.value,
      expected_updated_at: id ? form.dataset.updatedAt : undefined
    };
    const saved = await api(id ? `/api/facilities/${encodeURIComponent(id)}` : "/api/facilities", { method: id ? "PUT" : "POST", body: payload });
    $("#facility-dialog").close();
    form.dataset.editingId = "";
    form.elements.id.value = "";
    toast(id ? "Facility updated" : "Facility added", saved.name);
    await loadState();
    if (saved.latitude != null && saved.longitude != null) {
      app.map.focusPoint(saved.latitude, saved.longitude);
    } else if (saved.address) {
      toast(
        "Facility saved without a map marker",
        mapResolution.warning || "Use Look up address or enter coordinates to place it on the map.",
        "warning"
      );
    }
  } catch (error) {
    toast("Facility was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function openLocation(location = null) {
  const dialog = $("#location-dialog");
  const form = $("#location-form");
  form.reset();
  form.dataset.editingId = location?.id || "";
  form.dataset.updatedAt = location?.updated_at || "";
  form.elements.id.value = location?.id || "";
  dialog.classList.toggle("editing", Boolean(location));
  $("#location-dialog-title").textContent = location ? "Edit location" : "Add location";
  $("#location-geocode-results").classList.remove("visible");
  $("#location-geocode-results").innerHTML = "";
  if (location) setForm(form, location);
  dialog.showModal();
  requestAnimationFrame(() => form.elements.name.focus());
}

async function saveLocation(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const payload = {
    name: form.elements.name.value,
    type: form.elements.type.value,
    address: form.elements.address.value,
    city: form.elements.city.value,
    region: form.elements.region.value,
    postal_code: form.elements.postal_code.value,
    latitude: optionalNumber(form.elements.latitude.value),
    longitude: optionalNumber(form.elements.longitude.value),
    notes: form.elements.notes.value,
    expected_updated_at: id ? form.dataset.updatedAt : undefined
  };
  setFormBusy(form, true);
  try {
    const saved = await api(id ? `/api/locations/${encodeURIComponent(id)}` : "/api/locations", { method: id ? "PUT" : "POST", body: payload });
    $("#location-dialog").close();
    form.dataset.editingId = "";
    form.elements.id.value = "";
    toast(id ? "Location updated" : "Location added", saved.name);
    await loadState();
  } catch (error) {
    toast("Location was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function deleteLocation() {
  const form = $("#location-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  const location = (app.state.locations || []).find(item => item.id === id);
  if (!confirm(`Delete ${location?.name || "this saved location"}? This removes it from the map.`)) return;
  setFormBusy(form, true);
  try {
    await api(`/api/locations/${encodeURIComponent(id)}`, { method: "DELETE" });
    $("#location-dialog").close();
    form.dataset.editingId = "";
    form.elements.id.value = "";
    toast("Location deleted", location?.name || "");
    await loadState();
  } catch (error) {
    toast("Location was not deleted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function openAsset(asset = null) {
  const dialog = $("#asset-dialog");
  const form = $("#asset-form");
  form.reset();
  form.dataset.editingId = asset?.id || "";
  form.dataset.updatedAt = asset?.updated_at || "";
  form.elements.id.value = asset?.id || "";
  dialog.classList.toggle("editing", Boolean(asset));
  $("#asset-dialog-title").textContent = asset ? "Edit asset" : "Add asset";
  if (asset) {
    setForm(form, asset);
    form.elements.maintenance_due.value = asset.maintenance_due ? asset.maintenance_due.slice(0, 10) : "";
  } else {
    form.elements.category.value = "vehicle";
    form.elements.status.value = "ready";
    form.elements.quantity.value = 1;
  }
  dialog.showModal();
  requestAnimationFrame(() => form.elements.name.focus());
}

async function saveAsset(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const payload = {
    category: form.elements.category.value,
    name: form.elements.name.value,
    identifier: form.elements.identifier.value,
    status: form.elements.status.value,
    quantity: Number(form.elements.quantity.value),
    location: form.elements.location.value,
    custodian: form.elements.custodian.value,
    maintenance_due: form.elements.maintenance_due.value ? form.elements.maintenance_due.value + "T12:00:00Z" : null,
    notes: form.elements.notes.value,
    expected_updated_at: id ? form.dataset.updatedAt : undefined
  };
  setFormBusy(form, true);
  try {
    const saved = await api(id ? "/api/assets/" + encodeURIComponent(id) : "/api/assets", { method: id ? "PUT" : "POST", body: payload });
    $("#asset-dialog").close();
    toast(id ? "Asset updated" : "Asset added", saved.name);
    await loadState();
  } catch (error) {
    toast("Asset was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function deleteAsset() {
  const form = $("#asset-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  const asset = (app.state.assets || []).find(item => item.id === id);
  if (!confirm("Delete " + (asset?.name || "this asset") + "?")) return;
  setFormBusy(form, true);
  try {
    await api("/api/assets/" + encodeURIComponent(id), { method: "DELETE" });
    $("#asset-dialog").close();
    toast("Asset deleted", asset?.name || "");
    await loadState();
  } catch (error) {
    toast("Asset was not deleted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function openScheduleItem(item = null) {
  const dialog = $("#schedule-dialog");
  const form = $("#schedule-form");
  form.reset();
  form.dataset.editingId = item?.id || "";
  form.dataset.updatedAt = item?.updated_at || "";
  form.elements.id.value = item?.id || "";
  dialog.classList.toggle("editing", Boolean(item));
  $("#schedule-dialog-title").textContent = item ? "Edit scheduled item" : "Add scheduled item";
  if (item) {
    setForm(form, item);
    form.elements.start_at.value = toLocalDateTimeInput(item.start_at);
    form.elements.end_at.value = toLocalDateTimeInput(item.end_at);
  } else {
    const start = new Date();
    start.setMinutes(0, 0, 0);
    start.setHours(start.getHours() + 1);
    const end = new Date(start.getTime() + 8 * 60 * 60 * 1000);
    form.elements.type.value = "shift";
    form.elements.status.value = "planned";
    form.elements.start_at.value = toLocalDateTimeInput(start);
    form.elements.end_at.value = toLocalDateTimeInput(end);
  }
  renderScheduleAssignmentPicker(item);
  dialog.showModal();
  requestAnimationFrame(() => form.elements.title.focus());
}

function renderScheduleAssignmentPicker(item) {
  const selected = new Set(item?.assigned_responder_ids || []);
  const responders = [...(app.state.responders || [])].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  $("#schedule-assignment-picker").innerHTML = responders.length
    ? responders.map(responder => '<label class="assignment-option"><input type="checkbox" data-schedule-responder="' + attr(responder.id) + '" ' + (selected.has(responder.id) ? "checked" : "") + '><span><strong>' + html(displayResponder(responder)) + '</strong><small>' + html(responder.name || responder.type || "Responder") + '</small></span><span class="status-indicator ' + attr(responder.status) + '"></span></label>').join("")
    : '<div class="no-options">No responders configured.</div>';
}

async function saveScheduleItem(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const payload = {
    title: form.elements.title.value,
    type: form.elements.type.value,
    status: form.elements.status.value,
    start_at: new Date(form.elements.start_at.value).toISOString(),
    end_at: new Date(form.elements.end_at.value).toISOString(),
    location: form.elements.location.value,
    coordinator: form.elements.coordinator.value,
    assigned_responder_ids: $$('[data-schedule-responder]:checked', form).map(input => input.dataset.scheduleResponder),
    notes: form.elements.notes.value,
    expected_updated_at: id ? form.dataset.updatedAt : undefined
  };
  setFormBusy(form, true);
  try {
    const saved = await api(id ? "/api/schedule/" + encodeURIComponent(id) : "/api/schedule", { method: id ? "PUT" : "POST", body: payload });
    dialogClose("#schedule-dialog");
    toast(id ? "Schedule updated" : "Scheduled item added", saved.title);
    await loadState();
  } catch (error) {
    toast("Scheduled item was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function deleteScheduleItem() {
  const form = $("#schedule-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  const item = (app.state.schedule || []).find(candidate => candidate.id === id);
  if (!confirm("Delete " + (item?.title || "this scheduled item") + "?")) return;
  setFormBusy(form, true);
  try {
    await api("/api/schedule/" + encodeURIComponent(id), { method: "DELETE" });
    dialogClose("#schedule-dialog");
    toast("Scheduled item deleted", item?.title || "");
    await loadState();
  } catch (error) {
    toast("Scheduled item was not deleted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function dialogClose(selector) {
  $(selector).close();
}

function toLocalDateTimeInput(value) {
  const date = value instanceof Date ? value : new Date(value);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

function openQualification(item = null) {
  const dialog = $("#qualification-dialog");
  const form = $("#qualification-form");
  form.reset();
  form.dataset.editingId = item?.id || "";
  form.dataset.updatedAt = item?.updated_at || "";
  form.elements.id.value = item?.id || "";
  dialog.classList.toggle("editing", Boolean(item));
  $("#qualification-dialog-title").textContent = item ? "Edit qualification" : "Add qualification";
  const responders = [...(app.state.responders || [])].sort((a, b) => displayResponder(a).localeCompare(displayResponder(b)));
  form.elements.responder_id.innerHTML = '<option value="">Select responder</option>' + responders.map(responder => '<option value="' + attr(responder.id) + '">' + html(displayResponder(responder)) + (responder.name && responder.name !== displayResponder(responder) ? ' · ' + html(responder.name) : '') + '</option>').join("");
  if (item) {
    setForm(form, item);
    form.elements.completed_at.value = item.completed_at ? item.completed_at.slice(0, 10) : "";
    form.elements.expires_at.value = item.expires_at ? item.expires_at.slice(0, 10) : "";
  } else {
    form.elements.category.value = "certification";
    form.elements.status.value = "current";
  }
  dialog.showModal();
  requestAnimationFrame(() => (responders.length ? form.elements.responder_id : form.elements.name).focus());
}

async function saveQualification(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const id = form.dataset.editingId || "";
  const noonUTC = value => value ? value + "T12:00:00Z" : null;
  const payload = {
    responder_id: form.elements.responder_id.value,
    category: form.elements.category.value,
    name: form.elements.name.value,
    provider: form.elements.provider.value,
    credential_id: form.elements.credential_id.value,
    status: form.elements.status.value,
    completed_at: noonUTC(form.elements.completed_at.value),
    expires_at: noonUTC(form.elements.expires_at.value),
    notes: form.elements.notes.value,
    expected_updated_at: id ? form.dataset.updatedAt : undefined
  };
  setFormBusy(form, true);
  try {
    const saved = await api(id ? "/api/qualifications/" + encodeURIComponent(id) : "/api/qualifications", { method: id ? "PUT" : "POST", body: payload });
    dialogClose("#qualification-dialog");
    toast(id ? "Qualification updated" : "Qualification added", saved.name);
    await loadState();
  } catch (error) {
    toast("Qualification was not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function deleteQualification() {
  const form = $("#qualification-form");
  const id = form.dataset.editingId || "";
  if (!id) return;
  const item = (app.state.qualifications || []).find(candidate => candidate.id === id);
  if (!confirm("Delete " + (item?.name || "this qualification") + "?")) return;
  setFormBusy(form, true);
  try {
    await api("/api/qualifications/" + encodeURIComponent(id), { method: "DELETE" });
    dialogClose("#qualification-dialog");
    toast("Qualification deleted", item?.name || "");
    await loadState();
  } catch (error) {
    toast("Qualification was not deleted", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

function populateSavedLocationSelect(select, record) {
  const locations = [...(app.state.locations || [])].sort((a, b) => a.name.localeCompare(b.name));
  select.innerHTML = `<option value="">Enter a different location</option>${locations.map(location =>
    `<option value="${attr(location.id)}">${html(location.name)}${location.type ? ` · ${html(location.type)}` : ""}</option>`
  ).join("")}`;
  if (!record) return;
  const match = locations.find(location =>
    location.latitude != null && location.longitude != null &&
    Number(location.latitude) === Number(record.latitude) && Number(location.longitude) === Number(record.longitude)
  );
  select.value = match?.id || "";
}

function applySavedLocation(id, form, includeAddressParts) {
  if (!id) return;
  const location = (app.state.locations || []).find(item => item.id === id);
  if (!location) return;
  form.elements.address.value = includeAddressParts
    ? location.address
    : [location.address, location.city, location.region, location.postal_code].filter(Boolean).join(", ");
  if (includeAddressParts) {
    form.elements.city.value = location.city || "";
    form.elements.region.value = location.region || "";
    form.elements.postal_code.value = location.postal_code || "";
  }
  form.elements.latitude.value = location.latitude ?? "";
  form.elements.longitude.value = location.longitude ?? "";
}

async function saveSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const payload = {
    organization: form.elements.organization.value,
    center_address: form.elements.center_address.value,
    center_lat: Number(form.elements.center_lat.value),
    center_lon: Number(form.elements.center_lon.value),
    default_zoom: Number(form.elements.default_zoom.value),
    incident_stale_minutes: Number(form.elements.incident_stale_minutes.value),
    aprs: app.state.settings.aprs,
    weather: app.state.settings.weather,
    water: app.state.settings.water,
    integrations: app.state.settings.integrations
  };
  setFormBusy(form, true);
  try {
    await api("/api/settings", { method: "PUT", body: payload });
    app.settingsFormDirty.general = false;
    toast("Settings saved", payload.organization);
    await loadState();
  } catch (error) {
    toast("Settings were not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function saveWeatherSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const payload = {
    ...app.state.settings,
    weather: {
      enabled: form.elements.enabled.checked,
      refresh_minutes: Number(form.elements.refresh_minutes.value),
      radar_enabled: form.elements.radar_enabled.checked,
      radar_opacity: Number(form.elements.radar_opacity.value),
      radar_animation: form.elements.radar_animation.checked,
      radar_frames: Number(form.elements.radar_frames.value),
      alerts_url: form.elements.alerts_url.value.trim(),
      radar_url: form.elements.radar_url.value.trim()
    }
  };
  setFormBusy(form, true);
  try {
    await api("/api/settings", { method: "PUT", body: payload });
    app.settingsFormDirty.weather = false;
    await loadState();
    await loadWeather();
    toast("Weather settings saved", payload.weather.enabled ? "NWS alert monitoring is active." : "Weather awareness is off.");
  } catch (error) {
    toast("Weather settings were not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function saveWaterSettings(event) {
  event.preventDefault(); const form=event.currentTarget;
  const payload={...app.state.settings,water:{enabled:form.elements.enabled.checked,site_ids:form.elements.site_ids.value,refresh_minutes:Number(form.elements.refresh_minutes.value),source_url:form.elements.source_url.value.trim(),noaa_enabled:form.elements.noaa_enabled.checked,noaa_url:form.elements.noaa_url.value.trim()}};
  setFormBusy(form,true); try { await api("/api/settings",{method:"PUT",body:payload}); app.settingsFormDirty.water=false; await loadState(); await loadWater(); toast("Water settings saved",payload.water.enabled?"Gauge monitoring is active.":"Water monitoring is off."); } catch(error){toast("Water settings were not saved",error.message,"error");} finally{setFormBusy(form,false);}
}
async function saveIntegrationSettings(event){event.preventDefault();const form=event.currentTarget;const payload={...app.state.settings,integrations:{enabled:form.elements.enabled.checked,refresh_minutes:Number(form.elements.refresh_minutes.value),storm_reports_url:form.elements.storm_reports_url.value.trim(),infrastructure_url:form.elements.infrastructure_url.value.trim(),amateur_repeaters_url:form.elements.amateur_repeaters_url.value.trim(),amateur_osm_enabled:form.elements.amateur_osm_enabled.checked,gmrs_repeaters_url:form.elements.gmrs_repeaters_url.value.trim(),meshcore_url:form.elements.meshcore_url.value.trim(),aredn_node_urls:form.elements.aredn_node_urls.value.trim(),sensor_urls:form.elements.sensor_urls.value.trim()}};setFormBusy(form,true);try{await api("/api/settings",{method:"PUT",body:payload});app.settingsFormDirty.integrations=false;await loadState();await loadIntegrations();toast("Integration settings saved",payload.integrations.enabled?"Operational feeds are active.":"Operational feeds are off.")}catch(error){toast("Integration settings were not saved",error.message,"error")}finally{setFormBusy(form,false)}}

async function saveAPRSSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const payload = {
    ...app.state.settings,
    aprs: {
      enabled: form.elements.enabled.checked,
      mode: form.elements.mode.value,
      login_callsign: form.elements.login_callsign.value,
      server: form.elements.server.value,
      extra_filter: form.elements.extra_filter.value,
      watch_callsigns: form.elements.watch_callsigns.value.split(",").map(item => item.trim()).filter(Boolean),
      stale_minutes: Number(form.elements.stale_minutes.value),
      trail_hours: Number(form.elements.trail_hours.value),
      area_enabled: form.elements.area_enabled.checked,
      area_radius_miles: Number(form.elements.area_radius_miles.value),
      passcode: form.elements.passcode.value,
      local: {
        decoder: form.elements.local_decoder.value,
        kiss_address: form.elements.local_kiss_address.value,
        audio_device: form.elements.local_audio_device.value,
        audio_output_device: form.elements.local_audio_output_device.value,
        show_all: form.elements.local_show_all.checked,
        igate_enabled: form.elements.local_decoder.value === "bundled" && form.elements.local_igate_enabled.checked
      }
    }
  };
  setFormBusy(form, true);
  try {
    await api("/api/settings", { method: "PUT", body: payload });
    form.elements.passcode.value = "";
    app.settingsFormDirty.aprs = false;
    toast("APRS settings saved", payload.aprs.enabled ? `${label(payload.aprs.mode)} mode is starting.` : "APRS tracking is off.");
    await loadState();
  } catch (error) {
    toast("APRS settings were not saved", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function reconnectAPRS() {
  const button = $("#aprs-reconnect");
  button.disabled = true;
  try {
    await api("/api/aprs/reconnect", { method: "POST", body: {} });
    toast("APRS reconnect requested", "Connection status will update automatically.");
    setTimeout(() => loadState(), 800);
  } catch (error) {
    toast("APRS reconnect failed", error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function quitApplication() {
  if (!confirm("Quit Tickets Local? APRS tracking and the local server will stop. All saved data will remain available the next time you open the app.")) return;
  const button = $("#quit-application");
  button.disabled = true;
  try {
    await api("/api/shutdown", {
      method: "POST",
      body: { confirm: true },
      headers: { "X-Tickets-Local-Shutdown": "confirm" }
    });
    $("#shutdown-screen").hidden = false;
    document.body.classList.remove("nav-open", "map-expanded");
  } catch (error) {
    button.disabled = false;
    toast("Tickets Local could not quit", error.message, "error");
  }
}

async function geocodeIncident() {
  const form = $("#incident-form");
  const query = [form.elements.address.value, form.elements.city.value, form.elements.region.value, form.elements.postal_code.value].filter(Boolean).join(", ");
  await runGeocode({
    form,
    query,
    button: $("#geocode-button"),
    container: $("#geocode-results"),
    latitudeField: "latitude",
    longitudeField: "longitude",
    label: "Look up address"
  });
}

async function geocodeFacility() {
  const form = $("#facility-form");
  await runGeocode({
    form,
    query: form.elements.address.value,
    button: $("#facility-geocode-button"),
    container: $("#facility-geocode-results"),
    latitudeField: "latitude",
    longitudeField: "longitude",
    label: "Look up address",
    select: result => {
      form.elements.address.value = result.display_name;
    }
  });
}

async function geocodeLocation() {
  const form = $("#location-form");
  const query = [form.elements.address.value, form.elements.city.value, form.elements.region.value, form.elements.postal_code.value].filter(Boolean).join(", ");
  await runGeocode({
    form,
    query,
    button: $("#location-geocode-button"),
    container: $("#location-geocode-results"),
    latitudeField: "latitude",
    longitudeField: "longitude",
    label: "Find coordinates"
  });
}

async function geocodeMapCenter() {
  const form = $("#settings-form");
  await runGeocode({
    form,
    query: form.elements.center_address.value,
    button: $("#center-geocode-button"),
    container: $("#center-geocode-results"),
    latitudeField: "center_lat",
    longitudeField: "center_lon",
    label: "Find on map",
    select: result => {
      form.elements.center_address.value = result.display_name;
      app.map.center = [result.latitude, result.longitude];
      app.map.zoom = Number(form.elements.default_zoom.value || app.state.settings.default_zoom);
      app.map.draw(app.state);
    }
  });
}

async function runGeocode({ form, query, button, container, latitudeField, longitudeField, label: buttonLabel, select }) {
  if (query.trim().length < 3) {
    toast("Enter a location first", "Add an address, intersection, landmark, or city.", "error");
    return;
  }
  button.disabled = true;
  button.textContent = "Searching…";
  try {
    const results = await api(`/api/geocode?q=${encodeURIComponent(query)}`);
    container.innerHTML = results.length
      ? `<p class="geocode-guidance">Select the matching location:</p>${results.map((result, index) => `<button type="button" class="geocode-result" data-result-index="${index}"><strong>Use this location</strong><span>${html(result.display_name)}</span></button>`).join("")}`
      : `<div class="no-options">No matching locations found. Coordinates can be entered manually.</div>`;
    container.classList.add("visible");
    $$("[data-result-index]", container).forEach(item => item.addEventListener("click", () => {
      const result = results[Number(item.dataset.resultIndex)];
      applyCoordinateResult(form, result, latitudeField, longitudeField);
      if (select) select(result);
      container.classList.remove("visible");
      toast("Coordinates selected", result.display_name);
    }));
  } catch (error) {
    toast("Location search failed", error.message, "error");
  } finally {
    button.disabled = false;
    button.innerHTML = `<svg><use href="#i-map-pin"></use></svg>${html(buttonLabel)}`;
  }
}

function applyCoordinateResult(form, result, latitudeField = "latitude", longitudeField = "longitude") {
  form.elements[latitudeField].value = result.latitude;
  form.elements[longitudeField].value = result.longitude;
  if (form.id === "settings-form") app.settingsFormDirty.general = true;
}

async function resolveCoordinatesForSave(form, query) {
  const latitude = optionalNumber(form.elements.latitude.value);
  const longitude = optionalNumber(form.elements.longitude.value);
  if ((latitude == null) !== (longitude == null)) {
    throw new Error("Enter both latitude and longitude, or clear both fields.");
  }
  if (latitude != null && longitude != null) {
    return { mapped: true, automatic: false, warning: "" };
  }
  if (query.trim().length < 3) {
    return { mapped: false, automatic: false, warning: "The location was too short to look up." };
  }
  try {
    const results = await api(`/api/geocode?q=${encodeURIComponent(query)}`);
    if (!results.length) {
      return { mapped: false, automatic: false, warning: "No matching address was found." };
    }
    applyCoordinateResult(form, results[0]);
    return { mapped: true, automatic: true, warning: "", result: results[0] };
  } catch (error) {
    return { mapped: false, automatic: false, warning: error.message };
  }
}

function renderOverlayList() {
  const overlays = app.state.overlays || [];
  $("#overlay-list").innerHTML = overlays.length
    ? overlays.map(overlay => {
      const points = overlay.features.reduce((sum, feature) => sum + feature.paths.reduce((pathSum, path) => pathSum + path.length, 0), 0);
      const hasKMLColors = overlay.file_name !== "Map drawing" && overlay.features.some(feature => feature.color);
      return `<div class="overlay-row" data-overlay-id="${attr(overlay.id)}">
        <input class="overlay-toggle" type="checkbox" aria-label="Show ${attr(overlay.name)}" ${overlay.visible ? "checked" : ""}>
        <div class="overlay-copy"><strong>${html(overlay.name)}</strong><small>${html(overlay.file_name)} · ${overlay.features.length} features · ${points.toLocaleString()} points</small></div>
        <input class="overlay-color" type="color" value="${attr(overlay.color)}" aria-label="Color for ${attr(overlay.name)}">
        ${hasKMLColors ? `<label class="overlay-kml-style"><input class="overlay-kml-colors" type="checkbox" ${overlay.use_kml_styles ? "checked" : ""}> Use KML colors</label>` : ""}
        ${overlay.file_name !== "Map drawing" ? `<label class="overlay-opacity"><span>Opacity</span><input class="overlay-opacity-range" type="range" min="10" max="100" step="5" value="${Number(overlay.opacity) || 100}" aria-label="Opacity for ${attr(overlay.name)}"><output>${Number(overlay.opacity) || 100}%</output></label>` : ""}
        <button class="row-action overlay-zoom" type="button" data-overlay-action="zoom">Zoom</button>
        <button class="row-action overlay-delete" type="button" data-overlay-action="delete" aria-label="Delete ${attr(overlay.name)}">Delete</button>
      </div>`;
    }).join("")
    : `<div class="overlay-empty">No KML overlays imported.</div>`;
}

async function importKML(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const file = form.elements.file.files[0];
  if (!file) return;
  if (file.size > 5 * 1024 * 1024) {
    toast("KML was not imported", "The file must be 5 MB or smaller.", "error");
    return;
  }
  const body = new FormData(form);
  setFormBusy(form, true);
  try {
    const response = await fetch("/api/overlays/import", { method: "POST", headers: { Accept: "application/json" }, body });
    const payload = await response.json().catch(() => null);
    if (!response.ok) throw new Error(payload?.error || `Request failed (${response.status})`);
    form.reset();
    form.elements.color.value = "#a78bfa";
    toast("KML overlay imported", payload.name);
    await loadState();
  } catch (error) {
    toast("KML was not imported", error.message, "error");
  } finally {
    setFormBusy(form, false);
  }
}

async function updateOverlayFromEvent(event) {
  if (!event.target.matches(".overlay-toggle, .overlay-color, .overlay-kml-colors, .overlay-opacity-range")) return;
  const row = event.target.closest("[data-overlay-id]");
  const overlay = (app.state.overlays || []).find(item => item.id === row?.dataset.overlayId);
  if (!row || !overlay) return;
  const kmlColors = $(".overlay-kml-colors", row);
  if (event.target.matches(".overlay-color") && kmlColors) kmlColors.checked = false;
  const payload = {
    name: overlay.name,
    color: $(".overlay-color", row).value,
    use_kml_styles: Boolean(kmlColors?.checked),
    opacity: Number($(".overlay-opacity-range", row)?.value || overlay.opacity || 100),
    visible: $(".overlay-toggle", row).checked,
    expected_updated_at: overlay.updated_at
  };
  try {
    await api(`/api/overlays/${encodeURIComponent(overlay.id)}`, { method: "PUT", body: payload });
    await loadState();
  } catch (error) {
    toast("Overlay was not updated", error.message, "error");
    await loadState();
  }
}

async function overlayActionFromEvent(event) {
  const button = event.target.closest("[data-overlay-action]");
  if (!button) return;
  const row = button.closest("[data-overlay-id]");
  const overlay = (app.state.overlays || []).find(item => item.id === row?.dataset.overlayId);
  if (!overlay) return;
  if (button.dataset.overlayAction === "zoom") {
    if (!overlay.visible) {
      await api(`/api/overlays/${encodeURIComponent(overlay.id)}`, {
        method: "PUT",
        body: { name: overlay.name, color: overlay.color, use_kml_styles: Boolean(overlay.use_kml_styles), opacity: Number(overlay.opacity) || 100, visible: true, expected_updated_at: overlay.updated_at }
      });
      await loadState();
    }
    switchPage("dashboard");
    app.map.focusOverlay(overlay.id, app.state);
    return;
  }
  if (button.dataset.overlayAction === "delete" && confirm(`Delete the ${overlay.name} overlay?`)) {
    try {
      await api(`/api/overlays/${encodeURIComponent(overlay.id)}`, { method: "DELETE" });
      toast("Overlay deleted", overlay.name);
      await loadState();
    } catch (error) {
      toast("Overlay was not deleted", error.message, "error");
    }
  }
}

async function addDemoData() {
  if (!confirm("Add sample dispatch data? This is available only while the system is completely empty.")) return;
  try {
    await api("/api/demo", { method: "POST", body: {} });
    toast("Sample data added", "The situation board is ready to explore.");
    await loadState();
    switchPage("dashboard");
  } catch (error) {
    toast("Sample data was not added", error.message, "error");
  }
}

function connectEvents() {
  const stream = new EventSource("/api/events");
  stream.addEventListener("open", () => setConnectionState(true));
  stream.addEventListener("error", () => setConnectionState(false));
  stream.addEventListener("change", () => {
    clearTimeout(app.refreshTimer);
    app.refreshTimer = setTimeout(() => loadState(), 120);
  });
}

function startAPRSStatusPoll() {
  const refresh = async () => {
    try {
      app.state.aprs_status = await api("/api/aprs/status");
      if (app.page === "settings") renderAPRSStatus();
      if (app.page === "dashboard") renderDashboardAPRS();
    } catch {
      // The main live indicator already reports a local-server interruption.
    }
  };
  setTimeout(refresh, 800);
  setInterval(refresh, 5000);
}

function startResponderPositionPoll() {
  const refresh = () => {
    if (!document.hidden) loadState();
  };
  setInterval(refresh, 60000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
}

function startLANStatePoll() {
  setInterval(() => {
    if (app.network.active_mode === "client" && !document.hidden) loadState();
  }, 10000);
}

function startNetworkStatusPoll() {
  setInterval(async () => {
    try {
      app.network = await api("/api/network/status");
      if (app.page === "settings") {
        renderNetworkSettings();
        await loadMobileDevices();
      }
    } catch {
      // The local application health indicator covers a status interruption.
    }
  }, 5000);
  setInterval(() => {
    if (!document.hidden && app.page === "settings") loadConnections();
  }, 15000);
}

function startWeatherPoll() {
  setInterval(() => {
    if (!document.hidden) { loadWeather(); loadWater(); loadIntegrations(); }
  }, 60000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) { loadWeather(); loadWater(); loadIntegrations(); }
  });
}

function setConnectionState(connected) {
  const state = $("#sync-state");
  state.classList.toggle("offline", !connected);
  state.lastChild.textContent = connected ? " Live" : " Reconnecting";
}

function activeIncidents() {
  return app.state.incidents.filter(item => activeStatuses.has(item.status)).sort(sortIncidents);
}

function incidentStaleMinutes() {
  return Math.max(1, Number(app.state.settings.incident_stale_minutes) || 10);
}

function isIncidentStale(incident) {
  return Boolean(
    incident &&
    activeStatuses.has(incident.status) &&
    Date.now() - new Date(incident.updated_at).getTime() >= incidentStaleMinutes() * 60000
  );
}

function nextIncidentStatus(status) {
  return { new: "assigned", assigned: "enroute", enroute: "onscene", onscene: "closed" }[status] || "";
}

function sortIncidents(a, b) {
  const status = Number(activeStatuses.has(b.status)) - Number(activeStatuses.has(a.status));
  return status || severityRank[a.severity] - severityRank[b.severity] || new Date(b.created_at) - new Date(a.created_at);
}

function matchesSearch(item) {
  if (!app.search) return true;
  return JSON.stringify(item).toLowerCase().includes(app.search);
}

function responderByID(id) {
  return app.state.responders.find(item => item.id === id);
}

function displayResponder(responder) {
  return responder.callsign || responder.name || "Responder";
}

function responderType(responder) {
  return String(responder.type || "").trim() || "Unspecified";
}

function responderAPRSLabel(responder) {
  if (!responder.aprs_enabled) return "";
  if (!responder.position_updated_at) return "APRS waiting for position";
  const staleMinutes = app.state.settings.aprs?.stale_minutes || 15;
  const stale = Date.now() - new Date(responder.position_updated_at).getTime() > staleMinutes * 60000;
  return `APRS ${stale ? "stale · " : ""}${relativeTime(responder.position_updated_at)}`;
}

function locationText(incident) {
  return [incident.address, incident.city, incident.region].filter(Boolean).join(", ");
}

function setForm(form, record) {
  Object.entries(record).forEach(([key, value]) => {
    if (!form.elements[key] || Array.isArray(value) || typeof value === "object") return;
    form.elements[key].value = value ?? "";
  });
  if (form.elements.id) form.elements.id.value = record.id || "";
  if (form.elements.latitude) form.elements.latitude.value = record.latitude ?? "";
  if (form.elements.longitude) form.elements.longitude.value = record.longitude ?? "";
}

function resetRestoreValidation() {
  const report = $("#restore-report");
  report.className = "restore-report";
  report.textContent = "No restore file validated.";
  delete report.dataset.sha256;
  $("#apply-restore").disabled = true;
}

async function validateRestoreFile() {
  const file = $("#restore-event-file").files[0];
  const report = $("#restore-report");
  if (!file) {
    toast("Select a backup", "Choose an NDJSON event-log backup first.", "error");
    return;
  }
  const button = $("#validate-restore");
  button.disabled = true;
  report.className = "restore-report";
  report.textContent = "Validating without changing operational data…";
  try {
    const data = new FormData();
    data.append("file", file, file.name);
    const response = await fetch("/api/restore/validate", { method: "POST", headers: { Accept: "application/json" }, body: data });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `Validation failed (${response.status})`);
    report.dataset.sha256 = result.sha256;
    report.className = "restore-report success";
    report.textContent = `${result.message} ${result.incidents} incidents · ${result.responders} responders · ${result.facilities} facilities · ${result.locations} locations · ${result.overlays} overlays · ${result.activity} activity records · ${result.attachment_refs} attachment references (files are not imported by event-log restore).`;
    $("#apply-restore").disabled = false;
  } catch (error) {
    delete report.dataset.sha256;
    report.className = "restore-report error";
    report.textContent = error.message;
    $("#apply-restore").disabled = true;
  } finally {
    button.disabled = false;
  }
}

async function applyRestoreFile() {
  const file = $("#restore-event-file").files[0];
  const report = $("#restore-report");
  if (!file || !report.dataset.sha256) return;
  if (!window.confirm("Replace the current operational event log with this validated backup? Tickets Local will preserve the current log first.")) return;
  const button = $("#apply-restore");
  button.disabled = true;
  try {
    const data = new FormData();
    data.append("file", file, file.name);
    data.append("sha256", report.dataset.sha256);
    data.append("confirm", "RESTORE");
    const response = await fetch("/api/restore/apply", { method: "POST", headers: { Accept: "application/json" }, body: data });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `Restore failed (${response.status})`);
    report.className = "restore-report success";
    report.textContent = result.message;
    await loadState();
    toast("Restore completed", "The previous event log was preserved in automatic snapshots.");
    delete report.dataset.sha256;
  } catch (error) {
    report.className = "restore-report error";
    report.textContent = error.message;
    button.disabled = false;
    toast("Restore was not applied", error.message, "error");
  }
}

function setFormBusy(form, busy) {
  $$("button, input, select, textarea", form).forEach(control => control.disabled = busy);
}

async function api(url, options = {}) {
  const request = { method: options.method || "GET", headers: { Accept: "application/json", ...(options.headers || {}) } };
  if (app.mobileMode && !options.skipMobileAuth) {
    const deviceKey = localStorage.getItem("tickets-local-mobile-device-key") || "";
    const mobileKey = localStorage.getItem("tickets-local-mobile-lan-key") || "";
    if (deviceKey) request.headers["X-Tickets-Local-Device-Key"] = deviceKey;
    else if (mobileKey) request.headers["X-Tickets-Local-LAN-Key"] = mobileKey;
    request.headers["X-Tickets-Local-Client"] = "Mobile companion";
  }
  if (options.body !== undefined) {
    request.headers["Content-Type"] = "application/json";
    request.body = JSON.stringify(options.body);
  }
  const response = await fetch(url, request);
  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : null;
  if (!response.ok) {
    const error = new Error(payload?.error || `Request failed (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return payload;
}

function optionalNumber(value) {
  return value === "" ? null : Number(value);
}

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  $("#theme-button use")?.setAttribute("href", theme === "dark" ? "#i-sun" : "#i-moon");
}

function startClock() {
  const update = () => {
    const now = new Date();
    $("#current-time").innerHTML = `${now.toLocaleDateString([], { weekday: "long", month: "long", day: "numeric" })}<br>${now.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" })}`;
    if (now.getSeconds() % 15 === 0 && app.page === "dashboard") renderIncidentUpdateAlert();
  };
  update();
  setInterval(update, 1000);
}

function elapsed(value) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

function relativeTime(value) {
  const seconds = Math.floor((Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 15) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 604800) return `${Math.floor(seconds / 86400)}d ago`;
  return new Date(value).toLocaleDateString();
}

function formatDateTime(value) {
  return new Date(value).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

function label(value) {
  return String(value || "").replaceAll("_", " ").replace(/\b\w/g, letter => letter.toUpperCase());
}

function initials(value) {
  return value.split(/\s+/).map(item => item[0]).join("").slice(0, 3).toUpperCase();
}

function html(value) {
  return String(value ?? "").replace(/[&<>"']/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#039;" })[char]);
}

function attr(value) {
  return html(value);
}

function toast(title, message = "", type = "success") {
  const item = document.createElement("div");
  item.className = `toast ${type}`;
  item.innerHTML = `<div><strong>${html(title)}</strong>${message ? `<p>${html(message)}</p>` : ""}</div>`;
  $("#toast-stack").append(item);
  setTimeout(() => item.remove(), 5000);
}

function openMapWindow(event) {
  const url = new URL("/", window.location.origin);
  url.searchParams.set("view", "map");
  if (event?.currentTarget instanceof HTMLAnchorElement) event.currentTarget.href = url.toString();
  const left = Number.isFinite(screen.availLeft) ? screen.availLeft : 0;
  const top = Number.isFinite(screen.availTop) ? screen.availTop : 0;
  const mapWindow = window.open(
    url.toString(),
    "tickets-local-situation-map",
    `popup=yes,width=${screen.availWidth},height=${screen.availHeight},left=${left},top=${top},resizable=yes,scrollbars=no`
  );
  if (!mapWindow) return;
  event?.preventDefault();
  try {
    mapWindow.moveTo(left, top);
    mapWindow.resizeTo(screen.availWidth, screen.availHeight);
    mapWindow.focus();
  } catch (_) {
    // Browsers may ignore window placement while still opening the map correctly.
  }
}

async function toggleDocumentFullscreen() {
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else {
      await document.documentElement.requestFullscreen();
    }
  } catch (error) {
    toast("Full screen is unavailable", error.message, "error");
  }
}

function updateMapFullscreenButtons() {
  $$("[data-map-fullscreen]").forEach(button => {
    button.textContent = document.fullscreenElement ? "Exit full screen" : "Full screen";
  });
}

function loadMapLayers() {
  const defaults = { radar:true, alerts:true, incident:true, responder:true, facility:true, location:true, aprs:true, water:true, storm:true, infrastructure:true, amateur_repeater:true, gmrs_repeater:true, meshcore:true, sensor:true };
  try { return { ...defaults, ...JSON.parse(localStorage.getItem("tickets-local-map-layers") || "{}") }; }
  catch { return defaults; }
}

class SituationMap {
  constructor(element) {
    this.element = element;
    this.tileLayer = $(".map-tiles", element);
    this.radarLayer = $(".map-radar", element);
    this.trailLayer = $(".map-trails", element);
    this.markerLayer = $(".map-markers", element);
    this.unmappedLayer = $("#map-unmapped", element);
    this.center = [39.8283, -98.5795];
    this.zoom = 4;
    this.drag = null;
    this.wheelDelta = 0;
    this.tiles = new Map();
    this.renderFrame = null;
    this.radarTimer = null;
    this.radarKey = "";
    this.radarPendingKey = "";
    this.radarLoadedAt = 0;
    this.radarObjectURL = "";
    this.radarRequest = 0;
    this.radarFrames = [];
    this.radarFrameIndex = 0;
    this.radarPlaybackTimer = null;
    this.radarPaused = false;
    this.radarPlayButton = $("[data-radar-play]", element);
    this.radarStatus = $("[data-map-radar-status]", element);
    this.layers = loadMapLayers();
    this.hasFitInitialObjects = false;
    this.drawing = null;
    this.route = null;
    this.drawingControl = $(".map-drawing-control", element);
    this.drawingEditor = $(".map-drawing-editor", element);
    this.weatherBadge = $("#map-weather-badge", element);
    this.mapHint = $("[data-map-hint]", element);
    this.pendingState = null;
    this.suppressClick = false;
    this.positioningResponderID = "";
    this.panel = element.closest(".map-panel");
    this.expandButtons = $$("[data-map-expand]", this.panel);
    $("[data-map-zoom='in']", element).addEventListener("click", () => {
      this.zoomBy(1);
    });
    $("[data-map-zoom='out']", element).addEventListener("click", () => {
      this.zoomBy(-1);
    });
    $("[data-map-fit]", element).addEventListener("click", () => this.fitObjects(app.state));
    $("[data-map-home]", element).addEventListener("click", () => this.resetView(app.state));
    $("[data-map-draw-toggle]", element).addEventListener("click", () => {
      this.drawingEditor.hidden = false;
      $("[data-map-draw-toggle]", element).hidden = true;
    });
    $("[data-map-draw-start]", element).addEventListener("click", () => this.startDrawing());
    $("[data-map-draw-undo]", element).addEventListener("click", () => this.undoDrawing());
    $("[data-map-draw-save]", element).addEventListener("click", () => this.saveDrawing());
    $("[data-map-draw-cancel]", element).addEventListener("click", () => this.cancelDrawing());
    $("[data-map-route-start]", element).addEventListener("click", () => this.startRoute());
    $("[data-map-route-clear]", element).addEventListener("click", () => this.clearRoute());
    $("[data-map-manage-overlays]", element).addEventListener("click", () => {
      switchPage("settings");
      requestAnimationFrame(() => document.getElementById("overlay-settings")?.scrollIntoView({ behavior: "smooth", block: "start" }));
    });
    this.trailLayer.addEventListener("pointerdown", event => {
      if (event.target.closest("[data-drawn-overlay-id]")) event.stopPropagation();
    });
    this.trailLayer.addEventListener("click", event => this.deleteDrawingFromEvent(event));
    this.trailLayer.addEventListener("click", event => this.showAlertDetails(event));
    this.trailLayer.addEventListener("keydown", event => {
      if ((event.key === "Enter" || event.key === " ") && event.target.closest("[data-drawn-overlay-id]")) {
        event.preventDefault();
        this.deleteDrawingFromEvent(event);
      }
      if ((event.key === "Enter" || event.key === " ") && event.target.closest("[data-weather-alert-label]")) {
        event.preventDefault();
        this.showAlertDetails(event);
      }
    });
    this.radarPlayButton.addEventListener("click", () => this.toggleRadarPlayback());
    $$('[data-map-layer]',element).forEach(input=>{input.checked=this.layers[input.dataset.mapLayer]!==false;input.addEventListener("change",()=>{this.layers[input.dataset.mapLayer]=input.checked;localStorage.setItem("tickets-local-map-layers",JSON.stringify(this.layers));this.scheduleDraw(app.state);});});
    this.expandButtons.forEach(button => button.addEventListener("click", () => this.toggleExpanded()));
    element.addEventListener("wheel", event => {
      if (event.target.closest(".map-controls, .map-layer-control")) return;
      event.preventDefault();
      const normalized = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY;
      if (this.wheelDelta && Math.sign(this.wheelDelta) !== Math.sign(normalized)) {
        this.wheelDelta = 0;
      }
      this.wheelDelta += normalized;
      if (Math.abs(this.wheelDelta) < 18) return;
      this.zoomBy(this.wheelDelta > 0 ? 1 : -1, event.clientX, event.clientY);
      this.wheelDelta = 0;
    }, { passive: false });
    element.addEventListener("pointerdown", event => this.startDrag(event));
    element.addEventListener("pointermove", event => this.moveDrag(event));
    element.addEventListener("pointerup", event => this.endDrag(event));
    element.addEventListener("pointercancel", event => this.endDrag(event));
    element.addEventListener("click", event => {
      if (!this.suppressClick) return;
      event.preventDefault();
      event.stopImmediatePropagation();
    }, true);
    element.addEventListener("click", event => this.positionResponderFromClick(event));
    element.addEventListener("click", event => this.addDrawingPoint(event));
    element.addEventListener("dblclick", event => {
      if (event.target.closest(".map-controls, .map-layer-control, .map-marker")) return;
      event.preventDefault();
      this.zoomBy(1, event.clientX, event.clientY);
    });
    element.addEventListener("keydown", event => this.handleKey(event));
    new ResizeObserver(() => this.scheduleDraw(app.state)).observe(element);
  }

  render(state) {
    const mapSettings = JSON.stringify([
      state.settings.center_lat,
      state.settings.center_lon,
      state.settings.default_zoom
    ]);
    const settingsChanged = this.lastSettings !== mapSettings;
    if (settingsChanged) {
      this.center = [state.settings.center_lat, state.settings.center_lon];
      this.zoom = state.settings.default_zoom;
      this.lastSettings = mapSettings;
    }
    const drawingCount = (state.overlays || []).filter(overlay => overlay.file_name === "Map drawing").length;
    $("[data-map-manage-overlays]", this.element).hidden = drawingCount === 0;
    this.mapHint.textContent = this.positioningResponderID
      ? "Click the map to place the responder · Press Escape to cancel"
      : drawingCount
        ? `${drawingCount} saved drawing${drawingCount === 1 ? "" : "s"} · Click a shape to delete · Manage drawings for a list`
        : "Drag responder markers to reposition · Scroll to zoom · Double-click to zoom";
    this.scheduleDraw(state);
  }

  resetView(state) {
    this.center = [state.settings.center_lat, state.settings.center_lon];
    this.zoom = state.settings.default_zoom;
    this.scheduleDraw(state);
  }

  fitObjects(state, schedule = true) {
    const coordinates = mapPoints(state)
      .filter(point => this.layers[point.kind] !== false)
      .map(point => [Number(point.latitude), Number(point.longitude)])
      .filter(([latitude, longitude]) => Number.isFinite(latitude) && Number.isFinite(longitude) && Math.abs(latitude) <= 85.05112878 && Math.abs(longitude) <= 180);
    if (!coordinates.length) return false;

    const availableWidth = Math.max(160, this.element.clientWidth - 120);
    const availableHeight = Math.max(120, this.element.clientHeight - 120);
    const maximumZoom = coordinates.length === 1 ? 13 : 16;
    for (let zoom = maximumZoom; zoom >= 2; zoom--) {
      const worldSize = 256 * 2 ** zoom;
      const projected = coordinates.map(([latitude, longitude]) => project(latitude, longitude, zoom));
      const anchorX = projected[0].x;
      const adjustedX = projected.map(point => {
        let x = point.x;
        while (x - anchorX > worldSize / 2) x -= worldSize;
        while (x - anchorX < -worldSize / 2) x += worldSize;
        return x;
      });
      const minX = Math.min(...adjustedX);
      const maxX = Math.max(...adjustedX);
      const minY = Math.min(...projected.map(point => point.y));
      const maxY = Math.max(...projected.map(point => point.y));
      if (maxX - minX <= availableWidth && maxY - minY <= availableHeight) {
        this.center = unproject((minX + maxX) / 2, (minY + maxY) / 2, zoom);
        this.zoom = zoom;
        if (schedule) this.scheduleDraw(state);
        return true;
      }
    }
    return false;
  }

  zoomBy(amount, clientX = null, clientY = null) {
    const next = Math.max(2, Math.min(18, this.zoom + amount));
    if (next === this.zoom) return;
    if (clientX != null && clientY != null) {
      const rect = this.element.getBoundingClientRect();
      const offsetX = clientX - rect.left - rect.width / 2;
      const offsetY = clientY - rect.top - rect.height / 2;
      const currentCenter = project(this.center[0], this.center[1], this.zoom);
      const anchor = unproject(currentCenter.x + offsetX, currentCenter.y + offsetY, this.zoom);
      const nextAnchor = project(anchor[0], anchor[1], next);
      this.center = unproject(nextAnchor.x - offsetX, nextAnchor.y - offsetY, next);
    }
    this.zoom = next;
    this.scheduleDraw(app.state);
  }

  startDrag(event) {
    if (this.drawing || this.route?.selecting || event.button !== 0 || !event.isPrimary || event.target.closest(".map-controls, .map-layer-control, .map-drawing-control, .map-unmapped")) return;
    const projected = project(this.center[0], this.center[1], this.zoom);
    const startedOnMarker = Boolean(event.target.closest(".map-marker"));
    this.drag = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      centerX: projected.x,
      centerY: projected.y,
      moved: false,
      captured: false,
      startedOnMarker
    };
    if (!startedOnMarker) this.capturePointer(event.pointerId);
    this.element.focus({ preventScroll: true });
    this.element.classList.add("is-dragging");
    if (!startedOnMarker) event.preventDefault();
  }

  moveDrag(event) {
    if (!this.drag || this.drag.pointerId !== event.pointerId) return;
    const deltaX = event.clientX - this.drag.startX;
    const deltaY = event.clientY - this.drag.startY;
    if (!this.drag.moved && Math.hypot(deltaX, deltaY) >= 3) {
      this.drag.moved = true;
      this.capturePointer(event.pointerId);
    }
    if (!this.drag.moved) return;
    const x = this.drag.centerX - deltaX;
    const y = this.drag.centerY - deltaY;
    this.center = unproject(x, y, this.zoom);
    this.scheduleDraw(app.state);
    event.preventDefault();
  }

  endDrag(event) {
    if (!this.drag || this.drag.pointerId !== event.pointerId) return;
    const moved = this.drag.moved;
    if (this.drag.captured && this.element.hasPointerCapture(event.pointerId)) {
      this.element.releasePointerCapture(event.pointerId);
    }
    this.drag = null;
    this.element.classList.remove("is-dragging");
    if (moved) {
      this.suppressClick = true;
      setTimeout(() => {
        this.suppressClick = false;
      }, 0);
      event.preventDefault();
    }
  }

  capturePointer(pointerId) {
    if (!this.drag || this.drag.captured) return;
    try {
      this.element.setPointerCapture(pointerId);
      this.drag.captured = true;
    } catch {
      this.drag.captured = false;
    }
  }

  panBy(x, y) {
    const center = project(this.center[0], this.center[1], this.zoom);
    this.center = unproject(center.x + x, center.y + y, this.zoom);
    this.scheduleDraw(app.state);
  }

  focusPoint(latitude, longitude, zoom = 14) {
    this.center = [Number(latitude), Number(longitude)];
    this.zoom = Math.max(this.zoom, Math.min(18, zoom));
    this.scheduleDraw(app.state);
  }

  startResponderPosition(id) {
    const responder = app.state.responders.find(item => item.id === id);
    if (!responder) return;
    this.cancelDrawing();
    this.clearRoute();
    this.positioningResponderID = id;
    this.element.classList.add("is-positioning-responder");
    if (responder.latitude != null && responder.longitude != null) this.focusPoint(responder.latitude, responder.longitude, 14);
    this.render(app.state);
    this.element.focus({ preventScroll: true });
  }

  coordinateAt(clientX, clientY) {
    const rect = this.element.getBoundingClientRect();
    const center = project(this.center[0], this.center[1], this.zoom);
    return unproject(center.x + clientX - rect.left - rect.width / 2, center.y + clientY - rect.top - rect.height / 2, this.zoom);
  }

  async saveResponderPosition(id, coordinate) {
    const responder = app.state.responders.find(item => item.id === id);
    if (!responder) return;
    try {
      await api(`/api/responders/${encodeURIComponent(id)}/position`, { method: "POST", body: {
        latitude: coordinate[0], longitude: coordinate[1], source: "map", expected_updated_at: responder.updated_at
      }});
      toast("Responder position saved", `${coordinate[0].toFixed(5)}, ${coordinate[1].toFixed(5)}`);
      await loadState();
    } catch (error) {
      toast("Responder position was not saved", error.message, "error");
      this.scheduleDraw(app.state);
    }
  }

  positionResponderFromClick(event) {
    if (!this.positioningResponderID || event.target.closest(".map-controls, .map-layer-control, .map-drawing-control, .map-marker")) return;
    const id = this.positioningResponderID;
    this.positioningResponderID = "";
    this.element.classList.remove("is-positioning-responder");
    this.saveResponderPosition(id, this.coordinateAt(event.clientX, event.clientY));
    event.preventDefault();
    event.stopImmediatePropagation();
  }

  makeResponderDraggable(marker, point) {
    marker.addEventListener("pointerdown", event => {
      if (event.button !== 0 || !event.isPrimary || this.positioningResponderID || this.drawing || this.route?.selecting) return;
      event.stopPropagation();
      const origin = { x: event.clientX, y: event.clientY };
      let moved = false;
      marker.setPointerCapture(event.pointerId);
      const move = moveEvent => {
        if (moveEvent.pointerId !== event.pointerId) return;
        if (!moved && Math.hypot(moveEvent.clientX - origin.x, moveEvent.clientY - origin.y) < 4) return;
        moved = true;
        const rect = this.element.getBoundingClientRect();
        marker.classList.add("dragging");
        marker.style.left = `${moveEvent.clientX - rect.left}px`;
        marker.style.top = `${moveEvent.clientY - rect.top}px`;
        moveEvent.preventDefault();
      };
      const finish = upEvent => {
        if (upEvent.pointerId !== event.pointerId) return;
        marker.removeEventListener("pointermove", move);
        marker.removeEventListener("pointerup", finish);
        marker.removeEventListener("pointercancel", finish);
        marker.classList.remove("dragging");
        if (moved) {
          marker.dataset.dragged = "true";
          this.saveResponderPosition(point.id, this.coordinateAt(upEvent.clientX, upEvent.clientY));
          upEvent.preventDefault();
          upEvent.stopPropagation();
        }
      };
      marker.addEventListener("pointermove", move);
      marker.addEventListener("pointerup", finish);
      marker.addEventListener("pointercancel", finish);
    });
  }

  startDrawing() {
    this.drawing = {
      type: $("[data-map-draw-type]", this.element).value,
      name: $("[data-map-draw-name]", this.element).value.trim() || "Map annotation",
      color: $("[data-map-draw-color]", this.element).value,
      points: []
    };
    this.element.classList.add("is-drawing");
    this.updateDrawingControls();
    this.scheduleDraw(app.state);
  }

  addDrawingPoint(event) {
    if ((!this.drawing && !this.route?.selecting) || event.target.closest(".map-controls, .map-layer-control, .map-drawing-control, .map-marker")) return;
    const rect = this.element.getBoundingClientRect();
    const center = project(this.center[0], this.center[1], this.zoom);
    const coordinate = unproject(center.x + event.clientX - rect.left - rect.width / 2, center.y + event.clientY - rect.top - rect.height / 2, this.zoom);
    const point = { latitude: coordinate[0], longitude: coordinate[1] };
    if (this.route?.selecting) {
      this.route.points.push(point);
      if (this.route.points.length === 2) this.calculateRoute();
    } else {
      this.drawing.points.push(point);
      this.updateDrawingControls();
    }
    this.scheduleDraw(app.state);
    event.preventDefault();
  }

  undoDrawing() {
    if (!this.drawing) return;
    this.drawing.points.pop();
    this.updateDrawingControls();
    this.scheduleDraw(app.state);
  }

  updateDrawingControls() {
    const count = this.drawing?.points.length || 0;
    const minimum = this.drawing?.type === "point" ? 1 : this.drawing?.type === "line" ? 2 : 3;
    $("[data-map-draw-undo]", this.element).disabled = count === 0;
    $("[data-map-draw-save]", this.element).disabled = count < minimum;
    $("[data-map-draw-help]", this.element).textContent = this.drawing
      ? `${count} point${count === 1 ? "" : "s"} selected · ${count < minimum ? `add ${minimum - count} more` : "ready to save"}`
      : "Select Start, then click the map.";
  }

  cancelDrawing() {
    this.drawing = null;
    this.element.classList.remove("is-drawing");
    this.drawingEditor.hidden = true;
    $("[data-map-draw-toggle]", this.element).hidden = false;
    this.updateDrawingControls();
    this.scheduleDraw(app.state);
  }

  startRoute() {
    this.cancelDrawing();
    this.route = { selecting: true, points: [], path: [] };
    this.element.classList.add("is-drawing");
    const status = $("[data-map-route-status]", this.element);
    status.hidden = false;
    status.textContent = "Click a route start, then a destination.";
    $("[data-map-route-clear]", this.element).hidden = false;
    this.scheduleDraw(app.state);
  }

  async calculateRoute() {
    this.route.selecting = false;
    this.element.classList.remove("is-drawing");
    const status = $("[data-map-route-status]", this.element);
    status.textContent = "Calculating route…";
    try {
      const result = await api("/api/map/route", { method: "POST", body: { start: this.route.points[0], end: this.route.points[1] } });
      this.route.path = result.path;
      status.textContent = `${result.distance_miles.toFixed(1)} mi · about ${Math.round(result.duration_minutes)} min · ${result.source}`;
      this.scheduleDraw(app.state);
    } catch (error) {
      status.textContent = error.message;
      toast("Route unavailable", error.message, "error");
    }
  }

  clearRoute() {
    this.route = null;
    this.element.classList.remove("is-drawing");
    $("[data-map-route-status]", this.element).hidden = true;
    $("[data-map-route-clear]", this.element).hidden = true;
    this.scheduleDraw(app.state);
  }

  async saveDrawing() {
    if (!this.drawing) return;
    const drawing = this.drawing;
    const paths = [[...drawing.points]];
    if (drawing.type === "polygon") paths[0].push(drawing.points[0]);
    try {
      await api("/api/overlays", { method: "POST", body: {
        name: drawing.name,
        color: drawing.color,
        visible: true,
        features: [{ name: drawing.name, geometry_type: drawing.type, paths }]
      }});
      this.cancelDrawing();
      await loadState();
      toast("Map drawing saved", `${drawing.name}. Click the saved shape to delete it.`);
    } catch (error) {
      toast("Could not save drawing", error.message, "error");
    }
  }

  async deleteDrawingFromEvent(event) {
    const shape = event.target.closest("[data-drawn-overlay-id]");
    if (!shape || this.drawing || this.route?.selecting) return;
    event.preventDefault();
    event.stopPropagation();
    const overlay = (app.state.overlays || []).find(item => item.id === shape.dataset.drawnOverlayId);
    if (!overlay || overlay.file_name !== "Map drawing" || !confirm(`Delete the ${overlay.name} drawing?`)) return;
    try {
      await api(`/api/overlays/${encodeURIComponent(overlay.id)}`, { method: "DELETE" });
      toast("Map drawing deleted", overlay.name);
      await loadState();
    } catch (error) {
      toast("Could not delete drawing", error.message, "error");
    }
  }

  showAlertDetails(event) {
    const shape = event.target.closest("[data-weather-alert-label]");
    if (!shape || this.drawing || this.route?.selecting) return;
    event.preventDefault();
    event.stopPropagation();
    toast("Weather warning", shape.dataset.weatherAlertLabel);
  }

  toggleExpanded() {
    const expanded = !this.panel.classList.contains("is-expanded");
    this.panel.classList.toggle("is-expanded", expanded);
    document.body.classList.toggle("map-expanded", expanded);
    this.expandButtons.forEach(button => {
      button.setAttribute("aria-pressed", String(expanded));
      button.setAttribute("aria-label", expanded ? "Collapse map" : "Expand map");
      const label = $("[data-map-expand-label]", button);
      const glyph = $("[data-map-expand-glyph]", button);
      if (label) label.textContent = expanded ? "Close map" : "Expand map";
      if (glyph) glyph.textContent = expanded ? "×" : "⛶";
    });
    this.scheduleDraw(app.state);
  }

  handleKey(event) {
    const pan = 80;
    const actions = {
      ArrowLeft: () => this.panBy(-pan, 0),
      ArrowRight: () => this.panBy(pan, 0),
      ArrowUp: () => this.panBy(0, -pan),
      ArrowDown: () => this.panBy(0, pan),
      "+": () => this.zoomBy(1),
      "=": () => this.zoomBy(1),
      "-": () => this.zoomBy(-1),
      Home: () => this.resetView(app.state),
      Escape: () => {
        if (this.positioningResponderID) {
          this.positioningResponderID = "";
          this.element.classList.remove("is-positioning-responder");
          this.render(app.state);
        } else if (this.panel.classList.contains("is-expanded")) this.toggleExpanded();
      }
    };
    const action = actions[event.key];
    if (!action) return;
    event.preventDefault();
    action();
  }

  focusOverlay(id, state) {
    const overlay = (state.overlays || []).find(item => item.id === id);
    const coordinates = overlay?.features.flatMap(feature => feature.paths.flat()) || [];
    if (!coordinates.length) return;
    const latitudes = coordinates.map(item => item.latitude);
    const longitudes = coordinates.map(item => item.longitude);
    this.center = [
      (Math.min(...latitudes) + Math.max(...latitudes)) / 2,
      (Math.min(...longitudes) + Math.max(...longitudes)) / 2
    ];
    const width = Math.max(240, this.element.clientWidth * .82);
    const height = Math.max(180, this.element.clientHeight * .82);
    for (let zoom = 18; zoom >= 2; zoom--) {
      const center = project(this.center[0], this.center[1], zoom);
      const worldSize = 256 * 2 ** zoom;
      let maxX = 0;
      let maxY = 0;
      coordinates.forEach(coordinate => {
        const pixel = project(coordinate.latitude, coordinate.longitude, zoom);
        let deltaX = pixel.x - center.x;
        if (deltaX > worldSize / 2) deltaX -= worldSize;
        if (deltaX < -worldSize / 2) deltaX += worldSize;
        maxX = Math.max(maxX, Math.abs(deltaX));
        maxY = Math.max(maxY, Math.abs(pixel.y - center.y));
      });
      if (maxX * 2 <= width && maxY * 2 <= height) {
        this.zoom = zoom;
        break;
      }
    }
    this.scheduleDraw(state);
  }

  scheduleDraw(state) {
    this.pendingState = state;
    if (this.renderFrame != null) return;
    this.renderFrame = requestAnimationFrame(() => {
      this.renderFrame = null;
      const pending = this.pendingState;
      this.pendingState = null;
      this.draw(pending || app.state);
    });
  }

  draw(state) {
    const width = this.element.clientWidth;
    const height = this.element.clientHeight;
    if (!width || !height) return;
    if (!this.hasFitInitialObjects && this.fitObjects(state, false)) {
      this.hasFitInitialObjects = true;
    }
    const worldSize = 256 * 2 ** this.zoom;
    const center = project(this.center[0], this.center[1], this.zoom);
    const minX = center.x - width / 2;
    const minY = center.y - height / 2;
    const startTileX = Math.floor(minX / 256);
    const endTileX = Math.floor((minX + width) / 256);
    const startTileY = Math.floor(minY / 256);
    const endTileY = Math.floor((minY + height) / 256);
    const tileCount = 2 ** this.zoom;

    const visibleTiles = new Set();
    for (let y = startTileY; y <= endTileY; y++) {
      if (y < 0 || y >= tileCount) continue;
      for (let x = startTileX; x <= endTileX; x++) {
        const wrappedX = ((x % tileCount) + tileCount) % tileCount;
        const key = `${this.zoom}/${x}/${y}`;
        visibleTiles.add(key);
        let tile = this.tiles.get(key);
        if (!tile) {
          tile = document.createElement("img");
          tile.className = "map-tile";
          tile.alt = "";
          tile.draggable = false;
          tile.src = `/api/map/tiles/${this.zoom}/${wrappedX}/${y}.png`;
          tile.addEventListener("error", () => {
            this.tiles.delete(key);
            tile.remove();
          });
          this.tiles.set(key, tile);
          this.tileLayer.append(tile);
        }
        tile.style.left = `${x * 256 - minX}px`;
        tile.style.top = `${y * 256 - minY}px`;
      }
    }
    this.tiles.forEach((tile, key) => {
      if (visibleTiles.has(key)) return;
      tile.remove();
      this.tiles.delete(key);
    });

    this.drawTrails(state, center, width, height, worldSize);
    this.drawRadar(state, center, width, height, worldSize);
    this.markerLayer.replaceChildren();
    const visibleMarkers = mapPoints(state).filter(point => this.layers[point.kind] !== false).map(point => {
      const pixel = project(point.latitude, point.longitude, this.zoom);
      let deltaX = pixel.x - center.x;
      if (deltaX > worldSize / 2) deltaX -= worldSize;
      if (deltaX < -worldSize / 2) deltaX += worldSize;
      const left = width / 2 + deltaX;
      const top = height / 2 + (pixel.y - center.y);
      return { point, left, top };
    }).filter(marker => marker.left >= -30 && marker.left <= width + 30 && marker.top >= -30 && marker.top <= height + 30);
    spreadOverlappingMapMarkers(visibleMarkers).forEach(({ point, left, top }) => {
      const marker = document.createElement("button");
      marker.type = "button";
      marker.className = `map-marker ${point.kind}${point.statusClass ? ` ${point.statusClass}` : ""}${point.stale ? " stale" : ""}`;
      marker.style.left = `${left}px`;
      marker.style.top = `${top}px`;
      marker.title = point.label;
      marker.setAttribute("aria-label", point.label);
      if (point.markerColor) marker.style.setProperty("--marker", point.markerColor);
      marker.innerHTML = `<span>${point.mapLabel ? `<b>${html(point.mapLabel)}</b>` : ""}</span><small class="marker-label">${html(point.label)}</small>`;
      marker.addEventListener("click", () => {
        if (marker.dataset.dragged === "true") {
          delete marker.dataset.dragged;
          return;
        }
        if (point.kind === "incident") openIncident(app.state.incidents.find(item => item.id === point.id));
        else if (point.kind === "responder") openResponder(app.state.responders.find(item => item.id === point.id));
        else if (point.kind === "facility") openFacility(app.state.facilities.find(item => item.id === point.id));
        else if (point.kind === "location") openLocation((app.state.locations || []).find(item => item.id === point.id));
        else toast(mapPointKindLabel(point.kind), point.label);
      });
      if (point.kind === "responder") this.makeResponderDraggable(marker, point);
      this.markerLayer.append(marker);
    });
    this.drawUnmappedIncidents(state);
    const current=app.weather?.current;this.weatherBadge.hidden=!current;if(current){this.weatherBadge.innerHTML=`<strong>${current.temperature_f==null?"—":`${Math.round(current.temperature_f)}°F`}</strong>${html(current.description||"Current conditions")}`;}
  }

  drawRadar(state, center, width, height, worldSize) {
    const settings = state.settings.weather || {};
    if (!settings.enabled || !settings.radar_enabled || this.layers.radar === false) {
      clearTimeout(this.radarTimer);
      clearInterval(this.radarPlaybackTimer);
      this.radarPlayButton.hidden = true;
      this.radarLayer.hidden = true;
      this.radarStatus.hidden = true;
      this.radarPendingKey = "";
      this.radarRequest++;
      return;
    }
    this.radarLayer.style.opacity = String((settings.radar_opacity || 55) / 100);
    const limit = 20037508.342789244;
    const meterX = x => Math.max(-limit, Math.min(limit, (x / worldSize * 2 - 1) * limit));
    const meterY = y => Math.max(-limit, Math.min(limit, (1 - y / worldSize * 2) * limit));
    const bbox = [
      meterX(center.x - width / 2), meterY(center.y + height / 2),
      meterX(center.x + width / 2), meterY(center.y - height / 2)
    ].map(value => value.toFixed(2)).join(",");
    const imageWidth = Math.max(128, Math.min(1400, Math.round(width)));
    const imageHeight = Math.max(128, Math.min(1000, Math.round(height)));
    const localRadarURL = String(settings.radar_url || "").trim();
    const frameCount = settings.radar_animation && localRadarURL ? Math.max(2, Math.min(6, settings.radar_frames || 4)) : 1;
    const key = `${bbox}|${imageWidth}|${imageHeight}|${frameCount}`;
    if (key === this.radarKey && Date.now() - this.radarLoadedAt < 5 * 60 * 1000) return;
    if (this.radarPendingKey) return;
    this.radarLayer.hidden = true;
    this.setRadarStatus(null);
    clearTimeout(this.radarTimer);
    this.radarPendingKey = key;
    if (frameCount > 1) this.loadRadarAnimation(key, bbox, imageWidth, imageHeight, frameCount);
    else this.loadRadar(key, bbox, imageWidth, imageHeight);
  }

  async loadRadarAnimation(key, bbox, width, height, frameCount) {
    const requestID = ++this.radarRequest;
    const boundary = Math.floor(Date.now() / 300000) * 300000;
    const frames = [];
    try {
      for (let index = frameCount - 1; index >= 0; index--) {
        const time = boundary - index * 300000;
        const response = await fetch(`/api/weather/radar?${new URLSearchParams({ bbox, width, height, time })}`);
        if (!response.ok) throw new Error(`Radar frame returned HTTP ${response.status}`);
        const blob = await response.blob();
        if (requestID !== this.radarRequest) return this.revokeRadarFrames(frames);
        frames.push({ url: URL.createObjectURL(blob), time: response.headers.get("X-Tickets-Weather-Time"), stale: response.headers.get("X-Tickets-Weather-Stale") === "true" });
      }
      this.revokeRadarFrames(this.radarFrames);
      if (this.radarObjectURL) URL.revokeObjectURL(this.radarObjectURL);
      this.radarObjectURL = "";
      this.radarFrames = frames;
      this.radarFrameIndex = frames.length - 1;
      this.radarKey = key;
      this.radarLoadedAt = Date.now();
      this.radarPaused = false;
      this.radarPlayButton.hidden = false;
      this.showRadarFrame();
      this.startRadarPlayback();
    } catch (error) {
      this.revokeRadarFrames(frames);
      this.radarLayer.classList.add("is-stale");
      this.setRadarStatus(null, false, true);
      console.warn("Weather radar animation failed", error);
    } finally {
      if (requestID === this.radarRequest) this.radarPendingKey = "";
    }
  }

  showRadarFrame() {
    const frame = this.radarFrames[this.radarFrameIndex];
    if (!frame) return;
    this.radarLayer.src = frame.url;
    this.radarLayer.hidden = false;
    this.radarLayer.classList.toggle("is-stale", frame.stale);
    this.radarLayer.title = `Weather radar · ${frame.time || "current"}`;
    this.setRadarStatus(frame.time, frame.stale);
  }

  setRadarStatus(time, stale = false, failed = false) {
    this.radarStatus.hidden = false;
    this.radarStatus.classList.toggle("is-stale", stale);
    this.radarStatus.classList.toggle("is-error", failed);
    if (failed) {
      this.radarStatus.textContent = "Radar unavailable";
      return;
    }
    if (!time) {
      this.radarStatus.textContent = "Radar loading…";
      return;
    }
    const parsed = new Date(time);
    const label = Number.isNaN(parsed.getTime()) ? time : parsed.toLocaleString([], {
      month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit"
    });
    this.radarStatus.textContent = `${stale ? "Cached radar" : "Radar"} as of ${label}`;
  }

  startRadarPlayback() {
    clearInterval(this.radarPlaybackTimer);
    if (this.radarPaused || this.radarFrames.length < 2) return;
    this.radarPlaybackTimer = setInterval(() => {
      this.radarFrameIndex = (this.radarFrameIndex + 1) % this.radarFrames.length;
      this.showRadarFrame();
    }, 900);
    this.radarPlayButton.textContent = "Ⅱ";
    this.radarPlayButton.title = "Pause radar animation";
  }

  toggleRadarPlayback() {
    this.radarPaused = !this.radarPaused;
    if (this.radarPaused) {
      clearInterval(this.radarPlaybackTimer);
      this.radarPlayButton.textContent = "▶";
      this.radarPlayButton.title = "Play radar animation";
    } else {
      this.startRadarPlayback();
    }
  }

  revokeRadarFrames(frames) {
    frames.forEach(frame => URL.revokeObjectURL(frame.url));
  }

  async loadRadar(key, bbox, width, height) {
    const requestID = ++this.radarRequest;
    clearInterval(this.radarPlaybackTimer);
    this.radarPlayButton.hidden = true;
    this.revokeRadarFrames(this.radarFrames);
    this.radarFrames = [];
    try {
      const response = await fetch(`/api/weather/radar?${new URLSearchParams({ bbox, width, height })}`);
      if (!response.ok) throw new Error(`Radar returned HTTP ${response.status}`);
      const blob = await response.blob();
      if (requestID !== this.radarRequest) return;
      const objectURL = URL.createObjectURL(blob);
      const previousURL = this.radarObjectURL;
      this.radarLayer.onload = () => {
        if (previousURL) URL.revokeObjectURL(previousURL);
      };
      this.radarObjectURL = objectURL;
      this.radarLayer.src = objectURL;
      this.radarLayer.hidden = false;
      const radarTime = response.headers.get("X-Tickets-Weather-Time");
      const radarStale = response.headers.get("X-Tickets-Weather-Stale") === "true";
      this.radarLayer.classList.toggle("is-stale", radarStale);
      this.radarLayer.title = `NOAA weather radar · ${radarTime || "current"}`;
      this.setRadarStatus(radarTime, radarStale);
      this.radarKey = key;
      this.radarLoadedAt = Date.now();
    } catch (error) {
      this.radarLayer.classList.add("is-stale");
      this.setRadarStatus(null, false, true);
      console.warn("Weather radar update failed", error);
    } finally {
      if (requestID === this.radarRequest) this.radarPendingKey = "";
    }
  }

  drawUnmappedIncidents(state) {
    const incidents = state.incidents.filter(item =>
      activeStatuses.has(item.status) && (item.latitude == null || item.longitude == null)
    );
    const signature = incidents.map(item => `${item.id}:${item.updated_at}`).join("|");
    if (signature === this.lastUnmappedSignature) return;
    this.lastUnmappedSignature = signature;
    this.unmappedLayer.hidden = incidents.length === 0;
    if (!incidents.length) {
      this.unmappedLayer.replaceChildren();
      return;
    }
    this.unmappedLayer.innerHTML = `
      <strong>${incidents.length} active incident${incidents.length === 1 ? "" : "s"} need map locations</strong>
      <p>Open an incident to look up its address or enter coordinates.</p>
      ${incidents.slice(0, 4).map(item => `<button type="button" data-unmapped-incident="${attr(item.id)}">#${item.number} · ${html(item.title)}</button>`).join("")}
    `;
    $$("[data-unmapped-incident]", this.unmappedLayer).forEach(button => button.addEventListener("click", () => {
      openIncident(app.state.incidents.find(item => item.id === button.dataset.unmappedIncident));
    }));
  }

  drawTrails(state, center, width, height, worldSize) {
    this.trailLayer.replaceChildren();
    this.trailLayer.setAttribute("viewBox", `0 0 ${width} ${height}`);
    this.drawOverlays(state, center, width, height, worldSize);
    const groups = new Map();
    (this.layers.aprs === false ? [] : (state.tracks || [])).forEach(point => {
      if (!groups.has(point.responder_id)) groups.set(point.responder_id, []);
      groups.get(point.responder_id).push(point);
    });
    groups.forEach(points => {
      const projected = points
        .sort((a, b) => new Date(a.received_at) - new Date(b.received_at))
        .map(point => {
          const pixel = project(point.latitude, point.longitude, this.zoom);
          let deltaX = pixel.x - center.x;
          if (deltaX > worldSize / 2) deltaX -= worldSize;
          if (deltaX < -worldSize / 2) deltaX += worldSize;
          return `${width / 2 + deltaX},${height / 2 + pixel.y - center.y}`;
        });
      if (projected.length < 2) return;
      const trail = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
      trail.setAttribute("points", projected.join(" "));
      trail.setAttribute("class", "aprs-trail");
      this.trailLayer.append(trail);
    });
  }

  drawOverlays(state, center, width, height, worldSize) {
    const toScreen = coordinate => {
      const pixel = project(coordinate.latitude, coordinate.longitude, this.zoom);
      let deltaX = pixel.x - center.x;
      if (deltaX > worldSize / 2) deltaX -= worldSize;
      if (deltaX < -worldSize / 2) deltaX += worldSize;
      return { x: width / 2 + deltaX, y: height / 2 + pixel.y - center.y };
    };
    (this.layers.alerts === false ? [] : (state.weather_alerts || [])).forEach(alert => {
      const pathData = (alert.paths || []).map(path => path.map((coordinate, index) => {
        const point = toScreen(coordinate);
        return `${index ? "L" : "M"} ${point.x} ${point.y}`;
      }).join(" ") + " Z").join(" ");
      if (!pathData.trim()) return;
      const polygon = document.createElementNS("http://www.w3.org/2000/svg", "path");
      polygon.setAttribute("d", pathData);
      polygon.setAttribute("class", `weather-alert-polygon ${alert.severity || "unknown"}`);
      const alertLabel = [alert.event || alert.headline || "Weather warning", alert.area, alert.severity ? `${label(alert.severity)} severity` : ""].filter(Boolean).join(" · ");
      polygon.dataset.weatherAlertLabel = alertLabel;
      polygon.setAttribute("tabindex", "0");
      polygon.setAttribute("role", "button");
      polygon.setAttribute("aria-label", alertLabel);
      const title = document.createElementNS("http://www.w3.org/2000/svg", "title");
      title.textContent = `${alertLabel} — click for details`;
      polygon.append(title);
      this.trailLayer.append(polygon);
    });
    (state.overlays || []).filter(overlay => overlay.visible).forEach(overlay => {
      const makeDrawingInteractive = shape => {
        if (overlay.file_name !== "Map drawing") return;
        shape.classList.add("map-drawn-overlay");
        shape.dataset.drawnOverlayId = overlay.id;
        shape.setAttribute("tabindex", "0");
        shape.setAttribute("role", "button");
        shape.setAttribute("aria-label", `Delete map drawing ${overlay.name}`);
        const title = document.createElementNS("http://www.w3.org/2000/svg", "title");
        title.textContent = `${overlay.name} — click to delete`;
        shape.append(title);
      };
      overlay.features.forEach(feature => {
        const featureColor = overlay.use_kml_styles && feature.color ? feature.color : overlay.color;
        const overlayOpacity = Math.max(0.1, Math.min(1, (Number(overlay.opacity) || 100) / 100));
        const featureDetails = [feature.name || overlay.name, feature.description].filter(Boolean).join(" — ");
        const makeKMLInteractive = shape => {
          if (overlay.file_name === "Map drawing") return;
          shape.classList.add("map-kml-feature");
          shape.setAttribute("tabindex", "0");
          shape.setAttribute("role", "img");
          shape.setAttribute("aria-label", featureDetails);
          const title = document.createElementNS("http://www.w3.org/2000/svg", "title");
          title.textContent = featureDetails;
          shape.append(title);
        };
        if (feature.geometry_type === "point") {
          feature.paths.flat().forEach(coordinate => {
            const point = toScreen(coordinate);
            const marker = document.createElementNS("http://www.w3.org/2000/svg", "circle");
            marker.setAttribute("cx", point.x);
            marker.setAttribute("cy", point.y);
            marker.setAttribute("r", "5");
            marker.setAttribute("fill", featureColor);
            marker.setAttribute("fill-opacity", overlayOpacity);
            marker.setAttribute("class", "overlay-point");
            makeDrawingInteractive(marker);
            makeKMLInteractive(marker);
            this.trailLayer.append(marker);
          });
          return;
        }
        if (feature.geometry_type === "line") {
          feature.paths.forEach(path => {
            if (path.length < 2) return;
            const line = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
            line.setAttribute("points", path.map(coordinate => {
              const point = toScreen(coordinate);
              return `${point.x},${point.y}`;
            }).join(" "));
            line.setAttribute("stroke", featureColor);
            line.setAttribute("stroke-opacity", overlayOpacity);
            line.setAttribute("class", "overlay-line");
            makeDrawingInteractive(line);
            makeKMLInteractive(line);
            this.trailLayer.append(line);
          });
          return;
        }
        if (feature.geometry_type === "polygon") {
          const pathData = feature.paths.map(path => path.map((coordinate, index) => {
            const point = toScreen(coordinate);
            return `${index ? "L" : "M"} ${point.x} ${point.y}`;
          }).join(" ") + " Z").join(" ");
          if (!pathData.trim()) return;
          const polygon = document.createElementNS("http://www.w3.org/2000/svg", "path");
          polygon.setAttribute("d", pathData);
          polygon.setAttribute("stroke", featureColor);
          polygon.setAttribute("fill", featureColor);
          polygon.setAttribute("stroke-opacity", overlayOpacity);
          polygon.setAttribute("fill-opacity", overlayOpacity * 0.22);
          polygon.setAttribute("class", "overlay-polygon");
          makeDrawingInteractive(polygon);
          makeKMLInteractive(polygon);
          this.trailLayer.append(polygon);
        }
      });
    });
    this.drawDrawingPreview(center, width, height, worldSize);
    this.drawRoutePreview(center, width, height, worldSize);
  }

  drawDrawingPreview(center, width, height, worldSize) {
    if (!this.drawing?.points.length) return;
    const screenPoints = this.drawing.points.map(coordinate => {
      const pixel = project(coordinate.latitude, coordinate.longitude, this.zoom);
      let deltaX = pixel.x - center.x;
      if (deltaX > worldSize / 2) deltaX -= worldSize;
      if (deltaX < -worldSize / 2) deltaX += worldSize;
      return `${width / 2 + deltaX},${height / 2 + pixel.y - center.y}`;
    });
    const preview = document.createElementNS("http://www.w3.org/2000/svg", this.drawing.type === "point" ? "circle" : this.drawing.type === "polygon" ? "polygon" : "polyline");
    preview.setAttribute("class", `drawing-preview ${this.drawing.type}`);
    preview.setAttribute("stroke", this.drawing.color);
    if (this.drawing.type === "point") {
      const [x, y] = screenPoints[0].split(",");
      preview.setAttribute("cx", x);
      preview.setAttribute("cy", y);
      preview.setAttribute("r", "8");
      preview.setAttribute("fill", this.drawing.color);
    } else {
      preview.setAttribute("points", screenPoints.join(" "));
      if (this.drawing.type === "polygon") preview.setAttribute("fill", this.drawing.color);
    }
    this.trailLayer.append(preview);
  }

  drawRoutePreview(center, width, height, worldSize) {
    if (!this.route) return;
    const coordinates = this.route.path.length ? this.route.path : this.route.points;
    const points = coordinates.map(coordinate => {
      const pixel = project(coordinate.latitude, coordinate.longitude, this.zoom);
      let deltaX = pixel.x - center.x;
      if (deltaX > worldSize / 2) deltaX -= worldSize;
      if (deltaX < -worldSize / 2) deltaX += worldSize;
      return `${width / 2 + deltaX},${height / 2 + pixel.y - center.y}`;
    });
    if (points.length > 1) {
      const route = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
      route.setAttribute("points", points.join(" "));
      route.setAttribute("class", "route-preview");
      this.trailLayer.append(route);
    }
    this.route.points.forEach(coordinate => {
      const pixel = project(coordinate.latitude, coordinate.longitude, this.zoom);
      let deltaX = pixel.x - center.x;
      if (deltaX > worldSize / 2) deltaX -= worldSize;
      if (deltaX < -worldSize / 2) deltaX += worldSize;
      const marker = document.createElementNS("http://www.w3.org/2000/svg", "circle");
      marker.setAttribute("cx", width / 2 + deltaX);
      marker.setAttribute("cy", height / 2 + pixel.y - center.y);
      marker.setAttribute("r", "7");
      marker.setAttribute("fill", "#38bdf8");
      marker.setAttribute("stroke", "white");
      this.trailLayer.append(marker);
    });
  }
}

function mapPoints(state) {
  const points = [];
  state.incidents.filter(item => activeStatuses.has(item.status) && item.latitude != null && item.longitude != null)
    .forEach(item => {
      const stale = isIncidentStale(item);
      points.push({
        id: item.id,
        kind: "incident",
        label: `#${item.number} ${item.title}${stale ? ` · needs update (${relativeTime(item.updated_at)})` : ""}`,
        latitude: item.latitude,
        longitude: item.longitude,
        stale
      });
    });
  state.responders.filter(item => item.latitude != null && item.longitude != null)
    .forEach(item => {
      const staleMinutes = state.settings.aprs?.stale_minutes || 15;
      const stale = Boolean(item.aprs_enabled && item.position_updated_at && Date.now() - new Date(item.position_updated_at).getTime() > staleMinutes * 60000);
      const aprsLabel = responderAPRSLabel(item);
      points.push({
        id: item.id,
        kind: "responder",
        label: `${displayResponder(item)}${aprsLabel ? ` · ${aprsLabel}` : ""}`,
        latitude: item.latitude,
        longitude: item.longitude,
        mapLabel: mapMarkerLabel(item, displayResponder(item)),
        markerColor: item.marker_color,
        stale
      });
    });
  state.facilities.filter(item => item.latitude != null && item.longitude != null)
    .forEach(item => points.push({ id: item.id, kind: "facility", label: item.name, latitude: item.latitude, longitude: item.longitude, mapLabel: mapMarkerLabel(item, item.name), markerColor: item.marker_color }));
  (state.locations || []).filter(item => item.latitude != null && item.longitude != null)
    .forEach(item => points.push({ id: item.id, kind: "location", label: item.name, latitude: item.latitude, longitude: item.longitude }));
  if (state.settings.aprs?.area_enabled ||
      ((state.settings.aprs?.mode === "local" || state.settings.aprs?.mode === "hybrid") && state.settings.aprs?.local?.show_all)) {
    const trackedCallsigns = new Set(state.responders.filter(item => item.aprs_enabled).map(item => String(item.callsign || "").toUpperCase()));
    (state.aprs_stations || [])
      .filter(item => item.latitude != null && item.longitude != null && !trackedCallsigns.has(String(item.callsign || "").toUpperCase()))
      .forEach(item => points.push({
        id: item.callsign,
        kind: "aprs",
        label: `${item.callsign} · ${item.source === "local_rf" ? "local RF" : "APRS-IS"}${aprsWeatherLabel(item.weather)} · heard ${relativeTime(item.last_heard_at)}`,
        latitude: item.latitude,
        longitude: item.longitude
      }));
  }
  (app.water?.gauges || []).filter(item => item.latitude != null && item.longitude != null).forEach(item => points.push({id:item.site_id,kind:"water",statusClass:`flood-${waterGaugeFloodLevel(item)}`,label:`${item.name || item.site_id}${item.stage_feet == null ? "" : ` · ${item.stage_feet.toFixed(2)} ft ${item.stage_trend || ""}`}${item.flood_category ? ` · ${label(item.flood_category)}` : ""}${item.forecast_stage_feet == null ? "" : ` · crest ${item.forecast_stage_feet.toFixed(2)} ft`}`,latitude:item.latitude,longitude:item.longitude}));
  for(const item of app.integrations?.storm_reports||[])points.push({id:item.id,kind:"storm",label:`${item.name||item.kind||"Storm report"} · ${item.source||"configured source"}${item.observed_at?` · ${relativeTime(item.observed_at)}`:""}`,latitude:item.latitude,longitude:item.longitude});
  for(const item of app.integrations?.infrastructure||[])points.push({id:item.id,kind:"infrastructure",label:`${item.name||item.kind||"Infrastructure"} · ${item.source||"configured source"}${item.observed_at?` · ${relativeTime(item.observed_at)}`:""}`,latitude:item.latitude,longitude:item.longitude});
  for(const item of app.integrations?.amateur_repeaters||[])points.push({id:item.id,kind:"amateur_repeater",label:`${item.name||"Amateur repeater"}${item.details?` · ${item.details}`:""}${item.source?` · ${item.source}`:""}`,latitude:item.latitude,longitude:item.longitude});
  for(const item of app.integrations?.gmrs_repeaters||[])points.push({id:item.id,kind:"gmrs_repeater",label:`${item.name||"GMRS repeater"}${item.details?` · ${item.details}`:""}${item.source?` · ${item.source}`:""}`,latitude:item.latitude,longitude:item.longitude});
  for(const item of app.integrations?.meshcore_nodes||[])points.push({id:item.id,kind:"meshcore",label:`${item.name||"MeshCore node"}${item.kind?` · ${label(item.kind)}`:""}${item.details?` · ${item.details}`:""}${item.observed_at?` · ${relativeTime(item.observed_at)}`:""}`,latitude:item.latitude,longitude:item.longitude});
  for(const item of app.integrations?.sensors||[])if(item.latitude!=null&&item.longitude!=null)points.push({id:item.id,kind:"sensor",label:`${item.name||item.id}${item.temperature_f==null?"":` · ${Math.round(item.temperature_f)}°F`}${item.status?` · ${item.status}`:""} · ${relativeTime(item.observed_at)}`,latitude:item.latitude,longitude:item.longitude});
  return points;
}

function spreadOverlappingMapMarkers(markers) {
  const groups = new Map();
  markers.forEach(marker => {
    const key = `${Math.round(marker.left / 10)}:${Math.round(marker.top / 10)}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(marker);
  });
  groups.forEach(group => {
    if (group.length < 2) return;
    const radius = Math.min(24, 9 + group.length * 2);
    group.sort((left, right) => `${left.point.kind}:${left.point.id}`.localeCompare(`${right.point.kind}:${right.point.id}`));
    group.forEach((marker, index) => {
      const angle = -Math.PI / 2 + (2 * Math.PI * index) / group.length;
      marker.left += Math.cos(angle) * radius;
      marker.top += Math.sin(angle) * radius;
    });
  });
  return markers;
}

function mapMarkerLabel(item, fallback) {
  const configured = String(item.map_label || "").toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 3);
  if (configured.length >= 2) return configured;
  const automatic = String(fallback || "").toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 3);
  return automatic.length >= 2 ? automatic : "";
}

function mapPointKindLabel(kind) {
  return ({
    aprs: "APRS station",
    water: "Water gauge",
    storm: "Storm report",
    infrastructure: "Infrastructure",
    amateur_repeater: "Amateur repeater",
    gmrs_repeater: "GMRS repeater",
    meshcore: "MeshCore node",
    sensor: "Sensor"
  })[kind] || "Map item";
}

function aprsWeatherLabel(weather) {
  if (!weather) return "";
  const details = [];
  if (weather.temperature_f != null) details.push(`${weather.temperature_f}°F`);
  if (weather.wind_speed_mph != null) details.push(`wind ${weather.wind_speed_mph} mph${weather.wind_gust_mph != null ? ` gust ${weather.wind_gust_mph}` : ""}`);
  if (weather.humidity_percent != null) details.push(`${weather.humidity_percent}% RH`);
  if (weather.rain_24_hours_inches != null) details.push(`${weather.rain_24_hours_inches.toFixed(2)} in/24h`);
  return details.length ? ` · WX ${details.join(" · ")}` : " · WX observation";
}

function project(latitude, longitude, zoom) {
  const sin = Math.sin(latitude * Math.PI / 180);
  const size = 256 * 2 ** zoom;
  return {
    x: (longitude + 180) / 360 * size,
    y: (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) * size
  };
}

function unproject(x, y, zoom) {
  const size = 256 * 2 ** zoom;
  const normalizedX = ((x % size) + size) % size;
  const clampedY = Math.max(0, Math.min(size, y));
  const longitude = normalizedX / size * 360 - 180;
  const mercator = Math.PI - 2 * Math.PI * clampedY / size;
  const latitude = 180 / Math.PI * Math.atan(Math.sinh(mercator));
  return [Math.max(-85.05112878, Math.min(85.05112878, latitude)), longitude];
}
