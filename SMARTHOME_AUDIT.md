# Smart-home & automation audit — 2026-06-22

_Read-only multi-agent audit (6 domains: devices/WiZ, cloud bridge, scenes, automation engine, frontend control UX, security), each finding adversarially verified. 36 confirmed findings._

> **Implementation status 2026-07-17:** dit document bewaart de oorspronkelijke probleembeschrijving. De bridge-route valideert nu uitsluitend een afzonderlijke `BRIDGE_API_KEY`; queue/bridge-modus vereist minimaal 32 tekens en configuratie weigert hergebruik van `APP_SECRET_KEY`. Command-/scene-input en bridgestatus zijn begrensd/gevalideerd en optimistic-state writes lopen synchroon met de requestcontext. Gebruik `BACKEND_ARCHITECTURE.md` als actuele runtimekaart.

**Totaal:** 🔴 0 · 🟠 6 hoog · 🟡 13 medium · ⚪ 17 laag

## Executive verdict
Solid bones, one self-inflicted landmine. The control plane is well-structured (constant-time secret compares, owner-gated writes, a clean cloud-queue/local-bridge split), but the system has a cluster of high-severity issues concentrated at two seams: the bridge auth boundary and the optimistic-state write path. Critically, the codebase's OWN hardening documentation steers the operator into breaking it. Nothing here is an external takeover, so this is not a fire drill — but as a single-operator home setup, the failure modes are silent (lights stop responding, the UI lies about physical state) and several are triggered by following the repo's own advice or by a routine DB restore. Fix the bridge auth wiring and the optimistic-state reconciliation, and the system moves from fragile-but-working to genuinely robust.

