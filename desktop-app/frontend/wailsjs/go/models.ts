export namespace main {
	
	export class AppInfo {
	    name: string;
	    version: string;
	    os: string;
	    arch: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	    }
	}
	export class ConnectConfig {
	    link: string;
	    listen: string;
	    bypassList: string[];
	    maxLanes: number;
	    systemProxy: boolean;
	    tunMode: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConnectConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.link = source["link"];
	        this.listen = source["listen"];
	        this.bypassList = source["bypassList"];
	        this.maxLanes = source["maxLanes"];
	        this.systemProxy = source["systemProxy"];
	        this.tunMode = source["tunMode"];
	    }
	}
	export class LaneDTO {
	    id: number;
	    congestionWindow: number;
	    inflight: number;
	    peerWindow: number;
	    effectiveWindow: number;
	    targetPayload: number;
	    rttMs: number;
	    baseRttMs: number;
	    confirmedBytes: number;
	    draining: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LaneDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.congestionWindow = source["congestionWindow"];
	        this.inflight = source["inflight"];
	        this.peerWindow = source["peerWindow"];
	        this.effectiveWindow = source["effectiveWindow"];
	        this.targetPayload = source["targetPayload"];
	        this.rttMs = source["rttMs"];
	        this.baseRttMs = source["baseRttMs"];
	        this.confirmedBytes = source["confirmedBytes"];
	        this.draining = source["draining"];
	    }
	}
	export class LinkInfo {
	    label: string;
	    boards: string[];
	
	    static createFrom(source: any = {}) {
	        return new LinkInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.boards = source["boards"];
	    }
	}
	export class StreamDTO {
	    id: number;
	    target: string;
	    host: string;
	    startedAt: number;
	    totalUp: number;
	    totalDown: number;
	    rateUp: number;
	    rateDown: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.target = source["target"];
	        this.host = source["host"];
	        this.startedAt = source["startedAt"];
	        this.totalUp = source["totalUp"];
	        this.totalDown = source["totalDown"];
	        this.rateUp = source["rateUp"];
	        this.rateDown = source["rateDown"];
	    }
	}
	export class MetricsDTO {
	    status: string;
	    rttMs: number;
	    totalUp: number;
	    totalDown: number;
	    rateUp: number;
	    rateDown: number;
	    rateConfirmedTx: number;
	    backlogFrames: number;
	    backlogBytes: number;
	    blockedWriters: number;
	    lanes: LaneDTO[];
	    streams: StreamDTO[];
	
	    static createFrom(source: any = {}) {
	        return new MetricsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.rttMs = source["rttMs"];
	        this.totalUp = source["totalUp"];
	        this.totalDown = source["totalDown"];
	        this.rateUp = source["rateUp"];
	        this.rateDown = source["rateDown"];
	        this.rateConfirmedTx = source["rateConfirmedTx"];
	        this.backlogFrames = source["backlogFrames"];
	        this.backlogBytes = source["backlogBytes"];
	        this.blockedWriters = source["blockedWriters"];
	        this.lanes = this.convertValues(source["lanes"], LaneDTO);
	        this.streams = this.convertValues(source["streams"], StreamDTO);
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
	
	export class TrayProfile {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new TrayProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class TrayState {
	    status: string;
	    profiles: TrayProfile[];
	    activeId: string;
	
	    static createFrom(source: any = {}) {
	        return new TrayState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.profiles = this.convertValues(source["profiles"], TrayProfile);
	        this.activeId = source["activeId"];
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

