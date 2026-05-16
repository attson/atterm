export namespace main {
	
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
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new Endpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
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
	export class QuickInputButton {
	    id: string;
	    label: string;
	    send: string;
	    appendNewline: boolean;
	    hotkey?: string;
	
	    static createFrom(source: any = {}) {
	        return new QuickInputButton(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.send = source["send"];
	        this.appendNewline = source["appendNewline"];
	        this.hotkey = source["hotkey"];
	    }
	}
	export class QuickInputConfig {
	    enabled: boolean;
	    buttons: QuickInputButton[];
	
	    static createFrom(source: any = {}) {
	        return new QuickInputConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.buttons = this.convertValues(source["buttons"], QuickInputButton);
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
	export class PluginConfig {
	    quickInput: QuickInputConfig;
	    fileExplorer: FileExplorerConfig;
	
	    static createFrom(source: any = {}) {
	        return new PluginConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.quickInput = this.convertValues(source["quickInput"], QuickInputConfig);
	        this.fileExplorer = this.convertValues(source["fileExplorer"], FileExplorerConfig);
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
	    allow_insecure_relay: boolean;
	    remote_permission: string;
	    connected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelayConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
	        this.allow_insecure_relay = source["allow_insecure_relay"];
	        this.remote_permission = source["remote_permission"];
	        this.connected = source["connected"];
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

}