**Biggest risk:** The bridge trust boundary is a documented footgun that converts recommended hardening into a total outage. /bridge/* is gated by APP_SECRET_KEY (routes.go:37,74-78) while the bridge client sends BRIDGE_API_KEY (cloud_bridge.go:229), which is never validated server-side. It only works today because BRIDGE_API_KEY defaults to APP_SECRET_KEY. The instant the operator follows render.yaml/NEXT_STEPS.md and sets a distinct BRIDGE_API_KEY, every claim/complete/status call 403s, the queue stops draining (the in-process poller is off by default in queue mode), and all lights go dark with no error surfaced. Worse, config.Validate actively WARNS you to set a separate key — so the system documents you into the outage. This is the rare bug that is both a real trust-boundary gap (a leaked app secret grants full bridge control) and a latent availability bomb.

## Top fixes
1. 🟠 **Bind /bridge/* to its own BRIDGE_API_KEY middleware instead of reusing AppSecretKey** — `backend/internal/server/routes.go:37,74-78`
   - Replace authMw on the /bridge group with apiKeyMiddleware(cfg.BridgeAPIKey) (which already falls back to AppSecretKey when unset via config.go:129). This single change fixes BOTH the auth-separation gap and the distinct-key outage: setting a separate BRIDGE_API_KEY becomes correct rather than a 403 storm. Highest-leverage fix in the audit — it neutralizes the biggest risk.
2. 🟠 **Invert the config.Validate guidance so it never accepts a config where the bridge cannot authenticate** — `backend/internal/config/config.go:199-201`
   - Today Validate only warns when the keys are EQUAL — i.e. it nags you toward the broken state and stays silent on the broken state. After the routes fix, flip it: warn (or fail closed in production) when BRIDGE_API_KEY is unset/defaulted, and never steer the operator into a 403. Pair with a doc fix in render.yaml:51-56, NEXT_STEPS.md:31, README.md:59 so the three docs stop contradicting each other.
3. 🟠 **Reconcile optimistic device state on terminal command failure instead of leaving the UI lying for ~5 min** — `backend/internal/handler/device.go:389-407`
   - Queue mode writes the INTENDED state then returns 204 before any lamp is touched; RequeueOrFail (handler/bridge.go:146 -> device_command.go:111-132) sets status='failed' but never restores current_state. So an offline bulb renders as 'on/at new color' for up to 5 minutes, or indefinitely if BRIDGE_STATUS_POLL_ENABLED is false. On terminal-failed, restore last-confirmed current_state (or set a warning flag) and trigger a targeted status refresh for that device. This is the UI affirmatively misrepresenting physical state — unacceptable on a device-control surface.
4. 🟠 **Add onError handling to lamp command mutations so failures surface to the user** — `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/hooks/useDevices.ts:23-38`
   - useLampCommand defines only onSuccess; there is no per-call onError and providers.tsx has no global MutationCache onError, so a 502 (direct mode) or queue-write failure produces a swallowed promise rejection and zero feedback. Every caller (LampCard, LampDetailPanel, RoomSection.toggleAll, useGlobalShortcuts, SceneBar) uses bare mutate(). Add onError that toasts and invalidates devices to reconverge. This is the central UX defect for the whole control domain and pairs with the device.go reconciliation fix above.
5. 🟠 **Make the base/runtime automations schema match the live AutomationStore so DR rebuilds don't kill automations** — `backend/internal/store/runtime_schema_base.go:56-66`
   - ensureBaseTables creates automations with legacy columns (is_enabled, last_triggered, description, condition_config) while AutomationStore queries user_id/enabled/last_fired_at/group_name (automation.go:17,51,95). On any fresh/restored/DR-rebuilt DB the first List/Create/Toggle errors with 'column does not exist' and the entire automation feature is dead. Invisible in prod because the real DB was built by the now-dead migrations/. Fix the base schema (or add an idempotent ensureAutomationSchema with ADD COLUMN IF NOT EXISTS) and extend the fresh-DB test to actually run an automation query.
6. 🟡 **Validate device ip_address with net.ParseIP on register/update before it is dialed over UDP** — `backend/internal/wiz/client.go:52`
   - SendCommand dials whatever string is stored (VARCHAR(45), only non-empty-checked). A stored broadcast (255.255.255.255) turns every setPilot into a LAN-wide blast; a hostname triggers DNS per command/poll. Writes are owner-only (Next proxy 403s non-owners, route.ts:79-81), so this is owner garbage-in robustness, not external SSRF — hence medium. Reject loopback/multicast/broadcast/unspecified/non-RFC1918 and any host:port form at the register/update boundary.
7. 🟡 **Clamp optimistic statePatch to the same ranges the lamp enforces** — `backend/internal/handler/device.go:399,355-358`
   - brightness is stored as the raw *cmd.Brightness with no clamp, while client.SetState clamps to 10-100 (client.go:150). brightness<10 displays e.g. 5 in the UI while the bulb sits at 10 — a persistent off-by-display discrepancy independent of the failure-reconciliation issue. Clamp brightness (10-100) and temp (2200-6500) in the optimistic write to match physical behavior.

## Themes
- Optimistic-write-without-reconciliation: the system writes INTENDED state and reports success before (or regardless of whether) the physical device acts, and there is no rollback on failure — spanning the backend (device.go), the failure path (device_command.go RequeueOrFail never touches current_state), and the frontend (useDevices.ts has no onError). The UI can affirmatively lie about physical reality for ~5 minutes or indefinitely.
- Documentation that breaks the system: render.yaml, NEXT_STEPS.md, and config.Validate all push the operator toward a distinct BRIDGE_API_KEY that 403s the entire bridge, while README.md quietly documents the only value that works. The hardening advice and the code disagree, and following the docs causes an outage. Docs are treated as a trust boundary that the code does not actually enforce.
- Silent failure on a physical-control surface: across domains (bridge 403s, swallowed mutation rejections, dead automations after DR) the common pattern is that things fail with no operator-visible signal. For a solo-operator home where the person IS the monitoring, lack of feedback is the dominant risk class, not external attackers.
- Schema drift between a deprecated model and the live runtime path: the base/DR schema mirrors the Convex-era deprecated model.Automation while the live code uses model.AutomationRow, and nothing reconciles them. The same 'two models, no migration' hazard that makes the migrations/ directory dead also lurks in the DR/fresh-DB path, hidden because production was built by tooling that no longer runs.
- Single-secret control plane: the entire bridge surface (claim/complete commands, overwrite any device status and current_state) rests on the one APP_SECRET_KEY, and the new Next owner-gate only fronts browser /api/backend/* traffic while the engine hits Render directly — so the bridge path has no defense-in-depth beyond that single shared secret.

## Notably solid
- Authentication primitives are done correctly where they exist: subtle.ConstantTimeCompare on the X-API-Key check (server.go:172-186) avoids timing leaks — the problem is which key is wired in, not how the comparison is done.
- Defense-in-depth on the write path for browser traffic: non-owner writes are 403'd at the Next proxy (route.ts:79-81) AND require APP_SECRET_KEY at the backend, which correctly downgraded the ip_address issue from an external SSRF vector to owner-only garbage-in.
- Clean architectural separation of cloud queue vs. local LAN bridge: the device_commands queue + claim/complete/status protocol is a sound design for keeping WiZ UDP control on the LAN while the API lives in the cloud — the issues are wiring/reconciliation bugs on top of a good shape, not a flawed model.
- The lamp client itself clamps inputs defensively (brightness 10-100, temp 2200-6500 in client.go:150), so the physical device is protected from out-of-range values even though the optimistic DB copy is not — the durable failure is cosmetic/state-display, not a device-safety issue.
- Status reconciliation exists by default: BRIDGE_STATUS_POLL_ENABLED defaults true (config.go:130), so in the common configuration the optimistic-state lies are self-healing within ~5 minutes rather than permanent — a real safety net, just too slow and env-overridable to rely on alone.

## All confirmed findings

### 🟠 HIGH (6)

**1. Bridge auth uses APP_SECRET_KEY only; a separate BRIDGE_API_KEY 403s every bridge call and queued commands stick pending forever**  
- _Devices, command queue & WiZ UDP control_ · `reliability` · `backend/internal/server/routes.go:74`
- **Impact:** In queue mode the local bridge is the sole consumer of device_commands. The /bridge/* routes are gated by apiKeyMiddleware(cfg.AppSecretKey) (routes.go:37, server.go:172-186 constant-time compare against AppSecretKey), but the bridge client sends X-API-Key: cfg.BridgeAPIKey (cloud_bridge.go:229). BridgeAPIKey is consumed ONLY client-side; it is never validated server-side. config.go:129 defaults BRIDGE_API_KEY to APP_SECRET_KEY, so an unset key works by accident — but config.go:199-200 actively warns the operator to 'give the bridge its own secret', and the moment they do, every claim/complete/status POST returns 403. With the in-process poller disabled by default in queue mode (config.go:126), nothing else drains the queue, so commands stay 'pending' and lights stop responding.
- **Fix:** Add a bridge-specific middleware that validates cfg.BridgeAPIKey for /bridge/* (with APP_SECRET_KEY as fallback), instead of reusing apiKeyMiddleware(cfg.AppSecretKey). Otherwise remove the misleading 'use a separate key' warning since a separate key currently breaks the bridge.

**2. /bridge/* is authenticated with APP_SECRET_KEY, not BRIDGE_API_KEY — the documented trust-boundary separation does not exist and setting a distinct BRIDGE_API_KEY breaks the bridge**  
- _Cloud bridge (local engine ↔ cloud API)_ · `security` · `backend/internal/server/routes.go:37`
- **Impact:** Two compounding, confirmed problems. (1) The trust-boundary separation promised by render.yaml:51-54, config.go:199-200 and NEXT_STEPS.md:31 is illusory: /bridge/* only ever validates AppSecretKey, so a leaked APP_SECRET_KEY grants full bridge access (claim/complete commands, overwrite device status/state). (2) Operationally worse: if the owner follows the documented hardening (render.yaml/NEXT_STEPS) and sets a DISTINCT BRIDGE_API_KEY in Render, the bridge sends that value (cloud_bridge.go:229) while the server still compares against AppSecretKey (routes.go:37 + server.go:178), returning 403 on every claim/complete/status call — silently turning recommended hardening into a full bridge outage. README.md:59 ('zelfde waarde als APP_SECRET_KEY') is what keeps it working today, masking the trap.
- **Fix:** Mount a dedicated middleware on the /bridge route group that constant-time-compares X-API-Key against cfg.BridgeAPIKey (which already falls back to AppSecretKey when unset via config.go:129), e.g. bridgeMw := apiKeyMiddleware(cfg.BridgeAPIKey) in place of authMw at routes.go:74-78. Then invert the Validate() guidance so it warns when the keys are EQUAL but never silently accepts a config where the bridge cannot authenticate. Optionally accept BOTH keys on /bridge/* during a migration window so rotating BRIDGE_API_KEY can't strand the bridge.

**3. Fresh/restored DB creates the automations table with the wrong columns — every automation read/write errors after a DR rebuild**  
- _Automation engine (trigger / condition / action)_ · `reliability` · `backend/internal/store/runtime_schema_base.go:56-66`
- **Impact:** On a fresh/restored/DR-rebuilt or local-dev DB, ensureBaseTables creates automations with the legacy column set (id, name, description, is_enabled, trigger_config, condition_config, action_config, last_triggered, created_at). The live AutomationStore instead queries/INSERTs id, user_id, name, enabled, created_at, last_fired_at, group_name, trigger_config, action_config. With no reconciliation, the first tick's autoStore.List (and Create/Update/Toggle/MarkFired) fails with 'column "user_id"/"enabled"/... does not exist', so the entire automation feature is dead after any DR event. Invisible in production because the real DB was built by the now-dead migrations/.
- **Fix:** Make the base schema match the store (define automations with user_id/enabled/last_fired_at/group_name and drop/rename the unused is_enabled/last_triggered/description/condition_config), or add an idempotent ensureAutomationSchema step with ALTER TABLE ... ADD COLUMN IF NOT EXISTS repairs. Extend TestEnsureRuntimeSchema_FreshDB to actually call autoStore.List/Create against the empty DB so this drift is caught.

**4. Lamp commands have no error handling, so a failed or timed-out command is silently swallowed**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `reliability` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/hooks/useDevices.ts:23-38`
- **Impact:** When a lamp is offline/unreachable or the queue write fails, the user gets zero feedback. In direct mode the backend returns 502 and apiFetch throws, but the rejected promise is unhandled; in queue mode the optimistic write can even flip the card to the 'successful' state. The user cannot distinguish success from failure on a physical-device control surface.
- **Fix:** Add onError to useLampCommand (or per-call onError) that surfaces an error toast and invalidates devices so the UI reconverges on real state. For toggle/scene buttons prefer explicit optimistic update plus rollback in onError.

**5. Optimistic DB state in queue mode makes the UI report success before, and even when, the lamp never executes the command**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `correctness` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesBackend/backend/internal/handler/device.go:389-407`
- **Impact:** The frontend invalidateQueries refetch (useDevices.ts:31) reads back the optimistic intended state, so cards render the lamp as on / at new brightness / new color even if the bulb was offline or the UDP command failed. The discrepancy persists up to ~5 minutes (status poll), or indefinitely if BRIDGE_STATUS_POLL_ENABLED is set false.
- **Fix:** When RequeueOrFail makes a command terminal-failed, restore the device's last-confirmed current_state or set a status flag the UI renders as a warning. At minimum trigger a targeted status refresh for the affected device shortly after a queued command instead of waiting for the 5-minute poll.

**6. /bridge/* is gated by AppSecretKey, not BridgeAPIKey — the documented bridge trust boundary does not exist, and a distinct BRIDGE_API_KEY breaks the bridge**  
- _Cross-cutting security, authz & input validation_ · `security` · `backend/internal/server/routes.go:37`
- **Impact:** Two coupled problems. (1) No separate bridge trust boundary: /bridge/* (claim commands, mark done/failed, overwrite any device status + current_state) is validated against the same cfg.AppSecretKey as every other route, and the engine reaches Render directly so the new Next owner-gate (which only fronts /api/backend/* browser traffic via proxyBackend) gives the bridge path zero extra protection. The whole bridge surface rests on the single APP_SECRET_KEY. (2) Availability foot-gun: the server validates /bridge/* against AppSecretKey while the engine sends cfg.BridgeAPIKey; the moment the owner follows the repo's own hardening advice and sets a distinct BRIDGE_API_KEY, every bridge request 403s and the LAN bridge stops working. config.Validate only warns when the two are equal — it never enforces correct wiring.
- **Fix:** Build a dedicated bridge middleware bound to cfg.BridgeAPIKey (apiKeyMiddleware(cfg.BridgeAPIKey)) and mount the /bridge/* group with it instead of authMw, keeping the constant-time compare. Then setting BRIDGE_API_KEY distinct from APP_SECRET_KEY becomes correct rather than an outage, and a leaked browser/app secret no longer grants bridge control. Consider failing closed in config.Validate when BRIDGE_API_KEY is unset in production rather than silently defaulting to APP_SECRET_KEY.

### 🟡 MEDIUM (13)

**1. Device ip_address is dialed verbatim over UDP with no net.ParseIP validation (hostname/broadcast/loopback accepted)**  
- _Devices, command queue & WiZ UDP control_ · `reliability` · `backend/internal/wiz/client.go:52`
- **Impact:** SendCommand does net.JoinHostPort(ip,38899)+net.DialTimeout('udp',...) on whatever string is stored. ip_address is VARCHAR(45) in the live runtime schema (runtime_schema_base.go:24) and is only non-empty-checked on register/update (device.go:234,137-149) and in the bridge loop (cloud_bridge.go:127). No net.ParseIP / loopback / multicast / broadcast guard exists anywhere in the wiz package. A stored 255.255.255.255 (or a subnet broadcast) turns every setPilot into a LAN-wide blast; a hostname triggers DNS each command/poll. However writes are owner-only (Next proxy gates non-owner with 403, route.ts:79-81; backend also needs APP_SECRET_KEY), so this is garbage-in by the owner, not an external SSRF/takeover vector.
- **Fix:** Validate ip_address with net.ParseIP on register/update; reject loopback, multicast, broadcast, unspecified, and non-RFC1918 addresses, and reject any host:port form.

**2. Optimistic device state is written raw before the bridge acts and only reconciles every ~5 min**  
- _Devices, command queue & WiZ UDP control_ · `correctness` · `backend/internal/handler/device.go:399`
- **Impact:** In queue mode Command() writes statePatch to the DB then returns 204 before any lamp is touched (device.go:389-407). brightness is stored as the raw *cmd.Brightness (device.go:355-358) with no clamp, while the lamp clamps to 10-100 (client.go:150) — so brightness<10 shows e.g. 5 in the UI but the bulb is at 10. If the bridge later fails the command (lamp offline / bad IP), the optimistic state is never rolled back. Reconciliation only happens via the cloud bridge status poll, which runs every StatusPollEvery*EngineInterval = 10*30s = 300s (cloud_bridge.go:21, engine.go:26-28). So a failed or clamped command displays as 'applied' for up to ~5 minutes.
- **Fix:** Clamp statePatch the same way client.SetState does (brightness 10-100, temp 2200-6500), and on a terminal 'failed' completion (bridge.go:145) reconcile the device's current_state from the last known-good poll instead of leaving the optimistic value.

**3. A lost bridge completion POST causes at-least-once re-execution of a WiZ command**  
- _Devices, command queue & WiZ UDP control_ · `concurrency` · `backend/internal/store/device_command.go:48`
- **Impact:** ClaimPending re-pends any 'processing' row whose claimed_at is older than staleDeviceCommandAfter=2m (device_command.go:20,48-58). The bridge sends the WiZ UDP packet (cloud_bridge.go:131) and only afterwards POSTs /complete (cloud_bridge.go:144). If that completion POST is lost (e.g. cloud blip after the UDP send already landed), the row stays 'processing', is re-pended after 2 min, and the lamp command runs a second time. setPilot with absolute state is largely idempotent so most replays are harmless, but a toggle-style command or a manual change made in the gap gets reverted.
- **Fix:** Track dispatch (UDP-sent) separately from claim, or attach an idempotency key so a re-pended command that already actuated is not blindly resent.

**4. Bridge dials arbitrary UDP targets using the cloud-supplied device IP with no LAN/RFC1918 validation**  
- _Cloud bridge (local engine ↔ cloud API)_ · `security` · `backend/internal/engine/cloud_bridge.go:131`
- **Impact:** The boundary's premise is that the cloud (Render) is untrusted and the bridge runs on the home LAN. A compromised/malicious cloud claim response can make the local bridge emit attacker-chosen UDP datagrams to any host:38899 reachable from the LAN (internal scanning / poking other UDP services from inside the perimeter). The payload is constrained to a WiZ setPilot/getPilot JSON blob, so this is a UDP-egress/SSRF-style primitive, not RCE, and in the single-tenant deployment the cloud DB is only writable via APP_SECRET_KEY behind the owner proxy — hence medium, not high.
- **Fix:** Validate device.IPAddress before dialing: net.ParseIP and reject anything not a private/LAN address (RFC1918 / link-local) appropriate for WiZ bulbs, ideally restricting to the bridge host's own subnet. Skip the device and complete the command as 'failed' on a non-conforming IP, so a compromised cloud cannot steer the bridge at non-bulb hosts.

**5. transition_ms is stored, defaulted and surfaced but never applied — the fade feature is a complete no-op**  
- _Scenes & scene execution_ · `correctness` · `backend/internal/handler/scene.go:202-267`
- **Impact:** Every scene action's transition_ms is silently ignored. Any future UI authoring smooth fades gets instant hard cuts with no error, while the value is persisted as if meaningful.
- **Fix:** Either plumb transition_ms into the WiZ command (note WiZ setPilot has no fade duration, so a true transition requires the bridge to step dimming/temp over time), or remove transition_ms from the model/schema/Create default so the API stops advertising an unimplemented capability. At minimum, document it as unimplemented.

**6. A scene can never turn a device OFF — both apply paths hardcode on:true**  
- _Scenes & scene execution_ · `correctness` · `backend/internal/handler/scene.go:215`
- **Impact:** Any DB scene whose intent is 'off' (Goodnight / kill hallway lights for a movie) is impossible through the scene-activation path — activation forces every targeted bulb ON. Note the live frontend does not currently use the DB scene path (its OFF preset goes through useLampCommand as a direct device command with {on:false}), so the practical blast radius is limited to future/API consumers of /scenes.
- **Fix:** Read `on` from target_state when present and default to true only when absent: `on := true; if v, ok := a.TargetState["on"].(bool); ok { on = v }`; propagate to opts.On, statePatch, and the queued command. Mirror in commandFromSceneAction.

**7. Scene activation ignores color_temp_mireds — the unit the rest of the app actually uses**  
- _Scenes & scene execution_ · `correctness` · `backend/internal/handler/scene.go:225-230`
- **Impact:** A scene_action whose target_state carries color_temp_mireds (the convention used in lib/scenes.ts and honored by the engine) applies NO color temperature through scene activation — brightness/on change while temp is dropped, with no error. Cross-path inconsistency between the device-command engine and the scene handler.
- **Fix:** In both Activate and commandFromSceneAction, handle color_temp_mireds (convert via wiz.MiredsToKelvin when color_temp is absent), or better, route scene activation through the same commandToWizParams normalization the engine uses so units are handled in one place.

**8. Queue-path activation is non-atomic and fails mid-fan-out, leaving a scene half-applied with lying optimistic state**  
- _Scenes & scene execution_ · `reliability` · `backend/internal/handler/scene.go:180-200`
- **Impact:** A transient DB error partway through a multi-device scene returns a bare 500 while earlier actions are already queued (and will execute) and their optimistic device state already written; remaining actions are never queued. No rollback, no partial-result signal.
- **Fix:** Enqueue all device_commands for the scene in a single DB transaction (batched insert) and write optimistic state only after commit; roll back on error for all-or-nothing. Alternatively return a structured partial-result payload instead of a bare 500.

**9. Optimistic device state is written before the bridge executes, so a failed/stuck command leaves current_state permanently wrong**  
- _Scenes & scene execution_ · `correctness` · `backend/internal/handler/scene.go:192-196`
- **Impact:** In queue mode current_state is set to target before the bridge claims/runs the command. If the bridge is offline or the command fails, devices.current_state lies until the status poller corrects it; if status polling is disabled the lie is permanent. Uses context.Background(), decoupled from request lifecycle.
- **Fix:** Drop the pre-execution optimistic UpdateState in the queue path (let the bridge status report the real state), or write only a distinct 'pending/target' marker. If kept for snappy UX, ensure the bridge status poll is always enabled so failures self-correct.

**10. Scene-apply, color, and room toggle toasts report success synchronously before any command resolves**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `ux` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/components/scenes/SceneBar.tsx:146-156`
- **Impact:** Success toasts ('Scène toegepast', 'Kleur toegepast', '<scene> toegepast in <room>') appear even if every per-lamp command fails. The optimistic setQueryData also makes all online cards visually adopt the scene, reinforcing false confidence. The toast has no relationship to the actual outcome.
- **Fix:** Aggregate the per-lamp command promises with Promise.allSettled and base the toast on real results (success only if all/most succeed, otherwise partial/error). Keep the optimistic visual but reconcile on settle.

**11. Mid-drag slider and color state is clobbered by the post-command refetch**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `ux` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/components/lamp/LampControl.tsx:62-76`
- **Impact:** During a sustained slider drag or color sweep, an in-flight command success refetch can snap the handle to a value other than the current gesture position, so the control can fight the user. The debounce reduces but does not eliminate this.
- **Fix:** Guard the sync effect with an 'interacting' ref set on pointer down/up to suppress external resync during a gesture, or remove devices invalidation from the lamp-command onSuccess and refetch only after interaction ends.

**12. Add-device and IP-edit accept any string with no IP format validation client or proxy side**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `correctness` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/components/settings/AddDeviceForm.tsx:21-31`
- **Impact:** A typo or malformed IP (192.168.1, 192.168.1.999, a hostname) is silently registered in queue mode (the UDP probe is skipped). Every future command to that device fails at the bridge with no user-visible error, producing a device that looks registered but never responds.
- **Fix:** Validate IPv4 shape on submit in AddDeviceForm and DeviceRow before calling the API and show an inline error. The backend should also reject syntactically invalid ip_address in Register and Update regardless of command mode.

**13. Device ip_address is never validated and is dialed directly over UDP — SSRF-on-LAN primitive (port 38899)**  
- _Cross-cutting security, authz & input validation_ · `security` · `backend/internal/wiz/client.go:52`
- **Impact:** A caller with the owner/app secret who writes a device can point ip_address at any host:38899 reachable from the WiZ-client process. For the LOCAL engine/bridge that is the whole home LAN — every poll cycle fires UDP setPilot/getPilot at that host and reports reachable/unreachable back, turning the device list into a blind LAN UDP probe/packet-sender. In direct mode the cloud GetState probe on register/IP-change is a weaker SSRF from Render egress, but with LIGHT_COMMAND_MODE=queue or skip_probe=true that probe is skipped (device.go:261) so arbitrary IPs are stored unconditionally.
- **Fix:** Validate ip_address with net.ParseIP at Register and Update and reject non-parsing values; optionally reject loopback/link-local/multicast and constrain to an expected LAN CIDR/allowlist from config. Apply the same parse check in mapBridgeDevice / getDeviceMap before dialing so a pre-existing bad row cannot be used.

### ⚪ LOW (17)

**1. RGB values are sent to the lamp and stored without clamping to 0-255**  
- _Devices, command queue & WiZ UDP control_ · `correctness` · `backend/internal/wiz/client.go:155`
- **Impact:** SetState sets params r/g/b directly (client.go:155-159) while brightness/temp are clamped (client.go:150,153). The raw-RGB handler path uses derefOr(cmd.R,0) etc. with no bounds (device.go:373-383), and commandToWizParams passes r/g/b through unmodified (commands.go:227-231). model.DeviceCommandRequest R/G/B are *int (model.go:81-83), so an out-of-range value (e.g. 999 or negative) is sent verbatim and stored in current_state. Firmware will clip/reject while the DB keeps the bad value. Input is owner-only and the HSV path is bounded by construction, so impact is cosmetic.
- **Fix:** Clamp r,g,b to 0-255 in client.SetState (and ideally in commandToWizParams / the handler statePatch) for consistency with the existing brightness/temp clamps.

**2. Bridge writes unbounded, unvalidated current_state JSONB and arbitrary status strings from the LAN side back into the cloud devices table**  
- _Cloud bridge (local engine ↔ cloud API)_ · `correctness` · `backend/internal/handler/bridge.go:181`
- **Impact:** Whoever holds the bridge credential can write arbitrary JSON keys (bounded only by the global 50 MiB MaxBytes) and arbitrary status strings into the shared devices table that the UI and engine read back. Not privilege escalation (the same credential already controls the bulbs), but a misbehaving/compromised bridge can poison device state/status shown to the owner and consumed by automations. The legitimate bridge only ever sends the six known keys (cloud_bridge.go:192-199), so this is latent.
- **Fix:** Whitelist current_state to the known WiZ fields (on/brightness/color_temp/r/g/b) and validate Status against an allowed set (e.g. online/offline) in UpdateDeviceStatus before persisting, mirroring how command params are whitelisted in commandToWizParams.

**3. Direct-path activation is best-effort but always returns 204, hiding per-device failures**  
- _Scenes & scene execution_ · `reliability` · `backend/internal/handler/scene.go:202-266`
- **Impact:** A direct-mode scene touching N devices where some/all fail (offline, no IP, UDP timeout) still reports HTTP 204 success; the room is left partially applied with no signal for the client to retry.
- **Fix:** Aggregate per-device success/fail counts under a mutex (as processCommand does) and return a 207-style summary or a non-2xx when zero devices succeeded; surface which devices failed.

**4. No scenes Update endpoint — editing a scene requires delete+recreate, and there is no per-action validation on Create**  
- _Scenes & scene execution_ · `maintainability` · `backend/internal/store/scene.go:78-130`
- **Impact:** Scene edits are delete-then-create (loses id, races concurrent activations). Create does no device_id existence or target_state range validation, so an invalid device UUID surfaces as an opaque 500 from the FK violation (which also rolls back the whole scene), and out-of-range brightness/temp is accepted and stored (only clamped at WiZ send time).
- **Fix:** Add an Update/PATCH store method + route so edits are atomic and preserve the id; in Create, validate each action's device_id and basic target_state ranges, returning 400 with a clear message instead of leaking the FK error as a 500.

**5. Device deletion silently empties scenes via ON DELETE CASCADE**  
- _Scenes & scene execution_ · `ux` · `backend/internal/store/runtime_schema_base.go:50`
- **Impact:** Deleting a device silently strips it from every scene; a scene can become empty and then 'activate' as a no-op 204 that looks successful. No orphan corruption, but no user-visible signal that scenes were mutated.
- **Fix:** Acceptable for referential integrity, but surface it: count/return affected scene_actions when deleting a device (or warn the owner), and have Activate distinguish 'scene has no actions' from success so an emptied scene is detectable in the UI.

**6. GetAll issues an N+1 query (one getActions per scene)**  
- _Scenes & scene execution_ · `performance` · `backend/internal/store/scene.go:45-52`
- **Impact:** Negligible at current single-tenant scale (few scenes), but list cost grows linearly with scene count and adds round-trip latency if scenes proliferate.
- **Fix:** Fetch all actions for the listed scene ids in one query (WHERE scene_id = ANY($1) ORDER BY scene_id, execution_order) and group in memory, or LEFT JOIN and assemble. Low priority given scale.

**7. Automation can double-fire across a process restart inside the ±1-minute window (and on DST fall-back)**  
- _Automation engine (trigger / condition / action)_ · `correctness` · `backend/internal/engine/trigger.go:37-42`
- **Impact:** Restart inside the 3-minute fire window re-fires once (idempotent actions limit blast radius). DST fall-back fires a 02:30 rule twice ~1h apart; spring-forward silently skips a 02:30 rule. All low-impact because on/off/scene actions are idempotent.
- **Fix:** Raise MinFireInterval to span the whole fire window (>=120s) so the persisted last_fired_at alone blocks a re-fire after restart, and/or gate firing on a per-target-minute key (autoID+target-minute-of-day) instead of a sliding interval. Document or special-case DST.

**8. Update endpoint silently ignores enabled and group_name it accepts in the body**  
- _Automation engine (trigger / condition / action)_ · `correctness` · `backend/internal/store/automation.go:67-78`
- **Impact:** A PUT that sets enabled=false (or changes group_name) is a silent no-op on those fields; the rule stays enabled and keeps firing. Today the UI preserves initialData.enabled and uses a separate /toggle endpoint, so no live data loss, but the API contract (handler decodes the full AutomationRow, frontend sends enabled/group_name) is misleading.
- **Fix:** Either include enabled and group_name in the UPDATE SET clause, or strip/document those fields from the Update model so the API contract matches behavior.

**9. condition_config is never evaluated — conditions silently do nothing**  
- _Automation engine (trigger / condition / action)_ · `maintainability` · `backend/internal/engine/trigger.go:10-123`
- **Impact:** Any automation carrying a condition_config would be ignored and fire unconditionally — a schema-advertised guard the engine does not honor. No current automation uses it (the live AutomationRow model doesn't even carry the field and the frontend never writes it), so impact is contained to future misuse.
- **Fix:** Either implement condition evaluation in ShouldFire/tick, or remove condition_config from the base schema and document that conditions are unsupported, so no operator assumes a non-existent safety guard.

**10. Duplicate-name create returns a misleading HTTP 500 instead of a 409 conflict**  
- _Automation engine (trigger / condition / action)_ · `ux` · `backend/internal/store/automation.go:38-43`
- **Impact:** Creating an automation whose name already exists returns 500 with body 'no rows in result set' instead of a clear 409. Pure UX/observability; no data risk.
- **Fix:** Return a typed sentinel (e.g. ErrDuplicateName) and map it to 409 with a clear message in the handler; stop overloading pgx.ErrNoRows as a control-flow signal.

**11. Automation tick has no panic recovery (asymmetric with the command poller)**  
- _Automation engine (trigger / condition / action)_ · `reliability` · `backend/internal/engine/engine.go:277-362`
- **Impact:** A panic in tick/executeAction/applyAction or in a spawned action goroutine would crash the whole engine process (Telegram bot, crons, command poller) rather than skipping one bad automation. No reachable panic exists today (all config reads are comma-ok with fallbacks and the action-build path is panic-free), so this is purely defensive.
- **Fix:** Wrap per-automation evaluation/execution in a recover (mirror the command poller at commands.go:32-36) and add a recover inside the action goroutine, so one malformed automation is logged and skipped without crashing the engine.

**12. Device discovery scan performs no network discovery and just re-lists already-registered devices**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `ux` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/components/settings/DeviceDiscoveryPanel.tsx:22-33`
- **Impact:** The scanner labeled 'Lokale WiZ lampen zoeken' cannot find any new/unregistered bulb; it only echoes the existing registry minus itself, so users expecting auto-discovery get a misleading empty result ('Alle gevonden lampen staan al geregistreerd'). The error copy also references a non-deployed localhost:8000 backend.
- **Fix:** Wire this to a real WiZ UDP broadcast-discovery endpoint, or relabel/remove the panel so it does not imply network discovery. Fix the stale localhost:8000 error copy.

**13. Persisted devices cache shows stale lamp state on app launch with no freshness indicator**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `ux` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/app/providers.tsx:14-34`
- **Impact:** After reopening the PWA, lamps can briefly (or persistently if offline/slow) display a stale on/off or color from IndexedDB. Combined with the absence of polling, the dashboard lamps-on count can mislead until the background/focus refetch resolves.
- **Fix:** Add a short refetchInterval to the devices query while lamp/dashboard pages are mounted, or exclude devices from persistence, or show an 'actualiseren' indicator until the first post-mount refetch resolves.

**14. Dashboard allOn counts offline devices, so the toggle-all label can contradict what the action does**  
- _Frontend control UX (lamps, scenes, rooms, device setup)_ · `correctness` · `C:/Users/jeffrey/Desktop/Projecten/JeffriesHomeapp/app/page.tsx:64-66`
- **Impact:** If any device is offline, dashboard allOn is effectively always false (offline devices are never 'on'), so the toggle-all label and aria-pressed are computed against a population the action does not act on (toggleAll iterates onlineDevices only). Minor cosmetic/state inconsistency vs the lampen page.
- **Fix:** Use the same online-scoped predicate as the lampen page: allOn = onlineDevices.length > 0 && onlineDevices.every(d => d.current_state?.on).

**15. Queued and automation-driven WiZ params are sent unclamped, unlike the direct handler path**  
- _Cross-cutting security, authz & input validation_ · `correctness` · `backend/internal/engine/commands.go:202`
- **Impact:** In queue mode, user-supplied brightness/temp and automation action_config values reach the bulb over UDP unclamped, whereas the direct path (wiz.SetState / SetBrightness / SetColorTemp) clamps dimming to 10-100 and temp to 2200-6500. Same logical operation is bounded via one route and unsanitized via another. Bounded blast radius — it is the owner's own bulbs and the firmware rejects nonsense — so not a takeover, just an inconsistent-validation / lost-defensive-layer gap.
- **Fix:** Centralize clamping in commandToWizParams (clamp dimming 10-100, temp 2200-6500, r/g/b 0-255) so every path that reaches the bulb over UDP enforces the same bounds, rather than relying on wiz.SetState which the queue/automation paths bypass.

**16. GET list/read routes for devices, scenes, automations, rooms require only the shared secret, with no second authorization layer**  
- _Cross-cutting security, authz & input validation_ · `security` · `backend/internal/server/routes.go:51`
- **Impact:** Single-tenant design with no per-resource user_id authorization: exactly one credential (APP_SECRET_KEY, which doubles as BridgeAPIKey and per other audits the LaventeCare KEK) sits between an attacker and full device/scene/automation control. automation.go:31 takes userId from the query string with no ownership check; a direct-to-Render caller (bypassing the Next proxy that overrides userId at route.ts:86) could pass an arbitrary userId, though in single-tenant practice only the owner's userId has data. If the one secret leaks, the attacker gets device takeover + automation creation + IP repointing with no fallback authz.
- **Fix:** Accept the single-tenant model but reduce blast radius: (a) split BRIDGE_API_KEY and LAVENTECARE_SECRET_KEY off APP_SECRET_KEY so one leak is not three takeovers; (b) have the backend ignore client-supplied userId for these single-tenant tables and use cfg.HomeappUserID server-side instead of trusting the query param.

**17. Bridge status endpoint writes attacker-controlled current_state JSON and arbitrary status strings with no schema validation**  
- _Cross-cutting security, authz & input validation_ · `security` · `backend/internal/handler/bridge.go:161`
- **Impact:** A caller holding the bridge/app secret can set any device's status to an arbitrary string and merge arbitrary JSON into current_state, which the frontend renders. Requires the secret and only affects display state of the owner's own devices; no injection sink found (values rendered as data, not SQL/HTML-eval — UpdateState uses parameterized jsonb merge). An oversized current_state could bloat the row, bounded by the global MaxBytes request cap.
- **Fix:** Allowlist status values (online/offline/registered) and current_state keys (on/brightness/color_temp/r/g/b) with type+range checks in UpdateDeviceStatus, mirroring the clamping done elsewhere, so the bridge cannot persist arbitrary blobs.
