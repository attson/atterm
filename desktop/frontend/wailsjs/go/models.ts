export namespace main {
	
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
	export class RelayConfig {
	    url: string;
	    token: string;
	    connected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelayConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
	        this.connected = source["connected"];
	    }
	}

}

