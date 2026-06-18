export namespace connhealth {
	
	export class ByteStats {
	    in_per_sec: number;
	    out_per_sec: number;
	
	    static createFrom(source: any = {}) {
	        return new ByteStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.in_per_sec = source["in_per_sec"];
	        this.out_per_sec = source["out_per_sec"];
	    }
	}
	export class RTTSample {
	    at_ms: number;
	    rtt_ms: number;
	
	    static createFrom(source: any = {}) {
	        return new RTTSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at_ms = source["at_ms"];
	        this.rtt_ms = source["rtt_ms"];
	    }
	}
	export class RTTStats {
	    last_ms?: number;
	    p50_ms?: number;
	    p95_ms?: number;
	
	    static createFrom(source: any = {}) {
	        return new RTTStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.last_ms = source["last_ms"];
	        this.p50_ms = source["p50_ms"];
	        this.p95_ms = source["p95_ms"];
	    }
	}
	export class ReconnectEvent {
	    at_ms: number;
	    reason: string;
	    duration_ms: number;
	
	    static createFrom(source: any = {}) {
	        return new ReconnectEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at_ms = source["at_ms"];
	        this.reason = source["reason"];
	        this.duration_ms = source["duration_ms"];
	    }
	}
	export class ReconnectStats {
	    count_last_hour: number;
	    last_at_ms?: number;
	    last_reason: string;
	    history: ReconnectEvent[];
	
	    static createFrom(source: any = {}) {
	        return new ReconnectStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count_last_hour = source["count_last_hour"];
	        this.last_at_ms = source["last_at_ms"];
	        this.last_reason = source["last_reason"];
	        this.history = this.convertValues(source["history"], ReconnectEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    state: string;
	    rtt: RTTStats;
	    rtt_samples: RTTSample[];
	    reconnect: ReconnectStats;
	    bytes: ByteStats;
	    seq_gaps: number;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.rtt = this.convertValues(source["rtt"], RTTStats);
	        this.rtt_samples = this.convertValues(source["rtt_samples"], RTTSample);
	        this.reconnect = this.convertValues(source["reconnect"], ReconnectStats);
	        this.bytes = this.convertValues(source["bytes"], ByteStats);
	        this.seq_gaps = source["seq_gaps"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace feishu {
	
	export class Credentials {
	    AppID: string;
	    AppSecret: string;
	    EncryptKey: string;
	    VerifyToken: string;
	
	    static createFrom(source: any = {}) {
	        return new Credentials(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.AppID = source["AppID"];
	        this.AppSecret = source["AppSecret"];
	        this.EncryptKey = source["EncryptKey"];
	        this.VerifyToken = source["VerifyToken"];
	    }
	}

}

export namespace main {
	
	export class AIInfo {
	    kind: string;
	    session_id?: string;
	    captured_at_unix?: number;
	
	    static createFrom(source: any = {}) {
	        return new AIInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.session_id = source["session_id"];
	        this.captured_at_unix = source["captured_at_unix"];
	    }
	}
	export class ClipboardPastePayload {
	    kind: string;
	    text?: string;
	    filename?: string;
	    content_type?: string;
	    data_base64?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClipboardPastePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.text = source["text"];
	        this.filename = source["filename"];
	        this.content_type = source["content_type"];
	        this.data_base64 = source["data_base64"];
	        this.reason = source["reason"];
	    }
	}
	export class ConfigSummary {
	    default_shell: string;
	    locale: string;
	    terminal_theme: string;
	    notifications_enabled: boolean;
	    shell_integration_enabled: boolean;
	    webgl_renderer_enabled: boolean;
	    logging_enabled: boolean;
	    log_file_path: string;
	    auto_check_updates: boolean;
	    command_notify_threshold_seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new ConfigSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_shell = source["default_shell"];
	        this.locale = source["locale"];
	        this.terminal_theme = source["terminal_theme"];
	        this.notifications_enabled = source["notifications_enabled"];
	        this.shell_integration_enabled = source["shell_integration_enabled"];
	        this.webgl_renderer_enabled = source["webgl_renderer_enabled"];
	        this.logging_enabled = source["logging_enabled"];
	        this.log_file_path = source["log_file_path"];
	        this.auto_check_updates = source["auto_check_updates"];
	        this.command_notify_threshold_seconds = source["command_notify_threshold_seconds"];
	    }
	}
	export class CreateWebhookReq {
	    name: string;
	    url: string;
	    format: string;
	    allow_insecure: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateWebhookReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	        this.format = source["format"];
	        this.allow_insecure = source["allow_insecure"];
	    }
	}
	export class RelayErrorEntry {
	    timestamp: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RelayErrorEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.message = source["message"];
	    }
	}
	export class DiagnosticsPayload {
	    generated_at: string;
	    app_version: string;
	    os: string;
	    arch: string;
	    os_version: string;
	    webview_summary: string;
	    user_agent: string;
	    relay_url: string;
	    relay_status: string;
	    relay_token_redacted: string;
	    allow_insecure_relay: boolean;
	    remote_permission: string;
	    uplink_paused: boolean;
	    recent_relay_errors: RelayErrorEntry[];
	    config: ConfigSummary;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticsPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.generated_at = source["generated_at"];
	        this.app_version = source["app_version"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.os_version = source["os_version"];
	        this.webview_summary = source["webview_summary"];
	        this.user_agent = source["user_agent"];
	        this.relay_url = source["relay_url"];
	        this.relay_status = source["relay_status"];
	        this.relay_token_redacted = source["relay_token_redacted"];
	        this.allow_insecure_relay = source["allow_insecure_relay"];
	        this.remote_permission = source["remote_permission"];
	        this.uplink_paused = source["uplink_paused"];
	        this.recent_relay_errors = this.convertValues(source["recent_relay_errors"], RelayErrorEntry);
	        this.config = this.convertValues(source["config"], ConfigSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DirEntry {
	    name: string;
	    isDir: boolean;
	    size?: number;
	    modTime?: number;
	
	    static createFrom(source: any = {}) {
	        return new DirEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	    }
	}
	export class Endpoint {
	    url: string;
	    session_token: string;
	
	    static createFrom(source: any = {}) {
	        return new Endpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.session_token = source["session_token"];
	    }
	}
	export class FeishuStatusResp {
	    enabled: boolean;
	    mode: string;
	    bound: boolean;
	    open_id: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FeishuStatusResp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.mode = source["mode"];
	        this.bound = source["bound"];
	        this.open_id = source["open_id"];
	        this.disabled = source["disabled"];
	    }
	}
	export class FileContent {
	    path: string;
	    data: number[];
	    isBinary: boolean;
	    truncatedAt?: number;
	
	    static createFrom(source: any = {}) {
	        return new FileContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.data = source["data"];
	        this.isBinary = source["isBinary"];
	        this.truncatedAt = source["truncatedAt"];
	    }
	}
	export class FileExplorerConfig {
	    enabled: boolean;
	    panelWidthPx: number;
	    panelCollapsed: boolean;
	    innerTreeRatio: number;
	    showHidden: boolean;
	    showLineNumbers: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileExplorerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.panelWidthPx = source["panelWidthPx"];
	        this.panelCollapsed = source["panelCollapsed"];
	        this.innerTreeRatio = source["innerTreeRatio"];
	        this.showHidden = source["showHidden"];
	        this.showLineNumbers = source["showLineNumbers"];
	    }
	}
	export class FileMetaInfo {
	    path: string;
	    size: number;
	    modTime: number;
	    isBinary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileMetaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.isBinary = source["isBinary"];
	    }
	}
	export class HostInfo {
	    host_id: string;
	    host: string;
	    user: string;
	
	    static createFrom(source: any = {}) {
	        return new HostInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host_id = source["host_id"];
	        this.host = source["host"];
	        this.user = source["user"];
	    }
	}
	export class LogPreview {
	    path: string;
	    exists: boolean;
	    truncated: boolean;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new LogPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.truncated = source["truncated"];
	        this.content = source["content"];
	    }
	}
	export class LoggingConfig {
	    enabled: boolean;
	    path: string;
	    effective_path: string;
	    dev_dual_output: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LoggingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.path = source["path"];
	        this.effective_path = source["effective_path"];
	        this.dev_dual_output = source["dev_dual_output"];
	    }
	}
	export class NewSessionReq {
	    command: string;
	    args?: string[];
	    cwd?: string;
	    cols?: number;
	    rows?: number;
	    ai_kind?: string;
	    initial_ai_session_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new NewSessionReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.args = source["args"];
	        this.cwd = source["cwd"];
	        this.cols = source["cols"];
	        this.rows = source["rows"];
	        this.ai_kind = source["ai_kind"];
	        this.initial_ai_session_id = source["initial_ai_session_id"];
	    }
	}
	export class NewSessionResp {
	    session_id: string;
	
	    static createFrom(source: any = {}) {
	        return new NewSessionResp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	    }
	}
	export class PairingTokenResponse {
	    token: string;
	    expires_at: number;
	    qr_url: string;
	
	    static createFrom(source: any = {}) {
	        return new PairingTokenResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.expires_at = source["expires_at"];
	        this.qr_url = source["qr_url"];
	    }
	}
	export class PaneSnapshot {
	    slot: number;
	    shell: string;
	    shell_args?: string[];
	    last_cwd?: string;
	    session_type?: string;
	    last_command_line?: string;
	    title?: string;
	    ai?: AIInfo;
	
	    static createFrom(source: any = {}) {
	        return new PaneSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slot = source["slot"];
	        this.shell = source["shell"];
	        this.shell_args = source["shell_args"];
	        this.last_cwd = source["last_cwd"];
	        this.session_type = source["session_type"];
	        this.last_command_line = source["last_command_line"];
	        this.title = source["title"];
	        this.ai = this.convertValues(source["ai"], AIInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ShortcutsConfig {
	    bindings: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ShortcutsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bindings = source["bindings"];
	    }
	}
	export class TranslateConfig {
	    enabled: boolean;
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    defaultTargetLang: string;
	
	    static createFrom(source: any = {}) {
	        return new TranslateConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.defaultTargetLang = source["defaultTargetLang"];
	    }
	}
	export class PluginConfig {
	    fileExplorer: FileExplorerConfig;
	    translate: TranslateConfig;
	    shortcuts: ShortcutsConfig;
	
	    static createFrom(source: any = {}) {
	        return new PluginConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileExplorer = this.convertValues(source["fileExplorer"], FileExplorerConfig);
	        this.translate = this.convertValues(source["translate"], TranslateConfig);
	        this.shortcuts = this.convertValues(source["shortcuts"], ShortcutsConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QuickTemplate {
	    id: string;
	    label: string;
	    text: string;
	    hotkey?: string;
	
	    static createFrom(source: any = {}) {
	        return new QuickTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.text = source["text"];
	        this.hotkey = source["hotkey"];
	    }
	}
	export class TabSnapshot {
	    id: string;
	    layout: string;
	    active_pane_idx: number;
	    col_ratio: number;
	    row_ratio: number;
	    panes: PaneSnapshot[];
	
	    static createFrom(source: any = {}) {
	        return new TabSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.layout = source["layout"];
	        this.active_pane_idx = source["active_pane_idx"];
	        this.col_ratio = source["col_ratio"];
	        this.row_ratio = source["row_ratio"];
	        this.panes = this.convertValues(source["panes"], PaneSnapshot);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RecoverySnapshot {
	    version: number;
	    host_id: string;
	    clean_shutdown: boolean;
	    saved_at_unix: number;
	    active_tab_id?: string;
	    tabs: TabSnapshot[];
	
	    static createFrom(source: any = {}) {
	        return new RecoverySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.host_id = source["host_id"];
	        this.clean_shutdown = source["clean_shutdown"];
	        this.saved_at_unix = source["saved_at_unix"];
	        this.active_tab_id = source["active_tab_id"];
	        this.tabs = this.convertValues(source["tabs"], TabSnapshot);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RelayConfig {
	    url: string;
	    token: string;
	    session_expires_at: number;
	    allow_insecure_relay: boolean;
	    disable_e2ee: boolean;
	    remote_permission: string;
	    last_email: string;
	    connected: boolean;
	    paused: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelayConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
	        this.session_expires_at = source["session_expires_at"];
	        this.allow_insecure_relay = source["allow_insecure_relay"];
	        this.disable_e2ee = source["disable_e2ee"];
	        this.remote_permission = source["remote_permission"];
	        this.last_email = source["last_email"];
	        this.connected = source["connected"];
	        this.paused = source["paused"];
	    }
	}
	
	export class RelayMe {
	    user_id: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new RelayMe(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user_id = source["user_id"];
	        this.email = source["email"];
	    }
	}
	
	
	
	export class UpdateState {
	    current: string;
	    latest: string;
	    available: boolean;
	    notes: string;
	    checking: boolean;
	    last_check_at: number;
	    downloading: boolean;
	    download_pct: number;
	    ready: boolean;
	    error: string;
	    asset_url: string;
	    asset_size: number;
	    download_dir: string;
	    download_path: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.notes = source["notes"];
	        this.checking = source["checking"];
	        this.last_check_at = source["last_check_at"];
	        this.downloading = source["downloading"];
	        this.download_pct = source["download_pct"];
	        this.ready = source["ready"];
	        this.error = source["error"];
	        this.asset_url = source["asset_url"];
	        this.asset_size = source["asset_size"];
	        this.download_dir = source["download_dir"];
	        this.download_path = source["download_path"];
	    }
	}
	export class Webhook {
	    id: string;
	    name: string;
	    url: string;
	    format: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Webhook(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.format = source["format"];
	        this.created_at = source["created_at"];
	    }
	}

}

